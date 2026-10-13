package repository

import (
	"context"
	"fmt"
	"sort"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/apikey"
	"github.com/Wei-Shaw/sub2api/ent/organization"
	"github.com/Wei-Shaw/sub2api/ent/organizationallocation"
	"github.com/Wei-Shaw/sub2api/ent/organizationreservation"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"

	"entgo.io/ent/dialect"
)

type organizationLifecycleRepository struct {
	client *dbent.Client
}

func NewOrganizationLifecycleRepository(client *dbent.Client) service.OrganizationLifecycleRepository {
	return &organizationLifecycleRepository{client: client}
}

func (r *organizationLifecycleRepository) GetArchivePreview(ctx context.Context, organizationID int64) (*service.OrganizationArchivePreview, error) {
	if r == nil || r.client == nil {
		return nil, service.ErrOrganizationLifecycleUnavailable
	}
	if organizationID <= 0 {
		return nil, service.ErrInvalidOrganizationID
	}
	entity, err := clientFromContext(ctx, r.client).Organization.Query().Where(organization.IDEQ(organizationID)).Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrOrganizationNotFound, nil)
	}
	return archivePreviewFromEntity(entity), nil
}

func (r *organizationLifecycleRepository) Transition(ctx context.Context, command service.OrganizationLifecycleCommand) (*service.OrganizationLifecycleResult, error) {
	if r == nil || r.client == nil {
		return nil, service.ErrOrganizationLifecycleUnavailable
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin organization lifecycle transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	result, err := r.transitionInTx(txCtx, tx.Client(), command)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit organization lifecycle transaction: %w", err)
	}
	return result, nil
}

func (r *organizationLifecycleRepository) transitionInTx(ctx context.Context, client *dbent.Client, command service.OrganizationLifecycleCommand) (*service.OrganizationLifecycleResult, error) {
	organizationEntity, err := lockLifecycleOrganization(ctx, client, command.OrganizationID)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrOrganizationNotFound, nil)
	}

	switch command.TargetStatus {
	case service.OrganizationStatusDisabled:
		if organizationEntity.Status != service.OrganizationStatusActive {
			return nil, lifecycleTransitionError(organizationEntity.Status)
		}
	case service.OrganizationStatusActive:
		if organizationEntity.Status != service.OrganizationStatusDisabled {
			return nil, lifecycleTransitionError(organizationEntity.Status)
		}
	case service.OrganizationStatusArchived:
		if organizationEntity.Status != service.OrganizationStatusActive && organizationEntity.Status != service.OrganizationStatusDisabled {
			return nil, lifecycleTransitionError(organizationEntity.Status)
		}
		if !archiveConfirmationMatches(organizationEntity, command) {
			return nil, service.ErrOrganizationArchiveVersionConflict
		}
		if err := assertOrganizationArchiveSettled(ctx, client, organizationEntity.ID); err != nil {
			return nil, err
		}
	default:
		return nil, service.ErrOrganizationLifecycleConflict
	}

	updated, err := client.Organization.UpdateOneID(organizationEntity.ID).SetStatus(command.TargetStatus).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("update organization lifecycle: %w", err)
	}
	cacheTargets, err := lifecycleMemberCacheTargets(ctx, client, updated.ID)
	if err != nil {
		return nil, err
	}
	userIDs := make([]int64, 0, len(cacheTargets))
	for _, target := range cacheTargets {
		userIDs = append(userIDs, target.UserID)
	}
	return &service.OrganizationLifecycleResult{
		Organization:    organizationEntityToService(updated),
		CacheTargets:    cacheTargets,
		AffectedUserIDs: userIDs,
	}, nil
}

func lockLifecycleOrganization(ctx context.Context, client *dbent.Client, organizationID int64) (*dbent.Organization, error) {
	query := client.Organization.Query().Where(organization.IDEQ(organizationID))
	if client.Driver().Dialect() == dialect.Postgres {
		query = query.ForUpdate()
	}
	return query.Only(ctx)
}

func archivePreviewFromEntity(entity *dbent.Organization) *service.OrganizationArchivePreview {
	if entity == nil {
		return nil
	}
	return &service.OrganizationArchivePreview{
		OrganizationID: entity.ID,
		Status:         entity.Status,
		Unallocated:    entity.UnallocatedPool.StringFixed(domain.OrganizationMoneyScale),
		BalanceVersion: entity.BalanceVersion,
		UpdatedAt:      entity.UpdatedAt.UTC(),
	}
}

func archiveConfirmationMatches(entity *dbent.Organization, command service.OrganizationLifecycleCommand) bool {
	return entity != nil && command.ConfirmArchive &&
		entity.BalanceVersion == command.ExpectedBalanceVersion &&
		entity.UpdatedAt.UTC().Equal(command.ExpectedUpdatedAt.UTC())
}

func assertOrganizationArchiveSettled(ctx context.Context, client *dbent.Client, organizationID int64) error {
	allocations := client.OrganizationAllocation.Query().
		Where(organizationallocation.OrganizationIDEQ(organizationID)).
		Order(dbent.Asc(organizationallocation.FieldUserIDSnapshot), dbent.Asc(organizationallocation.FieldID))
	if client.Driver().Dialect() == dialect.Postgres {
		allocations = allocations.ForUpdate()
	}
	rows, err := allocations.All(ctx)
	if err != nil {
		return fmt.Errorf("lock organization allocations for archive: %w", err)
	}
	for _, allocation := range rows {
		if allocation.Available.GreaterThan(decimal.Zero) || allocation.Reserved.GreaterThan(decimal.Zero) {
			return service.ErrOrganizationBalanceNotSettled
		}
	}

	reservations := client.OrganizationReservation.Query().
		Where(
			organizationreservation.OrganizationIDEQ(organizationID),
			organizationreservation.StatusIn(
				service.OrganizationReservationStateReserved,
				service.OrganizationReservationStatePendingReconciliation,
			),
		).
		Order(dbent.Asc(organizationreservation.FieldID))
	if client.Driver().Dialect() == dialect.Postgres {
		reservations = reservations.ForUpdate()
	}
	pending, err := reservations.Exist(ctx)
	if err != nil {
		return fmt.Errorf("lock organization reservations for archive: %w", err)
	}
	if pending {
		return service.ErrOrganizationBalanceNotSettled
	}
	return nil
}

func lifecycleMemberCacheTargets(ctx context.Context, client *dbent.Client, organizationID int64) ([]service.OrganizationMemberCacheTarget, error) {
	users, err := client.User.Query().
		Where(dbuser.OrganizationIDEQ(organizationID), dbuser.DeletedAtIsNil()).
		Select(dbuser.FieldID).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list organization lifecycle cache targets: %w", err)
	}
	targets := make([]service.OrganizationMemberCacheTarget, 0, len(users))
	for _, user := range users {
		if user == nil || user.ID <= 0 {
			continue
		}
		keys, err := client.APIKey.Query().
			Where(apikey.UserIDEQ(user.ID), apikey.DeletedAtIsNil()).
			Select(apikey.FieldID, apikey.FieldGroupID).
			All(ctx)
		if err != nil {
			return nil, fmt.Errorf("list organization lifecycle api key cache targets: %w", err)
		}
		target := service.OrganizationMemberCacheTarget{UserID: user.ID}
		for _, key := range keys {
			if key == nil {
				continue
			}
			target.APIKeyIDs = append(target.APIKeyIDs, key.ID)
			if key.GroupID != nil && *key.GroupID > 0 {
				target.GroupIDs = append(target.GroupIDs, *key.GroupID)
			}
		}
		sort.Slice(target.APIKeyIDs, func(i, j int) bool { return target.APIKeyIDs[i] < target.APIKeyIDs[j] })
		sort.Slice(target.GroupIDs, func(i, j int) bool { return target.GroupIDs[i] < target.GroupIDs[j] })
		targets = append(targets, target)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].UserID < targets[j].UserID })
	return targets, nil
}

func lifecycleTransitionError(status string) error {
	if status == service.OrganizationStatusArchived {
		return service.ErrOrganizationArchived
	}
	return service.ErrOrganizationLifecycleConflict
}
