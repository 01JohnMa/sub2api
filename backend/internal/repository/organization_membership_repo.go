package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/apikey"
	"github.com/Wei-Shaw/sub2api/ent/organization"
	"github.com/Wei-Shaw/sub2api/ent/organizationallocation"
	"github.com/Wei-Shaw/sub2api/ent/organizationledgeroperation"
	"github.com/Wei-Shaw/sub2api/ent/organizationledgerposting"
	"github.com/Wei-Shaw/sub2api/ent/organizationreservation"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

type organizationMembershipRepository struct {
	client *dbent.Client
}

func NewOrganizationMembershipRepository(client *dbent.Client) service.OrganizationMembershipRepository {
	return &organizationMembershipRepository{client: client}
}

func (r *organizationMembershipRepository) List(ctx context.Context, organizationID int64, params pagination.PaginationParams) ([]service.OrganizationMember, *pagination.PaginationResult, error) {
	client := clientFromContext(ctx, r.client)
	if _, err := client.Organization.Query().Where(organization.IDEQ(organizationID)).Only(ctx); err != nil {
		return nil, nil, translatePersistenceError(err, service.ErrOrganizationNotFound, nil)
	}

	query := client.User.Query().Where(
		dbuser.OrganizationIDEQ(organizationID),
		dbuser.DeletedAtIsNil(),
	)
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, nil, err
	}
	entities, err := query.
		Offset(params.Offset()).
		Limit(params.Limit()).
		Order(organizationMemberListOrder(params)...).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}
	if len(entities) == 0 {
		return []service.OrganizationMember{}, paginationResultFromTotal(int64(total), params), nil
	}

	userIDs := make([]int64, 0, len(entities))
	for _, entity := range entities {
		userIDs = append(userIDs, entity.ID)
	}
	allocations, err := client.OrganizationAllocation.Query().
		Where(
			organizationallocation.OrganizationIDEQ(organizationID),
			organizationallocation.UserRefIDIn(userIDs...),
			organizationallocation.StatusEQ(domain.OrganizationAllocationStatusActive),
		).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}
	allocationByUser := make(map[int64]*dbent.OrganizationAllocation, len(allocations))
	for _, allocation := range allocations {
		if allocation.UserRefID != nil {
			allocationByUser[*allocation.UserRefID] = allocation
		}
	}

	items := make([]service.OrganizationMember, 0, len(entities))
	for _, entity := range entities {
		allocation := allocationByUser[entity.ID]
		if allocation == nil || entity.OrganizationRole == nil {
			return nil, nil, service.ErrOrganizationMembershipInconsistent
		}
		items = append(items, organizationMemberFromEntities(entity, allocation))
	}
	return items, paginationResultFromTotal(int64(total), params), nil
}

func organizationMemberListOrder(params pagination.PaginationParams) []dbuser.OrderOption {
	orderTermOptions := []entsql.OrderTermOption{}
	if params.NormalizedSortOrder(pagination.SortOrderDesc) == pagination.SortOrderDesc {
		orderTermOptions = append(orderTermOptions, entsql.OrderDesc())
	}

	var primary dbuser.OrderOption
	switch strings.ToLower(strings.TrimSpace(params.SortBy)) {
	case "email":
		primary = dbuser.ByEmail(orderTermOptions...)
	case "username":
		primary = dbuser.ByUsername(orderTermOptions...)
	case "created_at", "":
		primary = dbuser.ByCreatedAt(orderTermOptions...)
	}
	if primary == nil {
		primary = dbuser.ByCreatedAt(orderTermOptions...)
	}
	return []dbuser.OrderOption{primary, dbuser.ByID(orderTermOptions...)}
}

func (r *organizationMembershipRepository) Add(ctx context.Context, organizationID, userID int64, role string) (*service.OrganizationMember, error) {
	return r.withMemberTransaction(ctx, func(txCtx context.Context, client *dbent.Client) (*service.OrganizationMember, error) {
		organizationEntity, err := lockOrganizationForMembership(txCtx, client, organizationID)
		if err != nil {
			return nil, translatePersistenceError(err, service.ErrOrganizationNotFound, nil)
		}
		if organizationEntity.Status == domain.OrganizationStatusArchived {
			return nil, service.ErrOrganizationArchived
		}

		userEntity, err := lockCurrentMembershipTarget(txCtx, client, service.OrganizationMemberTarget{UserID: &userID})
		if err != nil || userEntity == nil {
			return nil, service.ErrOrganizationMemberIneligible
		}
		return r.addLockedMember(txCtx, client, organizationID, userEntity, role)
	})
}

// AddCurrentMember resolves an exact target inside the membership transaction
// and always assigns organization_member. This keeps email lookup, eligibility
// checks, zero allocation creation, and API Key attribution atomic.
func (r *organizationMembershipRepository) AddCurrentMember(ctx context.Context, organizationID int64, target service.OrganizationMemberTarget) (*service.OrganizationMember, error) {
	return r.withMemberTransaction(ctx, func(txCtx context.Context, client *dbent.Client) (*service.OrganizationMember, error) {
		organizationEntity, err := lockOrganizationForMembership(txCtx, client, organizationID)
		if err != nil {
			return nil, translatePersistenceError(err, service.ErrOrganizationNotFound, nil)
		}
		if organizationEntity.Status != domain.OrganizationStatusActive {
			return nil, service.ErrOrganizationUnavailable
		}

		userEntity, err := lockCurrentMembershipTarget(txCtx, client, target)
		if err != nil || userEntity == nil {
			return nil, service.ErrOrganizationMemberIneligible
		}
		return r.addLockedMember(txCtx, client, organizationID, userEntity, service.OrganizationRoleMember)
	})
}

func lockOrganizationForMembership(ctx context.Context, client *dbent.Client, organizationID int64) (*dbent.Organization, error) {
	query := client.Organization.Query().Where(organization.IDEQ(organizationID))
	if client.Driver().Dialect() == dialect.Postgres {
		query = query.ForUpdate()
	}
	return query.Only(ctx)
}

func lockCurrentMembershipTarget(ctx context.Context, client *dbent.Client, target service.OrganizationMemberTarget) (*dbent.User, error) {
	query := client.User.Query().Where(dbuser.DeletedAtIsNil())
	if target.UserID != nil {
		query = query.Where(dbuser.IDEQ(*target.UserID))
	} else {
		query = query.Where(userEmailLookupPredicate(target.Email))
	}
	if client.Driver().Dialect() == dialect.Postgres {
		query = query.ForUpdate()
	}
	entities, err := query.All(ctx)
	if err != nil || len(entities) != 1 {
		return nil, err
	}
	return entities[0], nil
}

func (r *organizationMembershipRepository) addLockedMember(ctx context.Context, client *dbent.Client, organizationID int64, userEntity *dbent.User, role string) (*service.OrganizationMember, error) {
	userID := userEntity.ID
	if userEntity.OrganizationID != nil || userEntity.OrganizationRole != nil {
		return nil, service.ErrOrganizationMemberIneligible
	}
	activeAllocation, err := client.OrganizationAllocation.Query().
		Where(
			organizationallocation.UserIDSnapshotEQ(userID),
			organizationallocation.StatusEQ(domain.OrganizationAllocationStatusActive),
		).
		Exist(ctx)
	if err != nil {
		return nil, err
	}
	if activeAllocation {
		return nil, service.ErrOrganizationMemberIneligible
	}

	updatedUser, err := client.User.UpdateOneID(userID).
		SetOrganizationID(organizationID).
		SetOrganizationRole(role).
		Save(ctx)
	if err != nil {
		return nil, translateMembershipWriteError(err)
	}
	allocation, err := client.OrganizationAllocation.Create().
		SetOrganizationID(organizationID).
		SetUserIDSnapshot(userID).
		SetUserRefID(userID).
		SetStatus(domain.OrganizationAllocationStatusActive).
		SetAvailable(decimal.Zero).
		SetReserved(decimal.Zero).
		Save(ctx)
	if err != nil {
		return nil, translateMembershipWriteError(err)
	}
	if _, err := client.APIKey.Update().
		Where(apikey.UserIDEQ(userID), apikey.DeletedAtIsNil()).
		SetOrganizationID(organizationID).
		Save(ctx); err != nil {
		return nil, translateMembershipWriteError(err)
	}

	return memberFromLockedEntities(updatedUser, allocation)
}

func (r *organizationMembershipRepository) ChangeRole(ctx context.Context, organizationID, userID int64, role string) (*service.OrganizationMember, error) {
	return r.withMemberTransaction(ctx, func(txCtx context.Context, client *dbent.Client) (*service.OrganizationMember, error) {
		organizationEntity, err := client.Organization.Query().
			Where(organization.IDEQ(organizationID)).
			ForUpdate().
			Only(txCtx)
		if err != nil {
			return nil, translatePersistenceError(err, service.ErrOrganizationNotFound, nil)
		}
		if organizationEntity.Status == domain.OrganizationStatusArchived {
			return nil, service.ErrOrganizationArchived
		}

		userEntity, err := client.User.Query().
			Where(
				dbuser.IDEQ(userID),
				dbuser.OrganizationIDEQ(organizationID),
				dbuser.DeletedAtIsNil(),
			).
			ForUpdate().
			Only(txCtx)
		if err != nil {
			return nil, service.ErrOrganizationMemberNotFound
		}
		allocation, err := client.OrganizationAllocation.Query().
			Where(
				organizationallocation.OrganizationIDEQ(organizationID),
				organizationallocation.UserRefIDEQ(userID),
				organizationallocation.StatusEQ(domain.OrganizationAllocationStatusActive),
			).
			ForUpdate().
			Only(txCtx)
		if err != nil {
			if dbent.IsNotFound(err) {
				return nil, service.ErrOrganizationMembershipInconsistent
			}
			return nil, err
		}

		if userEntity.OrganizationRole == nil {
			return nil, service.ErrOrganizationMembershipInconsistent
		}
		if *userEntity.OrganizationRole != role {
			userEntity, err = client.User.UpdateOneID(userID).
				SetOrganizationRole(role).
				Save(txCtx)
			if err != nil {
				return nil, translateMembershipWriteError(err)
			}
		}
		return memberFromLockedEntities(userEntity, allocation)
	})
}

func (r *organizationMembershipRepository) Remove(ctx context.Context, command service.OrganizationMemberRemovalCommand) (*service.OrganizationMemberRemovalResult, error) {
	if r == nil || r.client == nil {
		return nil, service.ErrOrganizationMembershipUnavailable
	}
	if err := validateMemberRemovalCommand(command); err != nil {
		return nil, err
	}
	if existing := dbent.TxFromContext(ctx); existing != nil {
		return nil, service.ErrOrganizationMemberRemovalInvariant.WithCause(errors.New("member removal must own its transaction"))
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin organization member removal: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	result, err := r.removeInTx(txCtx, tx.Client(), command)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit organization member removal: %w", err)
	}
	return result, nil
}

func (r *organizationMembershipRepository) removeInTx(ctx context.Context, client *dbent.Client, command service.OrganizationMemberRemovalCommand) (*service.OrganizationMemberRemovalResult, error) {
	organizationEntity, err := lockOrganizationForMemberRemoval(ctx, client, command.OrganizationID)
	if err != nil {
		if isMemberRemovalLockUnavailable(err) {
			return nil, service.ErrOrganizationAllocationHasReservations
		}
		return nil, translatePersistenceError(err, service.ErrOrganizationNotFound, nil)
	}
	if !memberRemovalLifecycleAllowed(organizationEntity.Status, command.Actor.AuthorizationScope) {
		return nil, service.ErrOrganizationMemberRemovalForbidden
	}

	lockedUsers, err := lockAllocationUsers(ctx, client, []int64{command.Actor.UserID, command.TargetUserID})
	if err != nil {
		return nil, err
	}
	actorEntity := userByID(lockedUsers, command.Actor.UserID)
	if !persistedMemberRemovalActorAllowed(actorEntity, command.Actor, command.OrganizationID) {
		return nil, service.ErrOrganizationMemberRemovalForbidden
	}

	existing, err := findMemberRemovalOperation(ctx, client, command)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		existing.Replayed = true
		return existing, nil
	}

	targetEntity := userByID(lockedUsers, command.TargetUserID)
	if targetEntity == nil || targetEntity.OrganizationID == nil || *targetEntity.OrganizationID != command.OrganizationID {
		return nil, service.ErrOrganizationMemberNotFound
	}
	if targetEntity.OrganizationRole == nil {
		return nil, service.ErrOrganizationMembershipInconsistent
	}
	if command.Actor.AuthorizationScope == domain.OrganizationFundingAuthorizationOrganizationJWT &&
		(command.Actor.UserID == targetEntity.ID || *targetEntity.OrganizationRole != domain.OrganizationRoleMember) {
		return nil, service.ErrOrganizationMemberRemovalForbidden
	}

	allocationQuery := client.OrganizationAllocation.Query().Where(
		organizationallocation.OrganizationIDEQ(command.OrganizationID),
		organizationallocation.UserRefIDEQ(command.TargetUserID),
		organizationallocation.StatusEQ(domain.OrganizationAllocationStatusActive),
	)
	if client.Driver().Dialect() == dialect.Postgres {
		allocationQuery = allocationQuery.ForUpdate(entsql.WithLockAction(entsql.NoWait))
	}
	allocationEntity, err := allocationQuery.Only(ctx)
	if err != nil {
		if isMemberRemovalLockUnavailable(err) {
			return nil, service.ErrOrganizationAllocationHasReservations
		}
		if dbent.IsNotFound(err) {
			return nil, service.ErrOrganizationMembershipInconsistent
		}
		return nil, fmt.Errorf("lock member removal allocation: %w", err)
	}

	reservationQuery := client.OrganizationReservation.Query().
		Where(
			organizationreservation.AllocationIDSnapshotEQ(allocationEntity.ID),
			organizationreservation.StatusIn(
				service.OrganizationReservationStateReserved,
				service.OrganizationReservationStatePendingReconciliation,
			),
		).
		Order(dbent.Asc(organizationreservation.FieldID))
	if client.Driver().Dialect() == dialect.Postgres {
		reservationQuery = reservationQuery.ForUpdate(entsql.WithLockAction(entsql.NoWait))
	}
	unresolvedReservations, err := reservationQuery.All(ctx)
	if err != nil {
		if isMemberRemovalLockUnavailable(err) {
			return nil, service.ErrOrganizationAllocationHasReservations
		}
		return nil, fmt.Errorf("lock member removal reservations: %w", err)
	}
	if len(unresolvedReservations) != 0 || !allocationEntity.Reserved.IsZero() {
		return nil, service.ErrOrganizationAllocationHasReservations
	}
	if organizationEntity.Status == domain.OrganizationStatusArchived && !allocationEntity.Available.IsZero() {
		return nil, service.ErrOrganizationArchived
	}
	if organizationEntity.BalanceVersion == math.MaxInt64 {
		return nil, service.ErrOrganizationMemberRemovalInvariant
	}

	keyEntities, err := client.APIKey.Query().
		Where(apikey.UserIDEQ(command.TargetUserID), apikey.DeletedAtIsNil()).
		Order(dbent.Asc(apikey.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list member removal api keys: %w", err)
	}
	keyIDs := make([]int64, 0, len(keyEntities))
	groupIDs := make([]int64, 0, len(keyEntities))
	for _, keyEntity := range keyEntities {
		if keyEntity.OrganizationID == nil || *keyEntity.OrganizationID != command.OrganizationID {
			return nil, service.ErrOrganizationMembershipInconsistent
		}
		keyIDs = append(keyIDs, keyEntity.ID)
		if keyEntity.GroupID != nil {
			groupIDs = append(groupIDs, *keyEntity.GroupID)
		}
	}

	reclaimed := allocationEntity.Available
	poolBefore := organizationEntity.UnallocatedPool
	poolAfter := poolBefore.Add(reclaimed)
	versionBefore := organizationEntity.BalanceVersion
	versionAfter := versionBefore + 1
	if _, err := client.Organization.UpdateOneID(command.OrganizationID).
		Where(organization.BalanceVersionEQ(versionBefore)).
		SetUnallocatedPool(poolAfter).
		SetBalanceVersion(versionAfter).
		Save(ctx); err != nil {
		if dbent.IsNotFound(err) {
			return nil, service.ErrOrganizationMemberRemovalInvariant
		}
		return nil, fmt.Errorf("update member removal organization pool: %w", err)
	}
	if _, err := client.OrganizationAllocation.UpdateOneID(allocationEntity.ID).
		Where(
			organizationallocation.OrganizationIDEQ(command.OrganizationID),
			organizationallocation.UserIDSnapshotEQ(command.TargetUserID),
			organizationallocation.StatusEQ(domain.OrganizationAllocationStatusActive),
		).
		SetAvailable(decimal.Zero).
		SetReserved(decimal.Zero).
		SetStatus(domain.OrganizationAllocationStatusClosed).
		AddVersion(1).
		Save(ctx); err != nil {
		return nil, service.ErrOrganizationMemberRemovalInvariant.WithCause(err)
	}
	if _, err := client.User.UpdateOneID(command.TargetUserID).
		ClearOrganizationID().
		ClearOrganizationRole().
		Save(ctx); err != nil {
		return nil, translateMembershipWriteError(err)
	}
	if _, err := client.APIKey.Update().
		Where(apikey.UserIDEQ(command.TargetUserID), apikey.DeletedAtIsNil()).
		ClearOrganizationID().
		Save(ctx); err != nil {
		return nil, translateMembershipWriteError(err)
	}

	result := &service.OrganizationMemberRemovalResult{
		OperationID:          command.OperationID,
		CorrelationID:        command.CorrelationID,
		OrganizationID:       command.OrganizationID,
		UserID:               command.TargetUserID,
		AllocationID:         allocationEntity.ID,
		OperationType:        domain.OrganizationFundingOperationMemberRemovalReclaim,
		Currency:             domain.OrganizationCurrencyUSD,
		Outcome:              service.OrganizationAllocationOutcomeCommitted,
		Reclaimed:            reclaimed.StringFixed(domain.OrganizationMoneyScale),
		PoolBefore:           poolBefore.StringFixed(domain.OrganizationMoneyScale),
		PoolAfter:            poolAfter.StringFixed(domain.OrganizationMoneyScale),
		BalanceVersionBefore: versionBefore,
		BalanceVersionAfter:  versionAfter,
		CanonicalPayloadHash: command.CanonicalPayloadHash,
		APIKeyIDs:            keyIDs,
		GroupIDs:             groupIDs,
	}
	if err := service.ValidateOrganizationMemberRemovalResult(result); err != nil {
		return nil, err
	}

	var resultSnapshot json.RawMessage
	if reclaimed.IsZero() {
		resultSnapshot, err = json.Marshal(result)
		if err != nil {
			return nil, service.ErrOrganizationMemberRemovalInvariant.WithCause(err)
		}
	}
	operationEntity, err := createMemberRemovalLedgerOperation(ctx, client, command, resultSnapshot)
	if err != nil {
		return nil, err
	}
	if !reclaimed.IsZero() {
		if _, err := createAllocationPosting(ctx, client, allocationPostingInput{
			Operation: operationEntity, Sequence: 1, BucketType: domain.OrganizationLedgerBucketUnallocated,
			BucketID: command.OrganizationID, Before: poolBefore, Delta: reclaimed, After: poolAfter,
			VersionBefore: versionBefore, VersionAfter: versionAfter,
		}); err != nil {
			return nil, err
		}
		negativeReclaimed := reclaimed.Neg()
		if _, err := createAllocationPosting(ctx, client, allocationPostingInput{
			Operation: operationEntity, Sequence: 2, BucketType: domain.OrganizationLedgerBucketAllocationAvailable,
			BucketID: allocationEntity.ID, AllocationID: &allocationEntity.ID, TargetUserID: &targetEntity.ID,
			Before: reclaimed, Delta: negativeReclaimed, After: decimal.Zero,
			VersionBefore: versionBefore, VersionAfter: versionAfter,
		}); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func lockOrganizationForMemberRemoval(ctx context.Context, client *dbent.Client, organizationID int64) (*dbent.Organization, error) {
	query := client.Organization.Query().Where(organization.IDEQ(organizationID))
	if client.Driver().Dialect() == dialect.Postgres {
		query = query.ForUpdate(entsql.WithLockAction(entsql.NoWait))
	}
	return query.Only(ctx)
}

func findMemberRemovalOperation(ctx context.Context, client *dbent.Client, command service.OrganizationMemberRemovalCommand) (*service.OrganizationMemberRemovalResult, error) {
	operationEntity, err := client.OrganizationLedgerOperation.Query().
		Where(
			organizationledgeroperation.AuthorizationScopeEQ(command.AuthorizationScope),
			organizationledgeroperation.OrganizationIDEQ(command.OrganizationID),
			organizationledgeroperation.OperationTypeEQ(domain.OrganizationFundingOperationMemberRemovalReclaim),
			organizationledgeroperation.IdempotencyKeyHashEQ(command.IdempotencyKeyHash),
		).
		WithPostings(func(query *dbent.OrganizationLedgerPostingQuery) {
			query.Order(dbent.Asc(organizationledgerposting.FieldPostingSequence))
		}).
		Only(ctx)
	if dbent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read member removal idempotency record: %w", err)
	}
	if operationEntity.CanonicalPayloadHash != command.CanonicalPayloadHash {
		return nil, service.ErrIdempotencyKeyConflict
	}
	result, err := memberRemovalResultFromOperation(operationEntity)
	if err != nil {
		return nil, err
	}
	keyEntities, err := client.APIKey.Query().
		Where(apikey.UserIDEQ(command.TargetUserID), apikey.DeletedAtIsNil()).
		Order(dbent.Asc(apikey.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list member removal replay cache targets: %w", err)
	}
	result.APIKeyIDs = make([]int64, 0, len(keyEntities))
	result.GroupIDs = make([]int64, 0, len(keyEntities))
	for _, keyEntity := range keyEntities {
		result.APIKeyIDs = append(result.APIKeyIDs, keyEntity.ID)
		if keyEntity.GroupID != nil {
			result.GroupIDs = append(result.GroupIDs, *keyEntity.GroupID)
		}
	}
	return result, nil
}

func memberRemovalResultFromOperation(operationEntity *dbent.OrganizationLedgerOperation) (*service.OrganizationMemberRemovalResult, error) {
	if operationEntity == nil {
		return nil, service.ErrOrganizationMemberRemovalInvariant
	}
	postings, err := operationEntity.Edges.PostingsOrErr()
	if err != nil {
		return nil, service.ErrOrganizationMemberRemovalInvariant.WithCause(err)
	}
	if len(postings) == 0 {
		var result service.OrganizationMemberRemovalResult
		if len(operationEntity.NoOpResultSnapshot) == 0 || json.Unmarshal(operationEntity.NoOpResultSnapshot, &result) != nil {
			return nil, service.ErrOrganizationMemberRemovalInvariant
		}
		if result.OperationID != operationEntity.OperationID || result.CorrelationID != operationEntity.CorrelationID ||
			result.OrganizationID != operationEntity.OrganizationID || result.OperationType != operationEntity.OperationType ||
			result.Currency != operationEntity.Currency || result.CanonicalPayloadHash != operationEntity.CanonicalPayloadHash ||
			result.Reclaimed != "0.00000000" || result.PoolBefore != result.PoolAfter {
			return nil, service.ErrOrganizationMemberRemovalInvariant
		}
		return &result, service.ValidateOrganizationMemberRemovalResult(&result)
	}
	if len(postings) != 2 || len(operationEntity.NoOpResultSnapshot) != 0 {
		return nil, service.ErrOrganizationMemberRemovalInvariant
	}
	var poolPosting, allocationPosting *dbent.OrganizationLedgerPosting
	for _, posting := range postings {
		switch posting.BucketType {
		case domain.OrganizationLedgerBucketUnallocated:
			poolPosting = posting
		case domain.OrganizationLedgerBucketAllocationAvailable:
			allocationPosting = posting
		}
	}
	if poolPosting == nil || allocationPosting == nil || allocationPosting.AllocationIDSnapshot == nil || allocationPosting.TargetUserIDSnapshot == nil ||
		!poolPosting.DeltaAmount.Equal(allocationPosting.DeltaAmount.Neg()) || !allocationPosting.AfterAmount.IsZero() ||
		poolPosting.BalanceVersionBefore != allocationPosting.BalanceVersionBefore || poolPosting.BalanceVersionAfter != allocationPosting.BalanceVersionAfter {
		return nil, service.ErrOrganizationMemberRemovalInvariant
	}
	result := &service.OrganizationMemberRemovalResult{
		OperationID:          operationEntity.OperationID,
		CorrelationID:        operationEntity.CorrelationID,
		OrganizationID:       operationEntity.OrganizationID,
		UserID:               *allocationPosting.TargetUserIDSnapshot,
		AllocationID:         *allocationPosting.AllocationIDSnapshot,
		OperationType:        operationEntity.OperationType,
		Currency:             operationEntity.Currency,
		Outcome:              service.OrganizationAllocationOutcomeCommitted,
		Reclaimed:            poolPosting.DeltaAmount.StringFixed(domain.OrganizationMoneyScale),
		PoolBefore:           poolPosting.BeforeAmount.StringFixed(domain.OrganizationMoneyScale),
		PoolAfter:            poolPosting.AfterAmount.StringFixed(domain.OrganizationMoneyScale),
		BalanceVersionBefore: poolPosting.BalanceVersionBefore,
		BalanceVersionAfter:  poolPosting.BalanceVersionAfter,
		CanonicalPayloadHash: operationEntity.CanonicalPayloadHash,
	}
	return result, service.ValidateOrganizationMemberRemovalResult(result)
}

func createMemberRemovalLedgerOperation(ctx context.Context, client *dbent.Client, command service.OrganizationMemberRemovalCommand, resultSnapshot json.RawMessage) (*dbent.OrganizationLedgerOperation, error) {
	builder := client.OrganizationLedgerOperation.Create().
		SetOperationID(command.OperationID).
		SetCorrelationID(command.CorrelationID).
		SetOrganizationID(command.OrganizationID).
		SetOperationType(domain.OrganizationFundingOperationMemberRemovalReclaim).
		SetAuthorizationScope(command.AuthorizationScope).
		SetActorIDSnapshot(command.Actor.UserID).
		SetActorRefID(command.Actor.UserID).
		SetActorDisplaySnapshot(strings.TrimSpace(command.Actor.DisplayName)).
		SetActorRoleSnapshot(memberRemovalActorRoleSnapshot(command.Actor)).
		SetCurrency(domain.OrganizationCurrencyUSD).
		SetIdempotencyKeyHash(command.IdempotencyKeyHash).
		SetCanonicalPayloadHash(command.CanonicalPayloadHash).
		SetInternalReason(command.InternalReason).
		SetOccurredAt(command.OccurredAt.UTC())
	if publicReason := strings.TrimSpace(command.PublicReason); publicReason != "" {
		builder.SetPublicReason(publicReason)
	}
	if len(resultSnapshot) != 0 {
		builder.SetNoOpResultSnapshot(resultSnapshot)
	}
	operationEntity, err := builder.Save(ctx)
	if err != nil {
		if isUniqueConstraintViolation(err) {
			return nil, service.ErrIdempotencyKeyConflict.WithCause(err)
		}
		return nil, fmt.Errorf("create member removal ledger operation: %w", err)
	}
	return operationEntity, nil
}

func validateMemberRemovalCommand(command service.OrganizationMemberRemovalCommand) error {
	if err := service.ValidateOrganizationMemberRemovalActor(command.Actor, command.OrganizationID); err != nil {
		return err
	}
	if command.OrganizationID <= 0 || command.TargetUserID <= 0 ||
		command.OperationType != domain.OrganizationFundingOperationMemberRemovalReclaim ||
		command.AuthorizationScope != command.Actor.AuthorizationScope ||
		!validSHA256(command.IdempotencyKeyHash) || !validSHA256(command.CanonicalPayloadHash) ||
		strings.TrimSpace(command.InternalReason) == "" || command.OperationID == "" || command.CorrelationID == "" || command.OccurredAt.IsZero() {
		return service.ErrOrganizationMemberRemovalInvalid
	}
	return nil
}

func memberRemovalLifecycleAllowed(status, authorizationScope string) bool {
	if authorizationScope == domain.OrganizationFundingAuthorizationPlatformAdminJWT {
		return status == domain.OrganizationStatusActive || status == domain.OrganizationStatusDisabled || status == domain.OrganizationStatusArchived
	}
	return authorizationScope == domain.OrganizationFundingAuthorizationOrganizationJWT && status == domain.OrganizationStatusActive
}

func persistedMemberRemovalActorAllowed(userEntity *dbent.User, actor service.OrganizationMemberRemovalActor, organizationID int64) bool {
	if userEntity == nil || userEntity.ID != actor.UserID || userEntity.Status != domain.StatusActive {
		return false
	}
	if actor.AuthorizationScope == domain.OrganizationFundingAuthorizationPlatformAdminJWT {
		return userEntity.Role == domain.RoleAdmin
	}
	return userEntity.OrganizationID != nil && *userEntity.OrganizationID == organizationID &&
		userEntity.OrganizationRole != nil && *userEntity.OrganizationRole == domain.OrganizationRoleAdmin
}

func memberRemovalActorRoleSnapshot(actor service.OrganizationMemberRemovalActor) string {
	if actor.AuthorizationScope == domain.OrganizationFundingAuthorizationPlatformAdminJWT {
		return domain.RoleAdmin
	}
	return domain.OrganizationRoleAdmin
}

func isMemberRemovalLockUnavailable(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == pq.ErrorCode("55P03")
}

func (r *organizationMembershipRepository) withMemberTransaction(ctx context.Context, operation func(context.Context, *dbent.Client) (*service.OrganizationMember, error)) (*service.OrganizationMember, error) {
	if existing := dbent.TxFromContext(ctx); existing != nil {
		return operation(ctx, existing.Client())
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	member, err := operation(txCtx, tx.Client())
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return member, nil
}

func memberFromLockedEntities(userEntity *dbent.User, allocation *dbent.OrganizationAllocation) (*service.OrganizationMember, error) {
	if userEntity == nil || allocation == nil || userEntity.OrganizationRole == nil {
		return nil, service.ErrOrganizationMembershipInconsistent
	}
	member := organizationMemberFromEntities(userEntity, allocation)
	return &member, nil
}

func organizationMemberFromEntities(userEntity *dbent.User, allocation *dbent.OrganizationAllocation) service.OrganizationMember {
	role := ""
	if userEntity.OrganizationRole != nil {
		role = *userEntity.OrganizationRole
	}
	return service.OrganizationMember{
		UserID:           userEntity.ID,
		Email:            userEntity.Email,
		Username:         userEntity.Username,
		Status:           userEntity.Status,
		OrganizationRole: role,
		Allocation: service.OrganizationMemberAllocation{
			ID:        allocation.ID,
			Status:    allocation.Status,
			Available: allocation.Available.StringFixed(8),
			Reserved:  allocation.Reserved.StringFixed(8),
		},
	}
}

func translateMembershipWriteError(err error) error {
	if err == nil {
		return nil
	}
	if isUniqueConstraintViolation(err) {
		return service.ErrOrganizationMemberIneligible.WithCause(err)
	}
	var validationError *dbent.ValidationError
	if errors.As(err, &validationError) {
		return service.ErrOrganizationMembershipInconsistent.WithCause(err)
	}
	return fmt.Errorf("persist organization membership: %w", err)
}
