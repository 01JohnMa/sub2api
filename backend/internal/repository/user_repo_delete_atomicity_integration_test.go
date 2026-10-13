//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type oauthRollbackSettingRepository map[string]string

func (r oauthRollbackSettingRepository) Get(_ context.Context, key string) (*service.Setting, error) {
	value, ok := r[key]
	if !ok {
		return nil, service.ErrSettingNotFound
	}
	return &service.Setting{Key: key, Value: value}, nil
}

func (r oauthRollbackSettingRepository) GetValue(_ context.Context, key string) (string, error) {
	value, ok := r[key]
	if !ok {
		return "", service.ErrSettingNotFound
	}
	return value, nil
}

func (r oauthRollbackSettingRepository) Set(_ context.Context, key, value string) error {
	r[key] = value
	return nil
}

func (r oauthRollbackSettingRepository) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r[key]; ok {
			values[key] = value
		}
	}
	return values, nil
}

func (r oauthRollbackSettingRepository) SetMultiple(_ context.Context, values map[string]string) error {
	for key, value := range values {
		r[key] = value
	}
	return nil
}

func (r oauthRollbackSettingRepository) GetAll(context.Context) (map[string]string, error) {
	values := make(map[string]string, len(r))
	for key, value := range r {
		values[key] = value
	}
	return values, nil
}

func (r oauthRollbackSettingRepository) Delete(_ context.Context, key string) error {
	delete(r, key)
	return nil
}

// TestUserRepository_DeleteUser_AtomicWithAPIKeys 复现 AdminService.DeleteUser 的事务编排场景：
// 把"删 API Key"(apiKeyRepo.DeleteWithAudit) 与"删 User"(userRepo.Delete) 放进同一个外部事务时，
// userRepo.Delete 必须复用 context 中的事务，而不是用 base client 自起一个独立事务并提前提交。
//
// 用例用"回滚外层事务"来模拟 commit 失败 / 中止：
//   - 修复前：userRepo.Delete 用 base client 自起独立事务并 commit，回滚外层事务后用户仍被删除，
//     而 API Key 随外层事务回滚 → Case 1 断言失败，暴露原子性缺陷（即 issue #3021 的不可恢复状态）。
//   - 修复后：两者落在同一事务，回滚后用户与 API Key 一起恢复。
//
// 关键点：repo 必须用 base client 构造（NewUserRepository/NewAPIKeyRepository），并由本测试手动
// 开启外层事务，这与生产环境 wire 注入的方式一致；不能复用 APIKeyRepoSuite 的 testEntTx
// （那会让 repo 持有 tx client，走的是另一条 ErrTxStarted 复用路径，无法覆盖本场景）。
func TestUserRepository_DeleteUser_AtomicWithAPIKeys(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)

	userRepo := NewUserRepository(client, integrationDB)
	apiKeyRepo := NewAPIKeyRepository(client, integrationDB)

	// 已提交的初始数据：1 个用户 + 2 个 active API Key。
	user := mustCreateUser(t, client, &service.User{})
	key1 := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: fmt.Sprintf("sk-atomic-a-%d", user.ID)})
	key2 := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: fmt.Sprintf("sk-atomic-b-%d", user.ID)})

	t.Cleanup(func() {
		// testEntClient 的写入不会自动回滚，best-effort 清理避免污染共享库。
		_, _ = integrationDB.Exec(`DELETE FROM deleted_api_key_audits WHERE user_id = $1`, user.ID)
		_, _ = integrationDB.Exec(`DELETE FROM api_keys WHERE user_id = $1`, user.ID)
		_, _ = integrationDB.Exec(`DELETE FROM users WHERE id = $1`, user.ID)
	})

	listParams := pagination.PaginationParams{Page: 1, PageSize: 10}

	// --- Case 1: 外层事务回滚 → 删 Key 与删 User 必须一起回滚 ---
	tx, err := client.Tx(ctx)
	require.NoError(t, err, "begin outer tx")
	opCtx := dbent.NewTxContext(ctx, tx)

	require.NoError(t, apiKeyRepo.DeleteWithAudit(opCtx, key1.ID))
	require.NoError(t, apiKeyRepo.DeleteWithAudit(opCtx, key2.ID))
	keysInTransaction, _, err := apiKeyRepo.ListByUserID(opCtx, user.ID, listParams, service.APIKeyListFilters{})
	require.NoError(t, err, "ListByUserID in transaction")
	require.Empty(t, keysInTransaction, "transaction-scoped list must observe uncommitted Key deletions")
	require.NoError(t, userRepo.Delete(opCtx, user.ID))

	require.NoError(t, tx.Rollback(), "rollback outer tx (模拟 commit 失败/中止)")

	// 已提交视图：不应有任何写入"逃逸"到独立已提交事务。
	gotUser, err := userRepo.GetByID(ctx, user.ID)
	require.NoError(t, err, "回滚后用户必须仍然存在（未被独立事务提前删除）")
	require.Equal(t, user.ID, gotUser.ID)

	keys, _, err := apiKeyRepo.ListByUserID(ctx, user.ID, listParams, service.APIKeyListFilters{})
	require.NoError(t, err, "ListByUserID")
	require.Len(t, keys, 2, "回滚后 2 个 API Key 必须仍为 active")

	var auditCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM deleted_api_key_audits WHERE user_id = $1`, user.ID).Scan(&auditCount))
	require.Zero(t, auditCount, "回滚后不应有已提交的审计行")

	// --- Case 2: 外层事务提交 → 删 Key 与删 User 一起生效 ---
	tx2, err := client.Tx(ctx)
	require.NoError(t, err, "begin outer tx #2")
	opCtx2 := dbent.NewTxContext(ctx, tx2)

	require.NoError(t, apiKeyRepo.DeleteWithAudit(opCtx2, key1.ID))
	require.NoError(t, apiKeyRepo.DeleteWithAudit(opCtx2, key2.ID))
	require.NoError(t, userRepo.Delete(opCtx2, user.ID))

	require.NoError(t, tx2.Commit(), "commit outer tx")

	_, err = userRepo.GetByID(ctx, user.ID)
	require.Error(t, err, "提交后用户应被软删除，GetByID 应返回未找到")

	keysAfter, _, err := apiKeyRepo.ListByUserID(ctx, user.ID, listParams, service.APIKeyListFilters{})
	require.NoError(t, err, "ListByUserID")
	require.Empty(t, keysAfter, "提交后 API Key 应全部被软删除")

	require.NoError(t, integrationDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM deleted_api_key_audits WHERE user_id = $1`, user.ID).Scan(&auditCount))
	require.Equal(t, 2, auditCount, "提交后应为每个被删 Key 写入一行审计")
}

func TestUserRepository_DeleteUser_RejectsCurrentOrganizationMembership(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUserRepositoryWithSQL(client, tx)

	member := mustCreateUser(t, client, &service.User{})
	organization, err := client.Organization.Create().
		SetName("Deletion Guard").
		SetCode(fmt.Sprintf("deletion-guard-%d", member.ID)).
		SetStatus(domain.OrganizationStatusActive).
		SetCreatedBy(member.ID).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.User.UpdateOneID(member.ID).
		SetOrganizationID(organization.ID).
		SetOrganizationRole(domain.OrganizationRoleMember).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.OrganizationAllocation.Create().
		SetOrganizationID(organization.ID).
		SetUserIDSnapshot(member.ID).
		SetUserRefID(member.ID).
		SetStatus(domain.OrganizationAllocationStatusActive).
		SetAvailable(decimal.Zero).
		SetReserved(decimal.Zero).
		Save(ctx)
	require.NoError(t, err)

	err = repo.Delete(ctx, member.ID)
	require.ErrorIs(t, err, service.ErrOrganizationMembershipMustBeRemoved)
	_, err = client.User.Get(ctx, member.ID)
	require.NoError(t, err)

	personalUser := mustCreateUser(t, client, &service.User{})
	_, err = client.OrganizationAllocation.Create().
		SetOrganizationID(organization.ID).
		SetUserIDSnapshot(personalUser.ID).
		SetUserRefID(personalUser.ID).
		SetStatus(domain.OrganizationAllocationStatusActive).
		SetAvailable(decimal.Zero).
		SetReserved(decimal.Zero).
		Save(ctx)
	require.NoError(t, err)

	err = repo.Delete(ctx, personalUser.ID)
	require.ErrorIs(t, err, service.ErrOrganizationMembershipMustBeRemoved)
	_, err = client.User.Get(ctx, personalUser.ID)
	require.NoError(t, err)
}

func TestUserRepository_DirectEntDeleteCannotBypassOrganizationGuard(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()

	member := mustCreateUser(t, client, &service.User{})
	organization, err := client.Organization.Create().
		SetName("Direct Delete Guard").
		SetCode(fmt.Sprintf("direct-delete-guard-%d", member.ID)).
		SetStatus(domain.OrganizationStatusActive).
		SetCreatedBy(member.ID).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.User.UpdateOneID(member.ID).
		SetOrganizationID(organization.ID).
		SetOrganizationRole(domain.OrganizationRoleMember).
		Save(ctx)
	require.NoError(t, err)

	err = client.User.DeleteOneID(member.ID).Exec(ctx)
	require.ErrorIs(t, err, mixins.ErrUserDeletionRequiresGuard)
	err = client.User.DeleteOneID(member.ID).Exec(mixins.SkipSoftDelete(ctx))
	require.ErrorIs(t, err, mixins.ErrUserDeletionRequiresGuard)
	_, err = client.User.UpdateOneID(member.ID).SetDeletedAt(time.Now()).Save(ctx)
	require.ErrorIs(t, err, mixins.ErrUserDeletionRequiresGuard)
	err = client.User.DeleteOneID(member.ID).Exec(mixins.AuthorizeUserDeletion(ctx))
	require.ErrorIs(t, err, dbent.ErrUserDeletionOrganizationMember)
	_, err = client.User.UpdateOneID(member.ID).
		SetDeletedAt(time.Now()).
		Save(mixins.AuthorizeUserDeletion(ctx))
	require.ErrorIs(t, err, dbent.ErrUserDeletionOrganizationMember)

	stored, err := client.User.Get(ctx, member.ID)
	require.NoError(t, err)
	require.Nil(t, stored.DeletedAt)

	personalUser := mustCreateUser(t, client, &service.User{})
	_, err = client.OrganizationAllocation.Create().
		SetOrganizationID(organization.ID).
		SetUserIDSnapshot(personalUser.ID).
		SetUserRefID(personalUser.ID).
		SetStatus(domain.OrganizationAllocationStatusActive).
		SetAvailable(decimal.Zero).
		SetReserved(decimal.Zero).
		Save(ctx)
	require.NoError(t, err)

	err = client.User.DeleteOneID(personalUser.ID).Exec(mixins.AuthorizeUserDeletion(ctx))
	require.ErrorIs(t, err, dbent.ErrUserDeletionOrganizationMember)
	_, err = client.User.Get(ctx, personalUser.ID)
	require.NoError(t, err)
}

func TestUserRepository_BulkSoftDeleteRejectsOrganizationMember(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()

	personalUser := mustCreateUser(t, client, &service.User{})
	member := mustCreateUser(t, client, &service.User{})
	organization, err := client.Organization.Create().
		SetName("Bulk Deletion Guard").
		SetCode(fmt.Sprintf("bulk-deletion-guard-%d", member.ID)).
		SetStatus(domain.OrganizationStatusActive).
		SetCreatedBy(member.ID).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.User.UpdateOneID(member.ID).
		SetOrganizationID(organization.ID).
		SetOrganizationRole(domain.OrganizationRoleMember).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.OrganizationAllocation.Create().
		SetOrganizationID(organization.ID).
		SetUserIDSnapshot(member.ID).
		SetUserRefID(member.ID).
		SetStatus(domain.OrganizationAllocationStatusActive).
		SetAvailable(decimal.Zero).
		SetReserved(decimal.Zero).
		Save(ctx)
	require.NoError(t, err)

	_, err = client.User.Update().
		Where(dbuser.IDIn(personalUser.ID, member.ID)).
		SetDeletedAt(time.Now()).
		Save(mixins.AuthorizeUserDeletion(ctx))
	require.ErrorIs(t, err, dbent.ErrUserDeletionOrganizationMember)

	_, err = client.User.Get(ctx, personalUser.ID)
	require.NoError(t, err)
	_, err = client.User.Get(ctx, member.ID)
	require.NoError(t, err)
}

func TestUserRepository_DirectEntDeleteRequiresTransaction(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{})

	t.Cleanup(func() {
		_, _ = integrationDB.Exec(`DELETE FROM users WHERE id = $1`, user.ID)
	})

	err := client.User.DeleteOneID(user.ID).Exec(mixins.AuthorizeUserDeletion(ctx))
	require.ErrorIs(t, err, dbent.ErrUserDeletionRequiresTransaction)
	_, err = client.User.Get(ctx, user.ID)
	require.NoError(t, err)
}

func TestOAuthRollbackCannotDeleteOrganizationMember(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := newUserRepositoryWithSQL(client, tx)

	member := mustCreateUser(t, client, &service.User{})
	organization, err := client.Organization.Create().
		SetName("OAuth Rollback Guard").
		SetCode(fmt.Sprintf("oauth-rollback-guard-%d", member.ID)).
		SetStatus(domain.OrganizationStatusActive).
		SetCreatedBy(member.ID).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.User.UpdateOneID(member.ID).
		SetOrganizationID(organization.ID).
		SetOrganizationRole(domain.OrganizationRoleMember).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.OrganizationAllocation.Create().
		SetOrganizationID(organization.ID).
		SetUserIDSnapshot(member.ID).
		SetUserRefID(member.ID).
		SetStatus(domain.OrganizationAllocationStatusActive).
		SetAvailable(decimal.Zero).
		SetReserved(decimal.Zero).
		Save(ctx)
	require.NoError(t, err)
	key := &service.APIKey{
		UserID: member.ID,
		Key:    fmt.Sprintf("sk-oauth-rollback-%d", member.ID),
		Name:   "oauth rollback",
		Status: service.StatusActive,
	}
	require.NoError(t, NewAPIKeyRepository(client, nil).Create(txCtx, key))

	authService := service.NewAuthService(client, repo, nil, nil, &config.Config{}, nil, nil, nil, nil, nil, nil, nil, nil)
	err = authService.RollbackOAuthEmailAccountCreation(txCtx, member.ID, "")
	require.ErrorIs(t, err, service.ErrOrganizationMembershipMustBeRemoved)

	_, err = client.User.Get(ctx, member.ID)
	require.NoError(t, err)
	storedKey, err := client.APIKey.Get(ctx, key.ID)
	require.NoError(t, err)
	require.NotNil(t, storedKey.OrganizationID)
	require.Equal(t, organization.ID, *storedKey.OrganizationID)
}

func TestOAuthRollbackDeleteRejectionKeepsInvitationUsed(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	userRepo := NewUserRepository(client, integrationDB)

	user := mustCreateUser(t, client, &service.User{})
	organization, err := client.Organization.Create().
		SetName("OAuth Invitation Rollback Guard").
		SetCode(fmt.Sprintf("oauth-invitation-rollback-guard-%d", user.ID)).
		SetStatus(domain.OrganizationStatusActive).
		SetCreatedBy(user.ID).
		Save(ctx)
	require.NoError(t, err)
	allocation, err := client.OrganizationAllocation.Create().
		SetOrganizationID(organization.ID).
		SetUserIDSnapshot(user.ID).
		SetUserRefID(user.ID).
		SetStatus(domain.OrganizationAllocationStatusActive).
		SetAvailable(decimal.Zero).
		SetReserved(decimal.Zero).
		Save(ctx)
	require.NoError(t, err)
	usedAt := time.Now().UTC()
	invitation := mustCreateRedeemCode(t, client, &service.RedeemCode{
		Code:   fmt.Sprintf("oauth-rollback-invitation-%d", user.ID),
		Type:   service.RedeemTypeInvitation,
		Status: service.StatusUsed,
		UsedBy: &user.ID,
		UsedAt: &usedAt,
	})

	t.Cleanup(func() {
		_, _ = integrationDB.Exec(`DELETE FROM redeem_codes WHERE id = $1`, invitation.ID)
		_, _ = integrationDB.Exec(`DELETE FROM organization_allocations WHERE id = $1`, allocation.ID)
		_, _ = integrationDB.Exec(`DELETE FROM organizations WHERE id = $1`, organization.ID)
		_, _ = integrationDB.Exec(`DELETE FROM users WHERE id = $1`, user.ID)
	})

	settingService := service.NewSettingService(oauthRollbackSettingRepository{
		service.SettingKeyInvitationCodeEnabled: "true",
	}, &config.Config{})
	authService := service.NewAuthService(client, userRepo, nil, nil, &config.Config{}, settingService, nil, nil, nil, nil, nil, nil, nil)

	err = authService.RollbackOAuthEmailAccountCreation(ctx, user.ID, invitation.Code)
	require.ErrorIs(t, err, service.ErrOrganizationMembershipMustBeRemoved)

	_, err = client.User.Get(ctx, user.ID)
	require.NoError(t, err, "delete rejection must keep the OAuth user")
	storedInvitation, err := client.RedeemCode.Get(ctx, invitation.ID)
	require.NoError(t, err)
	require.Equal(t, service.StatusUsed, storedInvitation.Status, "invitation restoration must roll back with the rejected user deletion")
	require.NotNil(t, storedInvitation.UsedBy)
	require.Equal(t, user.ID, *storedInvitation.UsedBy)
	require.NotNil(t, storedInvitation.UsedAt)
}
