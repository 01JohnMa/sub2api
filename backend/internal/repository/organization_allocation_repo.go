package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/organization"
	"github.com/Wei-Shaw/sub2api/ent/organizationallocation"
	"github.com/Wei-Shaw/sub2api/ent/organizationledgeroperation"
	"github.com/Wei-Shaw/sub2api/ent/organizationledgerposting"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"

	"entgo.io/ent/dialect"
)

type organizationAllocationRepository struct {
	client *dbent.Client
}

func NewOrganizationAllocationRepository(client *dbent.Client) service.OrganizationAllocationRepository {
	return &organizationAllocationRepository{client: client}
}

func (r *organizationAllocationRepository) LoadPreviewSnapshot(ctx context.Context, actor service.OrganizationAllocationActor, organizationID int64, operationType string) (*service.OrganizationAllocationSnapshot, error) {
	if r == nil || r.client == nil {
		return nil, service.ErrOrganizationAllocationRepositoryUnavailable
	}
	if err := service.ValidateOrganizationAllocationActor(actor, organizationID, operationType); err != nil {
		return nil, err
	}
	if existing := dbent.TxFromContext(ctx); existing != nil {
		return r.loadAuthorizedSnapshot(ctx, existing.Client(), actor, organizationID, operationType, false)
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, allocationStorageError("begin allocation preview", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	snapshot, err := r.loadAuthorizedSnapshot(txCtx, tx.Client(), actor, organizationID, operationType, false)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, allocationStorageError("commit allocation preview", err)
	}
	return snapshot, nil
}

func (r *organizationAllocationRepository) FindCommitted(ctx context.Context, query service.OrganizationAllocationCommittedQuery) (*service.OrganizationAllocationResult, error) {
	if r == nil || r.client == nil {
		return nil, service.ErrOrganizationAllocationRepositoryUnavailable
	}
	if err := validateAllocationCommittedQuery(query); err != nil {
		return nil, err
	}
	if existing := dbent.TxFromContext(ctx); existing != nil {
		return r.findAuthorizedCommitted(ctx, existing.Client(), query)
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, allocationStorageError("begin allocation replay lookup", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	result, err := r.findAuthorizedCommitted(txCtx, tx.Client(), query)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, allocationStorageError("commit allocation replay lookup", err)
	}
	return result, nil
}

func (r *organizationAllocationRepository) Execute(ctx context.Context, command service.OrganizationAllocationExecuteCommand) (*service.OrganizationAllocationResult, error) {
	if r == nil || r.client == nil {
		return nil, service.ErrOrganizationAllocationRepositoryUnavailable
	}
	if err := validateAllocationExecuteCommand(command); err != nil {
		return nil, err
	}
	if existing := dbent.TxFromContext(ctx); existing != nil {
		return r.executeInTx(ctx, existing.Client(), command)
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, allocationStorageError("begin allocation execution", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	result, err := r.executeInTx(txCtx, tx.Client(), command)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, allocationStorageError("commit allocation execution", err)
	}
	return result, nil
}

func (r *organizationAllocationRepository) findAuthorizedCommitted(ctx context.Context, client *dbent.Client, query service.OrganizationAllocationCommittedQuery) (*service.OrganizationAllocationResult, error) {
	organizationEntity, err := lockAllocationOrganization(ctx, client, query.OrganizationID)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrOrganizationNotFound, nil)
	}
	if err := service.ValidateOrganizationAllocationLifecycle(organizationEntity.Status, query.Actor.AuthorizationScope, query.OperationType); err != nil {
		return nil, err
	}
	actorEntity, err := lockAllocationUsers(ctx, client, []int64{query.Actor.UserID})
	if err != nil {
		return nil, err
	}
	if len(actorEntity) != 1 || !persistedAllocationActorAllowed(actorEntity[0], query.Actor, query.OrganizationID) {
		return nil, service.ErrOrganizationAllocationForbidden
	}
	return findAllocationOperation(ctx, client, query.Actor.AuthorizationScope, query.OrganizationID, query.OperationType, query.IdempotencyKeyHash, query.CanonicalPayloadHash)
}

func (r *organizationAllocationRepository) executeInTx(ctx context.Context, client *dbent.Client, command service.OrganizationAllocationExecuteCommand) (*service.OrganizationAllocationResult, error) {
	organizationID := command.Confirmation.OrganizationID
	organizationEntity, err := lockAllocationOrganization(ctx, client, organizationID)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrOrganizationNotFound, nil)
	}
	if err := service.ValidateOrganizationAllocationLifecycle(organizationEntity.Status, command.Actor.AuthorizationScope, command.Confirmation.OperationType); err != nil {
		return nil, err
	}

	memberIDs, err := currentOrganizationMemberIDs(ctx, client, organizationID)
	if err != nil {
		return nil, allocationStorageError("list allocation member identities", err)
	}
	userIDs := append(memberIDs, command.Actor.UserID)
	lockedUsers, err := lockAllocationUsers(ctx, client, userIDs)
	if err != nil {
		return nil, err
	}
	actorEntity := userByID(lockedUsers, command.Actor.UserID)
	if !persistedAllocationActorAllowed(actorEntity, command.Actor, organizationID) {
		return nil, service.ErrOrganizationAllocationForbidden
	}

	existing, err := findAllocationOperation(
		ctx,
		client,
		command.Actor.AuthorizationScope,
		organizationID,
		command.Confirmation.OperationType,
		command.IdempotencyKeyHash,
		command.CanonicalPayloadHash,
	)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		existing.Replayed = true
		return existing, nil
	}

	allocations, err := lockOrganizationAllocations(ctx, client, organizationID)
	if err != nil {
		return nil, err
	}
	snapshot, err := allocationSnapshotFromEntities(organizationEntity, lockedUsers, allocations)
	if err != nil {
		return nil, err
	}
	if err := service.ValidateOrganizationAllocationConfirmation(snapshot, command.Actor, command.Confirmation); err != nil {
		return nil, err
	}
	if command.Confirmation.NoOpReason != "" {
		result := &service.OrganizationAllocationResult{
			OperationID:          command.OperationID,
			CorrelationID:        command.CorrelationID,
			OrganizationID:       organizationID,
			OperationType:        command.Confirmation.OperationType,
			Currency:             domain.OrganizationCurrencyUSD,
			Outcome:              service.OrganizationAllocationOutcomeNoOp,
			NoOpReason:           command.Confirmation.NoOpReason,
			PoolBefore:           command.Confirmation.PoolBefore,
			PoolAfter:            command.Confirmation.PoolAfter,
			TotalDelta:           command.Confirmation.TotalDelta,
			BalanceVersionBefore: command.Confirmation.BalanceVersion,
			BalanceVersionAfter:  command.Confirmation.BalanceVersion,
			CanonicalPayloadHash: command.CanonicalPayloadHash,
			Postings:             []service.OrganizationLedgerEntry{},
		}
		if err := service.ValidateOrganizationAllocationResult(result); err != nil {
			return nil, err
		}
		resultSnapshot, err := json.Marshal(result)
		if err != nil {
			return nil, service.ErrOrganizationAllocationInvariant.WithCause(err)
		}
		if _, err := createAllocationLedgerOperation(ctx, client, command, resultSnapshot); err != nil {
			return nil, err
		}
		return result, nil
	}
	if organizationEntity.BalanceVersion == math.MaxInt64 {
		return nil, service.ErrOrganizationAllocationInvariant
	}
	poolBefore, err := decimal.NewFromString(command.Confirmation.PoolBefore)
	if err != nil {
		return nil, service.ErrOrganizationAllocationInvariant.WithCause(err)
	}
	poolAfter, err := decimal.NewFromString(command.Confirmation.PoolAfter)
	if err != nil {
		return nil, service.ErrOrganizationAllocationInvariant.WithCause(err)
	}
	versionBefore := organizationEntity.BalanceVersion
	versionAfter := versionBefore + 1
	updatedOrganization, err := client.Organization.UpdateOneID(organizationID).
		Where(organization.BalanceVersionEQ(versionBefore)).
		SetUnallocatedPool(poolAfter).
		SetBalanceVersion(versionAfter).
		Save(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, service.ErrOrganizationAllocationStalePreview
		}
		return nil, allocationStorageError("update organization allocation pool", err)
	}

	allocationByID := make(map[int64]*dbent.OrganizationAllocation, len(allocations))
	for _, allocation := range allocations {
		allocationByID[allocation.ID] = allocation
	}
	for _, target := range command.Confirmation.Targets {
		allocation := allocationByID[target.AllocationID]
		if allocation == nil || allocation.UserIDSnapshot != target.UserID {
			return nil, service.ErrOrganizationAllocationStalePreview
		}
		after, err := decimal.NewFromString(target.After)
		if err != nil {
			return nil, service.ErrOrganizationAllocationInvariant.WithCause(err)
		}
		if _, err := client.OrganizationAllocation.UpdateOneID(target.AllocationID).
			Where(
				organizationallocation.OrganizationIDEQ(organizationID),
				organizationallocation.UserIDSnapshotEQ(target.UserID),
				organizationallocation.StatusEQ(domain.OrganizationAllocationStatusActive),
			).
			SetAvailable(after).
			AddVersion(1).
			Save(ctx); err != nil {
			if dbent.IsNotFound(err) {
				return nil, service.ErrOrganizationAllocationStalePreview
			}
			return nil, allocationStorageError("update member allocation", err)
		}
	}

	operationEntity, err := createAllocationLedgerOperation(ctx, client, command, nil)
	if err != nil {
		return nil, err
	}

	postings := make([]service.OrganizationLedgerEntry, 0, len(command.Confirmation.Targets)+1)
	poolPosting, err := createAllocationPosting(ctx, client, allocationPostingInput{
		Operation:     operationEntity,
		Sequence:      1,
		BucketType:    domain.OrganizationLedgerBucketUnallocated,
		BucketID:      organizationID,
		Before:        poolBefore,
		Delta:         poolAfter.Sub(poolBefore),
		After:         poolAfter,
		VersionBefore: versionBefore,
		VersionAfter:  versionAfter,
	})
	if err != nil {
		return nil, err
	}
	postings = append(postings, allocationLedgerEntry(operationEntity, poolPosting))

	for index, target := range command.Confirmation.Targets {
		before, err := decimal.NewFromString(target.Before)
		if err != nil {
			return nil, service.ErrOrganizationAllocationInvariant.WithCause(err)
		}
		delta, err := decimal.NewFromString(target.Delta)
		if err != nil {
			return nil, service.ErrOrganizationAllocationInvariant.WithCause(err)
		}
		after, err := decimal.NewFromString(target.After)
		if err != nil {
			return nil, service.ErrOrganizationAllocationInvariant.WithCause(err)
		}
		posting, err := createAllocationPosting(ctx, client, allocationPostingInput{
			Operation:     operationEntity,
			Sequence:      index + 2,
			BucketType:    domain.OrganizationLedgerBucketAllocationAvailable,
			BucketID:      target.AllocationID,
			AllocationID:  &target.AllocationID,
			TargetUserID:  &target.UserID,
			Before:        before,
			Delta:         delta,
			After:         after,
			VersionBefore: versionBefore,
			VersionAfter:  versionAfter,
		})
		if err != nil {
			return nil, err
		}
		postings = append(postings, allocationLedgerEntry(operationEntity, posting))
	}

	result := &service.OrganizationAllocationResult{
		OperationID:          operationEntity.OperationID,
		CorrelationID:        operationEntity.CorrelationID,
		OrganizationID:       updatedOrganization.ID,
		OperationType:        operationEntity.OperationType,
		Currency:             operationEntity.Currency,
		Outcome:              service.OrganizationAllocationOutcomeCommitted,
		PoolBefore:           command.Confirmation.PoolBefore,
		PoolAfter:            command.Confirmation.PoolAfter,
		TotalDelta:           command.Confirmation.TotalDelta,
		BalanceVersionBefore: versionBefore,
		BalanceVersionAfter:  versionAfter,
		CanonicalPayloadHash: command.CanonicalPayloadHash,
		Postings:             postings,
	}
	if err := service.ValidateOrganizationAllocationResult(result); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *organizationAllocationRepository) loadAuthorizedSnapshot(ctx context.Context, client *dbent.Client, actor service.OrganizationAllocationActor, organizationID int64, operationType string, lock bool) (*service.OrganizationAllocationSnapshot, error) {
	organizationQuery := client.Organization.Query().Where(organization.IDEQ(organizationID))
	if lock && client.Driver().Dialect() == dialect.Postgres {
		organizationQuery = organizationQuery.ForUpdate()
	}
	organizationEntity, err := organizationQuery.Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrOrganizationNotFound, nil)
	}
	if err := service.ValidateOrganizationAllocationLifecycle(organizationEntity.Status, actor.AuthorizationScope, operationType); err != nil {
		return nil, err
	}
	actorEntity, err := client.User.Query().
		Where(dbuser.IDEQ(actor.UserID), dbuser.DeletedAtIsNil()).
		Only(ctx)
	if err != nil || !persistedAllocationActorAllowed(actorEntity, actor, organizationID) {
		return nil, service.ErrOrganizationAllocationForbidden
	}
	members, err := client.User.Query().
		Where(dbuser.OrganizationIDEQ(organizationID), dbuser.DeletedAtIsNil()).
		Order(dbent.Asc(dbuser.FieldID)).
		All(ctx)
	if err != nil {
		return nil, allocationStorageError("read organization allocation members", err)
	}
	allocations, err := client.OrganizationAllocation.Query().
		Where(
			organizationallocation.OrganizationIDEQ(organizationID),
			organizationallocation.StatusEQ(domain.OrganizationAllocationStatusActive),
		).
		Order(dbent.Asc(organizationallocation.FieldUserIDSnapshot)).
		All(ctx)
	if err != nil {
		return nil, allocationStorageError("read organization allocations", err)
	}
	return allocationSnapshotFromEntities(organizationEntity, members, allocations)
}

func lockAllocationOrganization(ctx context.Context, client *dbent.Client, organizationID int64) (*dbent.Organization, error) {
	query := client.Organization.Query().Where(organization.IDEQ(organizationID))
	if client.Driver().Dialect() == dialect.Postgres {
		query = query.ForUpdate()
	}
	return query.Only(ctx)
}

func currentOrganizationMemberIDs(ctx context.Context, client *dbent.Client, organizationID int64) ([]int64, error) {
	return client.User.Query().
		Where(dbuser.OrganizationIDEQ(organizationID), dbuser.DeletedAtIsNil()).
		Order(dbent.Asc(dbuser.FieldID)).
		IDs(ctx)
}

func lockAllocationUsers(ctx context.Context, client *dbent.Client, userIDs []int64) ([]*dbent.User, error) {
	unique := make(map[int64]struct{}, len(userIDs))
	ids := make([]int64, 0, len(userIDs))
	for _, id := range userIDs {
		if id <= 0 {
			continue
		}
		if _, exists := unique[id]; exists {
			continue
		}
		unique[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return []*dbent.User{}, nil
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	query := client.User.Query().
		Where(dbuser.IDIn(ids...), dbuser.DeletedAtIsNil()).
		Order(dbent.Asc(dbuser.FieldID))
	if client.Driver().Dialect() == dialect.Postgres {
		query = query.ForUpdate()
	}
	users, err := query.All(ctx)
	if err != nil {
		return nil, allocationStorageError("lock allocation users", err)
	}
	return users, nil
}

func lockOrganizationAllocations(ctx context.Context, client *dbent.Client, organizationID int64) ([]*dbent.OrganizationAllocation, error) {
	query := client.OrganizationAllocation.Query().
		Where(
			organizationallocation.OrganizationIDEQ(organizationID),
			organizationallocation.StatusEQ(domain.OrganizationAllocationStatusActive),
		).
		Order(dbent.Asc(organizationallocation.FieldUserIDSnapshot), dbent.Asc(organizationallocation.FieldID))
	if client.Driver().Dialect() == dialect.Postgres {
		query = query.ForUpdate()
	}
	allocations, err := query.All(ctx)
	if err != nil {
		return nil, allocationStorageError("lock organization allocations", err)
	}
	return allocations, nil
}

func allocationSnapshotFromEntities(organizationEntity *dbent.Organization, users []*dbent.User, allocations []*dbent.OrganizationAllocation) (*service.OrganizationAllocationSnapshot, error) {
	if organizationEntity == nil {
		return nil, service.ErrOrganizationAllocationInvariant
	}
	allocationByUser := make(map[int64]*dbent.OrganizationAllocation, len(allocations))
	for _, allocation := range allocations {
		if allocation == nil || allocation.UserRefID == nil || *allocation.UserRefID != allocation.UserIDSnapshot || allocation.OrganizationID != organizationEntity.ID {
			return nil, service.ErrOrganizationAllocationInvariant
		}
		if _, duplicate := allocationByUser[allocation.UserIDSnapshot]; duplicate {
			return nil, service.ErrOrganizationAllocationInvariant
		}
		allocationByUser[allocation.UserIDSnapshot] = allocation
	}
	members := make([]service.OrganizationAllocationMemberSnapshot, 0)
	for _, userEntity := range users {
		if userEntity == nil || userEntity.OrganizationID == nil || *userEntity.OrganizationID != organizationEntity.ID {
			continue
		}
		if userEntity.OrganizationRole == nil {
			return nil, service.ErrOrganizationAllocationInvariant
		}
		allocation := allocationByUser[userEntity.ID]
		if allocation == nil {
			return nil, service.ErrOrganizationAllocationInvariant
		}
		members = append(members, service.OrganizationAllocationMemberSnapshot{
			UserID:           userEntity.ID,
			UserStatus:       userEntity.Status,
			OrganizationRole: *userEntity.OrganizationRole,
			AllocationID:     allocation.ID,
			AllocationStatus: allocation.Status,
			Available:        allocation.Available.StringFixed(domain.OrganizationMoneyScale),
			Reserved:         allocation.Reserved.StringFixed(domain.OrganizationMoneyScale),
		})
		delete(allocationByUser, userEntity.ID)
	}
	if len(allocationByUser) != 0 {
		return nil, service.ErrOrganizationAllocationInvariant
	}
	sort.Slice(members, func(i, j int) bool { return members[i].UserID < members[j].UserID })
	return &service.OrganizationAllocationSnapshot{
		OrganizationID: organizationEntity.ID,
		Status:         organizationEntity.Status,
		OrganizationAt: organizationEntity.UpdatedAt,
		BalanceVersion: organizationEntity.BalanceVersion,
		Unallocated:    organizationEntity.UnallocatedPool.StringFixed(domain.OrganizationMoneyScale),
		Members:        members,
	}, nil
}

func persistedAllocationActorAllowed(userEntity *dbent.User, actor service.OrganizationAllocationActor, organizationID int64) bool {
	if userEntity == nil || userEntity.ID != actor.UserID || userEntity.Status != domain.StatusActive {
		return false
	}
	if actor.AuthorizationScope == domain.OrganizationFundingAuthorizationPlatformAdminJWT {
		return userEntity.Role == domain.RoleAdmin
	}
	return userEntity.OrganizationID != nil && *userEntity.OrganizationID == organizationID &&
		userEntity.OrganizationRole != nil && *userEntity.OrganizationRole == domain.OrganizationRoleAdmin
}

func userByID(users []*dbent.User, id int64) *dbent.User {
	for _, userEntity := range users {
		if userEntity != nil && userEntity.ID == id {
			return userEntity
		}
	}
	return nil
}

func findAllocationOperation(ctx context.Context, client *dbent.Client, scope string, organizationID int64, operationType, idempotencyKeyHash, canonicalPayloadHash string) (*service.OrganizationAllocationResult, error) {
	operationEntity, err := client.OrganizationLedgerOperation.Query().
		Where(
			organizationledgeroperation.AuthorizationScopeEQ(scope),
			organizationledgeroperation.OrganizationIDEQ(organizationID),
			organizationledgeroperation.OperationTypeEQ(operationType),
			organizationledgeroperation.IdempotencyKeyHashEQ(idempotencyKeyHash),
		).
		WithPostings(func(query *dbent.OrganizationLedgerPostingQuery) {
			query.Order(dbent.Asc(organizationledgerposting.FieldPostingSequence))
		}).
		Only(ctx)
	if dbent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, allocationStorageError("read allocation idempotency record", err)
	}
	if operationEntity.CanonicalPayloadHash != canonicalPayloadHash {
		return nil, service.ErrIdempotencyKeyConflict
	}
	return allocationResultFromOperation(operationEntity)
}

func allocationResultFromOperation(operationEntity *dbent.OrganizationLedgerOperation) (*service.OrganizationAllocationResult, error) {
	if operationEntity == nil {
		return nil, service.ErrOrganizationAllocationInvariant
	}
	postingEntities, err := operationEntity.Edges.PostingsOrErr()
	if err != nil {
		return nil, service.ErrOrganizationAllocationInvariant.WithCause(err)
	}
	if len(operationEntity.NoOpResultSnapshot) != 0 {
		if len(postingEntities) != 0 {
			return nil, service.ErrOrganizationAllocationInvariant
		}
		var result service.OrganizationAllocationResult
		if err := json.Unmarshal(operationEntity.NoOpResultSnapshot, &result); err != nil {
			return nil, service.ErrOrganizationAllocationInvariant.WithCause(err)
		}
		if result.OperationID != operationEntity.OperationID ||
			result.CorrelationID != operationEntity.CorrelationID ||
			result.OrganizationID != operationEntity.OrganizationID ||
			result.OperationType != operationEntity.OperationType ||
			result.Currency != operationEntity.Currency ||
			result.CanonicalPayloadHash != operationEntity.CanonicalPayloadHash {
			return nil, service.ErrOrganizationAllocationInvariant
		}
		if err := service.ValidateOrganizationAllocationResult(&result); err != nil {
			return nil, err
		}
		return &result, nil
	}
	if len(postingEntities) < 2 {
		return nil, service.ErrOrganizationAllocationInvariant
	}
	postings := make([]service.OrganizationLedgerEntry, 0, len(postingEntities))
	var poolBefore, poolAfter string
	totalDelta := decimal.Zero
	versionBefore := postingEntities[0].BalanceVersionBefore
	versionAfter := postingEntities[0].BalanceVersionAfter
	for _, posting := range postingEntities {
		postings = append(postings, allocationLedgerEntry(operationEntity, posting))
		if posting.BucketType == domain.OrganizationLedgerBucketUnallocated {
			poolBefore = posting.BeforeAmount.StringFixed(domain.OrganizationMoneyScale)
			poolAfter = posting.AfterAmount.StringFixed(domain.OrganizationMoneyScale)
		} else if posting.BucketType == domain.OrganizationLedgerBucketAllocationAvailable {
			totalDelta = totalDelta.Add(posting.DeltaAmount)
		}
		if posting.BalanceVersionBefore != versionBefore || posting.BalanceVersionAfter != versionAfter {
			return nil, service.ErrOrganizationAllocationInvariant
		}
	}
	result := &service.OrganizationAllocationResult{
		OperationID:          operationEntity.OperationID,
		CorrelationID:        operationEntity.CorrelationID,
		OrganizationID:       operationEntity.OrganizationID,
		OperationType:        operationEntity.OperationType,
		Currency:             operationEntity.Currency,
		Outcome:              service.OrganizationAllocationOutcomeCommitted,
		PoolBefore:           poolBefore,
		PoolAfter:            poolAfter,
		TotalDelta:           totalDelta.StringFixed(domain.OrganizationMoneyScale),
		BalanceVersionBefore: versionBefore,
		BalanceVersionAfter:  versionAfter,
		CanonicalPayloadHash: operationEntity.CanonicalPayloadHash,
		Postings:             postings,
	}
	if err := service.ValidateOrganizationAllocationResult(result); err != nil {
		return nil, err
	}
	return result, nil
}

func createAllocationLedgerOperation(ctx context.Context, client *dbent.Client, command service.OrganizationAllocationExecuteCommand, noOpResultSnapshot json.RawMessage) (*dbent.OrganizationLedgerOperation, error) {
	publicReason := optionalAllocationReason(command.Confirmation.PublicReason)
	builder := client.OrganizationLedgerOperation.Create().
		SetOperationID(command.OperationID).
		SetCorrelationID(command.CorrelationID).
		SetOrganizationID(command.Confirmation.OrganizationID).
		SetOperationType(command.Confirmation.OperationType).
		SetAuthorizationScope(command.Actor.AuthorizationScope).
		SetActorIDSnapshot(command.Actor.UserID).
		SetActorRefID(command.Actor.UserID).
		SetActorDisplaySnapshot(strings.TrimSpace(command.Actor.DisplayName)).
		SetActorRoleSnapshot(allocationActorRoleSnapshot(command.Actor)).
		SetCurrency(domain.OrganizationCurrencyUSD).
		SetIdempotencyKeyHash(command.IdempotencyKeyHash).
		SetCanonicalPayloadHash(command.CanonicalPayloadHash).
		SetNillablePublicReason(publicReason).
		SetInternalReason(command.InternalReason).
		SetOccurredAt(command.OccurredAt.UTC())
	if len(noOpResultSnapshot) != 0 {
		builder.SetNoOpResultSnapshot(noOpResultSnapshot)
	}
	operationEntity, err := builder.Save(ctx)
	if err != nil {
		if isUniqueConstraintViolation(err) {
			return nil, service.ErrIdempotencyKeyConflict.WithCause(err)
		}
		return nil, allocationStorageError("create allocation ledger operation", err)
	}
	return operationEntity, nil
}

type allocationPostingInput struct {
	Operation     *dbent.OrganizationLedgerOperation
	Sequence      int
	BucketType    string
	BucketID      int64
	AllocationID  *int64
	TargetUserID  *int64
	Before        decimal.Decimal
	Delta         decimal.Decimal
	After         decimal.Decimal
	VersionBefore int64
	VersionAfter  int64
}

func createAllocationPosting(ctx context.Context, client *dbent.Client, input allocationPostingInput) (*dbent.OrganizationLedgerPosting, error) {
	if input.Operation == nil || input.Delta.IsZero() || !input.Before.Add(input.Delta).Equal(input.After) {
		return nil, service.ErrOrganizationAllocationInvariant
	}
	builder := client.OrganizationLedgerPosting.Create().
		SetOperationRefID(input.Operation.ID).
		SetOperationIDSnapshot(input.Operation.OperationID).
		SetOrganizationID(input.Operation.OrganizationID).
		SetPostingSequence(input.Sequence).
		SetBucketType(input.BucketType).
		SetBucketIDSnapshot(input.BucketID).
		SetNillableAllocationIDSnapshot(input.AllocationID).
		SetNillableAllocationRefID(input.AllocationID).
		SetNillableTargetUserIDSnapshot(input.TargetUserID).
		SetNillableTargetUserRefID(input.TargetUserID).
		SetCurrency(domain.OrganizationCurrencyUSD).
		SetBeforeAmount(input.Before).
		SetDeltaAmount(input.Delta).
		SetAfterAmount(input.After).
		SetBalanceVersionBefore(input.VersionBefore).
		SetBalanceVersionAfter(input.VersionAfter).
		SetOccurredAt(input.Operation.OccurredAt)
	posting, err := builder.Save(ctx)
	if err != nil {
		return nil, allocationStorageError("create allocation ledger posting", err)
	}
	return posting, nil
}

func allocationLedgerEntry(operationEntity *dbent.OrganizationLedgerOperation, posting *dbent.OrganizationLedgerPosting) service.OrganizationLedgerEntry {
	return service.OrganizationLedgerEntry{
		PostingID:            posting.ID,
		PostingSequence:      posting.PostingSequence,
		OperationID:          operationEntity.OperationID,
		CorrelationID:        operationEntity.CorrelationID,
		OrganizationID:       operationEntity.OrganizationID,
		OperationType:        operationEntity.OperationType,
		AuthorizationScope:   operationEntity.AuthorizationScope,
		ActorIDSnapshot:      operationEntity.ActorIDSnapshot,
		ActorDisplaySnapshot: operationEntity.ActorDisplaySnapshot,
		ActorRoleSnapshot:    operationEntity.ActorRoleSnapshot,
		Currency:             posting.Currency,
		IdempotencyKeyHash:   operationEntity.IdempotencyKeyHash,
		CanonicalPayloadHash: operationEntity.CanonicalPayloadHash,
		PublicReason:         cloneOptionalString(operationEntity.PublicReason),
		InternalReason:       operationEntity.InternalReason,
		BucketType:           posting.BucketType,
		BucketIDSnapshot:     posting.BucketIDSnapshot,
		AllocationIDSnapshot: cloneOptionalInt64(posting.AllocationIDSnapshot),
		TargetUserIDSnapshot: cloneOptionalInt64(posting.TargetUserIDSnapshot),
		BeforeAmount:         posting.BeforeAmount.StringFixed(domain.OrganizationMoneyScale),
		DeltaAmount:          posting.DeltaAmount.StringFixed(domain.OrganizationMoneyScale),
		AfterAmount:          posting.AfterAmount.StringFixed(domain.OrganizationMoneyScale),
		BalanceVersionBefore: posting.BalanceVersionBefore,
		BalanceVersionAfter:  posting.BalanceVersionAfter,
		OccurredAt:           posting.OccurredAt,
		CreatedAt:            posting.CreatedAt,
	}
}

func validateAllocationCommittedQuery(query service.OrganizationAllocationCommittedQuery) error {
	if err := service.ValidateOrganizationAllocationActor(query.Actor, query.OrganizationID, query.OperationType); err != nil {
		return err
	}
	if !validSHA256(query.IdempotencyKeyHash) || !validSHA256(query.CanonicalPayloadHash) {
		return service.ErrIdempotencyKeyInvalid
	}
	return nil
}

func validateAllocationExecuteCommand(command service.OrganizationAllocationExecuteCommand) error {
	if err := service.ValidateOrganizationAllocationActor(command.Actor, command.Confirmation.OrganizationID, command.Confirmation.OperationType); err != nil {
		return err
	}
	if !validSHA256(command.IdempotencyKeyHash) || !validSHA256(command.CanonicalPayloadHash) || strings.TrimSpace(command.InternalReason) == "" {
		return service.ErrOrganizationAllocationInvalid
	}
	if command.Confirmation.Currency != domain.OrganizationCurrencyUSD || command.Confirmation.MoneyScale != domain.OrganizationMoneyScale || command.Confirmation.AuthorizationScope != command.Actor.AuthorizationScope {
		return service.ErrOrganizationAllocationStalePreview
	}
	if command.OperationID == "" || command.CorrelationID == "" || command.OccurredAt.IsZero() {
		return service.ErrOrganizationAllocationInvariant
	}
	return nil
}

func allocationActorRoleSnapshot(actor service.OrganizationAllocationActor) string {
	if actor.AuthorizationScope == domain.OrganizationFundingAuthorizationPlatformAdminJWT {
		return domain.RoleAdmin
	}
	return domain.OrganizationRoleAdmin
}

func optionalAllocationReason(value string) *string {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return nil
	}
	return &normalized
}

func cloneOptionalInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func allocationStorageError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return service.ErrOrganizationAllocationRepositoryUnavailable.WithCause(fmt.Errorf("%s: %w", operation, err))
}

var _ service.OrganizationAllocationRepository = (*organizationAllocationRepository)(nil)
