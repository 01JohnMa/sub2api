package repository

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"
)

type organizationFundingRepository struct {
	client *dbent.Client
}

func NewOrganizationFundingRepository(client *dbent.Client) service.OrganizationFundingRepository {
	return &organizationFundingRepository{client: client}
}

func (r *organizationFundingRepository) Credit(ctx context.Context, input service.OrganizationCreditCommand) (*service.OrganizationFundingResult, error) {
	if r == nil || r.client == nil {
		return nil, service.ErrOrganizationFundingRepositoryUnavailable
	}
	if err := validateCreditCommand(input); err != nil {
		return nil, err
	}
	if existing := dbent.TxFromContext(ctx); existing != nil {
		return r.creditInTx(ctx, existing.Client(), input)
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, service.ErrOrganizationFundingRepositoryUnavailable.WithCause(err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	result, err := r.creditInTx(txCtx, tx.Client(), input)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, service.ErrOrganizationFundingRepositoryUnavailable.WithCause(err)
	}
	return result, nil
}

func (r *organizationFundingRepository) creditInTx(ctx context.Context, client *dbent.Client, input service.OrganizationCreditCommand) (*service.OrganizationFundingResult, error) {
	var status string
	var beforeText string
	var beforeVersion int64
	err := scanOne(ctx, client, `
		SELECT status, unallocated_pool::text, balance_version
		FROM organizations
		WHERE id = $1
		FOR UPDATE
	`, []any{input.OrganizationID}, &status, &beforeText, &beforeVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrOrganizationNotFound
	}
	if err != nil {
		return nil, fundingStorageError("lock organization", err)
	}
	if status == domain.OrganizationStatusArchived {
		return nil, service.ErrOrganizationArchived
	}
	if status != domain.OrganizationStatusActive && status != domain.OrganizationStatusDisabled {
		return nil, service.ErrOrganizationFundingInvariant
	}
	before, err := parseStoredMoney(beforeText)
	if err != nil {
		return nil, service.ErrOrganizationFundingInvariant.WithCause(err)
	}

	existing, err := r.findOperation(ctx, client, input)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if existing.CanonicalPayloadHash != input.CanonicalPayloadHash {
			return nil, service.ErrIdempotencyKeyConflict
		}
		existing.Replayed = true
		return existing, nil
	}

	amount, err := parseCreditAmount(input.Amount)
	if err != nil {
		return nil, err
	}
	after := before.Add(amount)
	if after.GreaterThan(maxOrganizationMoney()) {
		return nil, service.ErrOrganizationFundingAmountOverflow
	}
	if beforeVersion == math.MaxInt64 {
		return nil, service.ErrOrganizationFundingInvariant
	}
	afterText := fixedMoney(after)
	afterVersion := beforeVersion + 1
	occurredAt := input.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	operationID := strings.TrimSpace(input.OperationID)
	correlationID := strings.TrimSpace(input.CorrelationID)
	if operationID == "" || correlationID == "" {
		return nil, service.ErrOrganizationFundingInvariant
	}
	var operationRefID int64
	err = scanOne(ctx, client, `
		INSERT INTO organization_ledger_operations (
			operation_id, correlation_id, organization_id, operation_type,
			authorization_scope, actor_id_snapshot, actor_ref_id,
			actor_display_snapshot, actor_role_snapshot, currency,
			idempotency_key_hash, canonical_payload_hash, public_reason,
			internal_reason, occurred_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING id
	`, []any{
		operationID,
		correlationID,
		input.OrganizationID,
		input.OperationType,
		input.AuthorizationScope,
		input.Actor.UserID,
		input.Actor.UserID,
		input.Actor.DisplayName,
		input.Actor.PlatformRole,
		input.Currency,
		input.IdempotencyKeyHash,
		input.CanonicalPayloadHash,
		input.PublicReason,
		input.InternalReason,
		occurredAt,
	}, &operationRefID)
	if err != nil {
		return nil, translateFundingWriteError(err)
	}

	var postingID int64
	var postingCreatedAt time.Time
	err = scanOne(ctx, client, `
		INSERT INTO organization_ledger_postings (
			operation_ref_id, operation_id_snapshot, organization_id,
			posting_sequence, bucket_type, bucket_id_snapshot,
			currency, before_amount, delta_amount, after_amount,
			balance_version_before, balance_version_after, occurred_at
		) VALUES ($1, $2, $3, 1, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id, created_at
	`, []any{
		operationRefID,
		operationID,
		input.OrganizationID,
		domain.OrganizationLedgerBucketUnallocated,
		input.OrganizationID,
		input.Currency,
		fixedMoney(before),
		fixedMoney(amount),
		afterText,
		beforeVersion,
		afterVersion,
		occurredAt,
	}, &postingID, &postingCreatedAt)
	if err != nil {
		return nil, translateFundingWriteError(err)
	}

	result, err := client.ExecContext(ctx, `
		UPDATE organizations
		SET unallocated_pool = $1,
			balance_version = $2,
			updated_at = NOW()
		WHERE id = $3 AND balance_version = $4
	`, afterText, afterVersion, input.OrganizationID, beforeVersion)
	if err != nil {
		return nil, fundingStorageError("update organization funding pool", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		if err == nil {
			err = fmt.Errorf("expected one organization row, affected %d", rows)
		}
		return nil, service.ErrOrganizationFundingInvariant.WithCause(err)
	}

	publicReason := cloneOptionalString(input.PublicReason)
	ledger := service.OrganizationLedgerEntry{
		PostingID:            postingID,
		PostingSequence:      1,
		OperationID:          operationID,
		CorrelationID:        correlationID,
		OrganizationID:       input.OrganizationID,
		OperationType:        input.OperationType,
		AuthorizationScope:   input.AuthorizationScope,
		ActorIDSnapshot:      input.Actor.UserID,
		ActorDisplaySnapshot: input.Actor.DisplayName,
		ActorRoleSnapshot:    input.Actor.PlatformRole,
		Currency:             input.Currency,
		IdempotencyKeyHash:   input.IdempotencyKeyHash,
		CanonicalPayloadHash: input.CanonicalPayloadHash,
		PublicReason:         publicReason,
		InternalReason:       input.InternalReason,
		BucketType:           domain.OrganizationLedgerBucketUnallocated,
		BucketIDSnapshot:     input.OrganizationID,
		BeforeAmount:         fixedMoney(before),
		DeltaAmount:          fixedMoney(amount),
		AfterAmount:          afterText,
		BalanceVersionBefore: beforeVersion,
		BalanceVersionAfter:  afterVersion,
		OccurredAt:           occurredAt,
		CreatedAt:            postingCreatedAt,
	}
	resultModel := &service.OrganizationFundingResult{
		OperationID:          operationID,
		CorrelationID:        correlationID,
		OrganizationID:       input.OrganizationID,
		Currency:             input.Currency,
		Amount:               fixedMoney(amount),
		PoolBefore:           fixedMoney(before),
		PoolAfter:            afterText,
		BalanceVersionBefore: beforeVersion,
		BalanceVersionAfter:  afterVersion,
		CanonicalPayloadHash: input.CanonicalPayloadHash,
		Ledger:               ledger,
	}
	if err := service.ValidateOrganizationFundingResult(resultModel); err != nil {
		return nil, err
	}
	return resultModel, nil
}

func (r *organizationFundingRepository) GetSummary(ctx context.Context, organizationID int64) (*service.OrganizationBalanceSummary, error) {
	if r == nil || r.client == nil {
		return nil, service.ErrOrganizationFundingRepositoryUnavailable
	}
	client := clientFromContext(ctx, r.client)
	var status string
	var unallocatedText, availableText, reservedText, consumedText string
	var version int64
	err := scanOne(ctx, client, `
		SELECT
			o.status,
			o.unallocated_pool::text,
			COALESCE((
				SELECT SUM(a.available)
				FROM organization_allocations a
				WHERE a.organization_id = o.id AND a.status = 'active'
			), 0)::text,
			COALESCE((
				SELECT SUM(a.reserved)
				FROM organization_allocations a
				WHERE a.organization_id = o.id AND a.status = 'active'
			), 0)::text,
			COALESCE((
				SELECT SUM(-p.delta_amount)
				FROM organization_ledger_postings p
				JOIN organization_ledger_operations op ON op.id = p.operation_ref_id
				WHERE p.organization_id = o.id
				  AND op.operation_type = 'consumption'
				  AND p.delta_amount < 0
			), 0)::text,
			o.balance_version
		FROM organizations o
		WHERE o.id = $1
	`, []any{organizationID}, &status, &unallocatedText, &availableText, &reservedText, &consumedText, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrOrganizationNotFound
	}
	if err != nil {
		return nil, fundingStorageError("read organization funding summary", err)
	}
	unallocated, err := parseStoredMoney(unallocatedText)
	if err != nil {
		return nil, service.ErrOrganizationFundingInvariant.WithCause(err)
	}
	available, err := parseStoredMoney(availableText)
	if err != nil {
		return nil, service.ErrOrganizationFundingInvariant.WithCause(err)
	}
	reserved, err := parseStoredMoney(reservedText)
	if err != nil {
		return nil, service.ErrOrganizationFundingInvariant.WithCause(err)
	}
	consumed, err := parseStoredMoney(consumedText)
	if err != nil {
		return nil, service.ErrOrganizationFundingInvariant.WithCause(err)
	}
	return &service.OrganizationBalanceSummary{
		OrganizationID: organizationID,
		Status:         status,
		Currency:       domain.OrganizationCurrencyUSD,
		Unallocated:    fixedMoney(unallocated),
		Available:      fixedMoney(available),
		Reserved:       fixedMoney(reserved),
		Consumed:       fixedMoney(consumed),
		TotalRemaining: fixedMoney(unallocated.Add(available).Add(reserved)),
		BalanceVersion: version,
	}, nil
}

func (r *organizationFundingRepository) GetReport(ctx context.Context, organizationID int64, reportRange service.OrganizationReportRange) (*service.OrganizationReport, error) {
	if r == nil || r.client == nil {
		return nil, service.ErrOrganizationFundingRepositoryUnavailable
	}
	if !reportRange.Valid() {
		return nil, service.ErrOrganizationReportRangeInvalid
	}
	summary, err := r.GetSummary(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	client := clientFromContext(ctx, r.client)
	report := &service.OrganizationReport{
		OrganizationID: organizationID,
		Start:          reportRange.Start.UTC(),
		End:            reportRange.End.UTC(),
		Funds:          *summary,
		Models:         []service.OrganizationReportModel{},
		Trends:         []service.OrganizationReportTrend{},
	}
	var evidenceCostText, postingCostText string
	var reconciliationFailures int64
	err = scanOne(ctx, client, `
		WITH evidence AS (
			SELECT *
			FROM organization_final_execution_evidences
			WHERE organization_id = $1 AND occurred_at >= $2 AND occurred_at < $3
		), postings AS (
			SELECT p.final_evidence_id_snapshot, -p.delta_amount AS settled_cost, p.occurred_at
			FROM organization_ledger_postings p
			JOIN organization_ledger_operations op ON op.id = p.operation_ref_id
			WHERE p.organization_id = $1 AND op.operation_type = 'consumption'
		)
		SELECT
			COUNT(*) FILTER (WHERE NOT e.is_count_tokens_probe AND e.terminal_outcome = 'success'),
			COUNT(*) FILTER (WHERE NOT e.is_count_tokens_probe AND e.terminal_outcome = 'error'),
			COUNT(*) FILTER (WHERE NOT e.is_count_tokens_probe AND e.terminal_outcome = 'error' AND e.is_business_limited),
			COUNT(*) FILTER (WHERE NOT e.is_count_tokens_probe AND e.terminal_outcome = 'error' AND NOT e.is_business_limited),
			COALESCE(SUM(e.input_tokens), 0), COALESCE(SUM(e.output_tokens), 0),
			COALESCE(SUM(e.cache_creation_tokens), 0), COALESCE(SUM(e.cache_read_tokens), 0),
			COALESCE(SUM(e.organization_settled_cost), 0)::text,
			COALESCE(SUM(p.settled_cost), 0)::text,
			COUNT(*) FILTER (WHERE (e.organization_settled_cost > 0 AND p.final_evidence_id_snapshot IS NULL)
				OR (e.organization_settled_cost = 0 AND p.final_evidence_id_snapshot IS NOT NULL)
				OR (p.final_evidence_id_snapshot IS NOT NULL AND (p.settled_cost <> e.organization_settled_cost OR p.occurred_at <> e.occurred_at)))
		FROM evidence e
		LEFT JOIN postings p ON p.final_evidence_id_snapshot = e.id
	`, []any{organizationID, reportRange.Start.UTC(), reportRange.End.UTC()},
		&report.SuccessCount, &report.ErrorCountTotal, &report.BusinessLimitedCount, &report.ErrorCountSLA,
		&report.InputTokens, &report.OutputTokens, &report.CacheCreationTokens, &report.CacheReadTokens,
		&evidenceCostText, &postingCostText, &reconciliationFailures,
	)
	if err != nil {
		return nil, fundingStorageError("aggregate organization report", err)
	}
	evidenceCost, err := parseStoredMoney(evidenceCostText)
	if err != nil {
		return nil, service.ErrOrganizationFundingInvariant.WithCause(err)
	}
	postingCost, err := parseStoredMoney(postingCostText)
	if err != nil || reconciliationFailures != 0 || !evidenceCost.Equal(postingCost) {
		if err == nil {
			err = fmt.Errorf("final evidence and consumption postings do not reconcile")
		}
		return nil, service.ErrOrganizationFundingInvariant.WithCause(err)
	}
	report.Cost = fixedMoney(postingCost)
	report.RequestCountTotal = report.SuccessCount + report.ErrorCountTotal
	if denominator := report.SuccessCount + report.ErrorCountSLA; denominator > 0 {
		report.SuccessRate = float64(report.SuccessCount) / float64(denominator)
		report.ErrorRate = float64(report.ErrorCountSLA) / float64(denominator)
	}
	if err := scanOne(ctx, client, `
		SELECT
			(SELECT COUNT(*) FROM users WHERE organization_id = $1 AND deleted_at IS NULL),
			(SELECT COUNT(*) FROM api_keys WHERE organization_id = $1 AND deleted_at IS NULL),
			(SELECT COUNT(DISTINCT (COALESCE(allocation_id_snapshot, 0), user_id_snapshot))
			 FROM organization_final_execution_evidences
			 WHERE organization_id = $1 AND occurred_at >= $2 AND occurred_at < $3 AND NOT is_count_tokens_probe)
	`, []any{organizationID, reportRange.Start.UTC(), reportRange.End.UTC()}, &report.CurrentMemberCount, &report.APIKeyCount, &report.ActiveMemberCount); err != nil {
		return nil, fundingStorageError("aggregate organization report identities", err)
	}

	modelRows, err := client.QueryContext(ctx, `
		WITH evidence AS (
			SELECT * FROM organization_final_execution_evidences
			WHERE organization_id = $1 AND occurred_at >= $2 AND occurred_at < $3
		), postings AS (
			SELECT p.final_evidence_id_snapshot, -p.delta_amount AS settled_cost
			FROM organization_ledger_postings p JOIN organization_ledger_operations op ON op.id = p.operation_ref_id
			WHERE p.organization_id = $1 AND op.operation_type = 'consumption'
		)
		SELECT COALESCE(NULLIF(BTRIM(e.model), ''), 'unknown'),
			COUNT(*) FILTER (WHERE NOT e.is_count_tokens_probe),
			COALESCE(SUM(e.input_tokens), 0), COALESCE(SUM(e.output_tokens), 0),
			COALESCE(SUM(e.cache_creation_tokens), 0), COALESCE(SUM(e.cache_read_tokens), 0),
			COALESCE(SUM(p.settled_cost), 0)::text
		FROM evidence e LEFT JOIN postings p ON p.final_evidence_id_snapshot = e.id
		GROUP BY COALESCE(NULLIF(BTRIM(e.model), ''), 'unknown') ORDER BY 2 DESC, 1 ASC
	`, organizationID, reportRange.Start.UTC(), reportRange.End.UTC())
	if err != nil {
		return nil, fundingStorageError("list organization report models", err)
	}
	defer modelRows.Close()
	for modelRows.Next() {
		var model service.OrganizationReportModel
		if err := modelRows.Scan(&model.Model, &model.RequestCount, &model.InputTokens, &model.OutputTokens, &model.CacheCreationTokens, &model.CacheReadTokens, &model.Cost); err != nil {
			return nil, fundingStorageError("scan organization report model", err)
		}
		cost, err := parseStoredMoney(model.Cost)
		if err != nil {
			return nil, service.ErrOrganizationFundingInvariant.WithCause(err)
		}
		model.Cost = fixedMoney(cost)
		report.Models = append(report.Models, model)
	}
	if err := modelRows.Err(); err != nil {
		return nil, fundingStorageError("iterate organization report models", err)
	}

	trendRows, err := client.QueryContext(ctx, `
		WITH evidence AS (
			SELECT * FROM organization_final_execution_evidences
			WHERE organization_id = $1 AND occurred_at >= $2 AND occurred_at < $3
		), postings AS (
			SELECT p.final_evidence_id_snapshot, -p.delta_amount AS settled_cost
			FROM organization_ledger_postings p JOIN organization_ledger_operations op ON op.id = p.operation_ref_id
			WHERE p.organization_id = $1 AND op.operation_type = 'consumption'
		)
		SELECT TO_CHAR(date_trunc('day', e.occurred_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD'),
			COUNT(*) FILTER (WHERE NOT e.is_count_tokens_probe),
			COUNT(*) FILTER (WHERE NOT e.is_count_tokens_probe AND e.terminal_outcome = 'success'),
			COUNT(*) FILTER (WHERE NOT e.is_count_tokens_probe AND e.terminal_outcome = 'error'),
			COALESCE(SUM(e.input_tokens), 0), COALESCE(SUM(e.output_tokens), 0),
			COALESCE(SUM(e.cache_creation_tokens), 0), COALESCE(SUM(e.cache_read_tokens), 0),
			COALESCE(SUM(p.settled_cost), 0)::text
		FROM evidence e LEFT JOIN postings p ON p.final_evidence_id_snapshot = e.id
		GROUP BY 1 ORDER BY 1 ASC
	`, organizationID, reportRange.Start.UTC(), reportRange.End.UTC())
	if err != nil {
		return nil, fundingStorageError("list organization report trends", err)
	}
	defer trendRows.Close()
	for trendRows.Next() {
		var trend service.OrganizationReportTrend
		if err := trendRows.Scan(&trend.OccurredOn, &trend.RequestCount, &trend.SuccessCount, &trend.ErrorCountTotal, &trend.InputTokens, &trend.OutputTokens, &trend.CacheCreationTokens, &trend.CacheReadTokens, &trend.Cost); err != nil {
			return nil, fundingStorageError("scan organization report trend", err)
		}
		cost, err := parseStoredMoney(trend.Cost)
		if err != nil {
			return nil, service.ErrOrganizationFundingInvariant.WithCause(err)
		}
		trend.Cost = fixedMoney(cost)
		report.Trends = append(report.Trends, trend)
	}
	if err := trendRows.Err(); err != nil {
		return nil, fundingStorageError("iterate organization report trends", err)
	}
	return report, nil
}

// GetReportingDimensionReport is the minimal first-release report. It reads
// the existing usage/error projections with their request-time organization
// dimension and deliberately does not consult organization funding tables.
func (r *organizationFundingRepository) GetReportingDimensionReport(ctx context.Context, organizationID int64, reportRange service.OrganizationReportRange) (*service.OrganizationReport, error) {
	if r == nil || r.client == nil {
		return nil, service.ErrOrganizationFundingRepositoryUnavailable
	}
	if organizationID <= 0 {
		return nil, service.ErrInvalidOrganizationID
	}
	if !reportRange.Valid() {
		return nil, service.ErrOrganizationReportRangeInvalid
	}
	client := clientFromContext(ctx, r.client)
	start := reportRange.Start.UTC()
	end := reportRange.End.UTC()
	report := &service.OrganizationReport{
		OrganizationID: organizationID,
		Start:          start,
		End:            end,
		Models:         []service.OrganizationReportModel{},
		Trends:         []service.OrganizationReportTrend{},
	}

	var costText string
	if err := scanOne(ctx, client, `
		SELECT
			(SELECT COUNT(*) FROM usage_logs
			 WHERE organization_id = $1 AND created_at >= $2 AND created_at < $3 AND actual_cost > 0),
			(SELECT COUNT(*) FROM ops_error_logs
			 WHERE organization_id = $1 AND created_at >= $2 AND created_at < $3
			   AND COALESCE(status_code, 0) >= 400 AND NOT COALESCE(is_count_tokens, false)),
			(SELECT COUNT(*) FROM ops_error_logs
			 WHERE organization_id = $1 AND created_at >= $2 AND created_at < $3
			   AND COALESCE(status_code, 0) >= 400 AND COALESCE(is_business_limited, false)
			   AND NOT COALESCE(is_count_tokens, false)),
			(SELECT COUNT(*) FROM ops_error_logs
			 WHERE organization_id = $1 AND created_at >= $2 AND created_at < $3
			   AND COALESCE(status_code, 0) >= 400 AND NOT COALESCE(is_business_limited, false)
			   AND NOT COALESCE(is_count_tokens, false)),
			(SELECT COALESCE(SUM(input_tokens), 0) FROM usage_logs
			 WHERE organization_id = $1 AND created_at >= $2 AND created_at < $3),
			(SELECT COALESCE(SUM(output_tokens), 0) FROM usage_logs
			 WHERE organization_id = $1 AND created_at >= $2 AND created_at < $3),
			(SELECT COALESCE(SUM(cache_creation_tokens), 0) FROM usage_logs
			 WHERE organization_id = $1 AND created_at >= $2 AND created_at < $3),
			(SELECT COALESCE(SUM(cache_read_tokens), 0) FROM usage_logs
			 WHERE organization_id = $1 AND created_at >= $2 AND created_at < $3),
			(SELECT COALESCE(SUM(actual_cost), 0)::text FROM usage_logs
			 WHERE organization_id = $1 AND created_at >= $2 AND created_at < $3)
	`, []any{organizationID, start, end},
		&report.SuccessCount, &report.ErrorCountTotal, &report.BusinessLimitedCount, &report.ErrorCountSLA,
		&report.InputTokens, &report.OutputTokens, &report.CacheCreationTokens, &report.CacheReadTokens,
		&costText,
	); err != nil {
		return nil, fundingStorageError("aggregate organization reporting dimension", err)
	}
	cost, err := parseStoredMoney(costText)
	if err != nil {
		return nil, service.ErrOrganizationReportRangeInvalid.WithCause(err)
	}
	report.Cost = fixedMoney(cost)
	report.RequestCountTotal = report.SuccessCount + report.ErrorCountTotal
	if denominator := report.SuccessCount + report.ErrorCountSLA; denominator > 0 {
		report.SuccessRate = float64(report.SuccessCount) / float64(denominator)
		report.ErrorRate = float64(report.ErrorCountSLA) / float64(denominator)
	}
	if err := scanOne(ctx, client, `
		SELECT
			(SELECT COUNT(*) FROM users WHERE organization_id = $1 AND deleted_at IS NULL),
			(SELECT COUNT(*) FROM api_keys WHERE organization_id = $1 AND deleted_at IS NULL),
			(SELECT COUNT(DISTINCT user_id) FROM usage_logs
			 WHERE organization_id = $1 AND created_at >= $2 AND created_at < $3),
			(SELECT COUNT(*) FROM usage_logs
			 WHERE organization_id = $1 AND created_at >= $2 AND created_at < $3)
	`, []any{organizationID, start, end}, &report.CurrentMemberCount, &report.APIKeyCount, &report.ActiveMemberCount, new(int64)); err != nil {
		return nil, fundingStorageError("aggregate organization reporting identities", err)
	}

	modelRows, err := client.QueryContext(ctx, `
		SELECT COALESCE(NULLIF(BTRIM(model), ''), 'unknown'), COUNT(*),
			COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(cache_creation_tokens), 0), COALESCE(SUM(cache_read_tokens), 0),
			COALESCE(SUM(actual_cost), 0)::text
		FROM usage_logs
		WHERE organization_id = $1 AND created_at >= $2 AND created_at < $3 AND actual_cost > 0
		GROUP BY COALESCE(NULLIF(BTRIM(model), ''), 'unknown')
		ORDER BY 2 DESC, 1 ASC
	`, organizationID, start, end)
	if err != nil {
		return nil, fundingStorageError("list organization reporting models", err)
	}
	defer modelRows.Close()
	for modelRows.Next() {
		var model service.OrganizationReportModel
		if err := modelRows.Scan(&model.Model, &model.RequestCount, &model.InputTokens, &model.OutputTokens, &model.CacheCreationTokens, &model.CacheReadTokens, &model.Cost); err != nil {
			return nil, fundingStorageError("scan organization reporting model", err)
		}
		parsed, err := parseStoredMoney(model.Cost)
		if err != nil {
			return nil, fundingStorageError("parse organization reporting model cost", err)
		}
		model.Cost = fixedMoney(parsed)
		report.Models = append(report.Models, model)
	}
	if err := modelRows.Err(); err != nil {
		return nil, fundingStorageError("iterate organization reporting models", err)
	}

	trendRows, err := client.QueryContext(ctx, `
		SELECT occurred_on, SUM(request_count), SUM(success_count), SUM(error_count_total),
			SUM(input_tokens), SUM(output_tokens), SUM(cache_creation_tokens), SUM(cache_read_tokens),
			SUM(cost)::text
		FROM (
			SELECT TO_CHAR(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD') AS occurred_on,
				COUNT(*) AS request_count, COUNT(*) AS success_count, 0::bigint AS error_count_total,
				COALESCE(SUM(input_tokens), 0) AS input_tokens, COALESCE(SUM(output_tokens), 0) AS output_tokens,
				COALESCE(SUM(cache_creation_tokens), 0) AS cache_creation_tokens,
				COALESCE(SUM(cache_read_tokens), 0) AS cache_read_tokens,
				COALESCE(SUM(actual_cost), 0) AS cost
			FROM usage_logs
			WHERE organization_id = $1 AND created_at >= $2 AND created_at < $3 AND actual_cost > 0
			GROUP BY 1
			UNION ALL
			SELECT TO_CHAR(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD'),
				COUNT(*), 0::bigint, COUNT(*), 0::bigint, 0::bigint, 0::bigint, 0::bigint, 0::numeric
			FROM ops_error_logs
			WHERE organization_id = $1 AND created_at >= $2 AND created_at < $3
				AND COALESCE(status_code, 0) >= 400 AND NOT COALESCE(is_count_tokens, false)
			GROUP BY 1
		) grouped
		GROUP BY occurred_on
		ORDER BY occurred_on ASC
	`, organizationID, start, end)
	if err != nil {
		return nil, fundingStorageError("list organization reporting trends", err)
	}
	defer trendRows.Close()
	for trendRows.Next() {
		var trend service.OrganizationReportTrend
		if err := trendRows.Scan(&trend.OccurredOn, &trend.RequestCount, &trend.SuccessCount, &trend.ErrorCountTotal, &trend.InputTokens, &trend.OutputTokens, &trend.CacheCreationTokens, &trend.CacheReadTokens, &trend.Cost); err != nil {
			return nil, fundingStorageError("scan organization reporting trend", err)
		}
		parsed, err := parseStoredMoney(trend.Cost)
		if err != nil {
			return nil, fundingStorageError("parse organization reporting trend cost", err)
		}
		trend.Cost = fixedMoney(parsed)
		report.Trends = append(report.Trends, trend)
	}
	if err := trendRows.Err(); err != nil {
		return nil, fundingStorageError("iterate organization reporting trends", err)
	}
	return report, nil
}

func (r *organizationFundingRepository) ListReportMembers(ctx context.Context, organizationID int64, reportRange service.OrganizationReportRange, params pagination.PaginationParams) ([]service.OrganizationReportMemberUsage, *service.OrganizationReportMemberUsage, *pagination.PaginationResult, error) {
	if r == nil || r.client == nil {
		return nil, nil, nil, service.ErrOrganizationFundingRepositoryUnavailable
	}
	if !reportRange.Valid() {
		return nil, nil, nil, service.ErrOrganizationReportRangeInvalid
	}
	client := clientFromContext(ctx, r.client)
	var total int64
	if err := scanOne(ctx, client, `
		SELECT COUNT(*)
		FROM users u
		JOIN organization_allocations allocation
		  ON allocation.organization_id = $1
		 AND allocation.user_id_snapshot = u.id
		 AND allocation.user_ref_id = u.id
		 AND allocation.status = 'active'
		WHERE u.organization_id = $1 AND u.deleted_at IS NULL
	`, []any{organizationID}, &total); err != nil {
		return nil, nil, nil, fundingStorageError("count organization report members", err)
	}
	if total == 0 {
		var exists bool
		if err := scanOne(ctx, client, `SELECT EXISTS(SELECT 1 FROM organizations WHERE id = $1)`, []any{organizationID}, &exists); err != nil || !exists {
			if err == nil {
				err = sql.ErrNoRows
			}
			return nil, nil, nil, service.ErrOrganizationNotFound.WithCause(err)
		}
	}
	rows, err := client.QueryContext(ctx, `
		WITH page AS (
			SELECT u.id, u.email, u.username, allocation.id AS allocation_id
			FROM users u
			JOIN organization_allocations allocation
			  ON allocation.organization_id = $1
			 AND allocation.user_id_snapshot = u.id
			 AND allocation.user_ref_id = u.id
			 AND allocation.status = 'active'
			WHERE u.organization_id = $1 AND u.deleted_at IS NULL
			ORDER BY u.id ASC, allocation.id ASC LIMIT $4 OFFSET $5
		), evidence AS (
			SELECT * FROM organization_final_execution_evidences
			WHERE organization_id = $1 AND occurred_at >= $2 AND occurred_at < $3
		), postings AS (
			SELECT p.final_evidence_id_snapshot, -p.delta_amount AS settled_cost
			FROM organization_ledger_postings p JOIN organization_ledger_operations op ON op.id = p.operation_ref_id
			WHERE p.organization_id = $1 AND op.operation_type = 'consumption'
		)
		SELECT page.id, page.email, page.username,
			COUNT(e.id) FILTER (WHERE NOT e.is_count_tokens_probe),
			COUNT(e.id) FILTER (WHERE NOT e.is_count_tokens_probe AND e.terminal_outcome = 'success'),
			COUNT(e.id) FILTER (WHERE NOT e.is_count_tokens_probe AND e.terminal_outcome = 'error'),
			COALESCE(SUM(e.input_tokens), 0), COALESCE(SUM(e.output_tokens), 0),
			COALESCE(SUM(e.cache_creation_tokens), 0), COALESCE(SUM(e.cache_read_tokens), 0), COALESCE(SUM(p.settled_cost), 0)::text
		FROM page LEFT JOIN evidence e
		  ON e.user_id_snapshot = page.id AND e.allocation_id_snapshot = page.allocation_id
		LEFT JOIN postings p ON p.final_evidence_id_snapshot = e.id
		GROUP BY page.id, page.email, page.username ORDER BY page.id ASC
	`, organizationID, reportRange.Start.UTC(), reportRange.End.UTC(), params.Limit(), params.Offset())
	if err != nil {
		return nil, nil, nil, fundingStorageError("list organization report members", err)
	}
	defer rows.Close()
	items := make([]service.OrganizationReportMemberUsage, 0)
	for rows.Next() {
		var userID int64
		var email, username string
		var item service.OrganizationReportMemberUsage
		if err := rows.Scan(&userID, &email, &username, &item.RequestCount, &item.SuccessCount, &item.ErrorCountTotal, &item.InputTokens, &item.OutputTokens, &item.CacheCreationTokens, &item.CacheReadTokens, &item.Cost); err != nil {
			return nil, nil, nil, fundingStorageError("scan organization report member", err)
		}
		cost, err := parseStoredMoney(item.Cost)
		if err != nil {
			return nil, nil, nil, service.ErrOrganizationFundingInvariant.WithCause(err)
		}
		item.Cost = fixedMoney(cost)
		item.UserID = &userID
		item.DisplayName = strings.TrimSpace(username)
		if item.DisplayName == "" {
			item.DisplayName = email
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, fundingStorageError("iterate organization report members", err)
	}

	departed := &service.OrganizationReportMemberUsage{DisplayName: "departed_members", Cost: "0.00000000"}
	err = scanOne(ctx, client, `
		WITH evidence AS (
			SELECT * FROM organization_final_execution_evidences
			WHERE organization_id = $1 AND occurred_at >= $2 AND occurred_at < $3
		), postings AS (
			SELECT p.final_evidence_id_snapshot, -p.delta_amount AS settled_cost
			FROM organization_ledger_postings p JOIN organization_ledger_operations op ON op.id = p.operation_ref_id
			WHERE p.organization_id = $1 AND op.operation_type = 'consumption'
		)
		SELECT COUNT(e.id) FILTER (WHERE NOT e.is_count_tokens_probe),
			COUNT(e.id) FILTER (WHERE NOT e.is_count_tokens_probe AND e.terminal_outcome = 'success'),
			COUNT(e.id) FILTER (WHERE NOT e.is_count_tokens_probe AND e.terminal_outcome = 'error'),
			COALESCE(SUM(e.input_tokens), 0), COALESCE(SUM(e.output_tokens), 0),
			COALESCE(SUM(e.cache_creation_tokens), 0), COALESCE(SUM(e.cache_read_tokens), 0), COALESCE(SUM(p.settled_cost), 0)::text
		FROM evidence e LEFT JOIN postings p ON p.final_evidence_id_snapshot = e.id
		WHERE NOT EXISTS (
			SELECT 1
			FROM users u
			JOIN organization_allocations allocation
			  ON allocation.organization_id = $1
			 AND allocation.id = e.allocation_id_snapshot
			 AND allocation.user_id_snapshot = e.user_id_snapshot
			 AND allocation.user_ref_id = u.id
			 AND allocation.status = 'active'
			WHERE u.id = e.user_id_snapshot AND u.organization_id = $1 AND u.deleted_at IS NULL
		)
	`, []any{organizationID, reportRange.Start.UTC(), reportRange.End.UTC()}, &departed.RequestCount, &departed.SuccessCount, &departed.ErrorCountTotal, &departed.InputTokens, &departed.OutputTokens, &departed.CacheCreationTokens, &departed.CacheReadTokens, &departed.Cost)
	if err != nil {
		return nil, nil, nil, fundingStorageError("aggregate departed organization report members", err)
	}
	cost, err := parseStoredMoney(departed.Cost)
	if err != nil {
		return nil, nil, nil, service.ErrOrganizationFundingInvariant.WithCause(err)
	}
	departed.Cost = fixedMoney(cost)
	return items, departed, paginationResultFromTotal(total, params), nil
}

func (r *organizationFundingRepository) ListReportMembersFiltered(ctx context.Context, organizationID int64, reportRange service.OrganizationReportRange, params pagination.PaginationParams, filters service.OrganizationReportMemberFilters) ([]service.OrganizationReportMemberUsage, *service.OrganizationReportMemberUsage, *pagination.PaginationResult, error) {
	if r == nil || r.client == nil {
		return nil, nil, nil, service.ErrOrganizationFundingRepositoryUnavailable
	}
	if !reportRange.Valid() {
		return nil, nil, nil, service.ErrOrganizationReportRangeInvalid
	}
	client := clientFromContext(ctx, r.client)
	if err := validateOrganizationReportFilters(ctx, client, organizationID, filters); err != nil {
		return nil, nil, nil, err
	}
	var total int64
	if err := scanOne(ctx, client, `
		SELECT COUNT(*) FROM users u
		JOIN organization_allocations a ON a.organization_id = $1 AND a.user_id_snapshot = u.id AND a.user_ref_id = u.id AND a.status = 'active'
		WHERE u.organization_id = $1 AND u.deleted_at IS NULL
		  AND ($2::bigint IS NULL OR u.id = $2) AND ($3::bigint IS NULL OR a.id = $3)
		  AND ($4::bigint IS NULL OR EXISTS (SELECT 1 FROM api_keys k WHERE k.id = $4 AND k.user_id = u.id))
	`, []any{organizationID, filters.UserID, filters.AllocationID, filters.APIKeyID}, &total); err != nil {
		return nil, nil, nil, fundingStorageError("count filtered organization report members", err)
	}
	rows, err := client.QueryContext(ctx, `
		WITH page AS (
			SELECT u.id, u.email, u.username, a.id AS allocation_id FROM users u
			JOIN organization_allocations a ON a.organization_id = $1 AND a.user_id_snapshot = u.id AND a.user_ref_id = u.id AND a.status = 'active'
			WHERE u.organization_id = $1 AND u.deleted_at IS NULL
			  AND ($4::bigint IS NULL OR u.id = $4) AND ($5::bigint IS NULL OR a.id = $5)
			  AND ($6::bigint IS NULL OR EXISTS (SELECT 1 FROM api_keys k WHERE k.id = $6 AND k.user_id = u.id))
			ORDER BY u.id ASC, a.id ASC LIMIT $7 OFFSET $8
		), evidence AS (
			SELECT * FROM organization_final_execution_evidences
			WHERE organization_id = $1 AND occurred_at >= $2 AND occurred_at < $3
			  AND ($9::bigint IS NULL OR api_key_id_snapshot = $9)
		), postings AS (
			SELECT p.final_evidence_id_snapshot, -p.delta_amount AS settled_cost FROM organization_ledger_postings p
			JOIN organization_ledger_operations op ON op.id = p.operation_ref_id
			WHERE p.organization_id = $1 AND op.operation_type = 'consumption'
		)
		SELECT page.id, page.email, page.username,
			COUNT(e.id) FILTER (WHERE NOT e.is_count_tokens_probe),
			COUNT(e.id) FILTER (WHERE NOT e.is_count_tokens_probe AND e.terminal_outcome = 'success'),
			COUNT(e.id) FILTER (WHERE NOT e.is_count_tokens_probe AND e.terminal_outcome = 'error'),
			COALESCE(SUM(e.input_tokens), 0), COALESCE(SUM(e.output_tokens), 0),
			COALESCE(SUM(e.cache_creation_tokens), 0), COALESCE(SUM(e.cache_read_tokens), 0), COALESCE(SUM(p.settled_cost), 0)::text
		FROM page LEFT JOIN evidence e ON e.user_id_snapshot = page.id AND e.allocation_id_snapshot = page.allocation_id
		LEFT JOIN postings p ON p.final_evidence_id_snapshot = e.id
		GROUP BY page.id, page.email, page.username ORDER BY page.id ASC
	`, organizationID, reportRange.Start.UTC(), reportRange.End.UTC(), filters.UserID, filters.AllocationID, filters.APIKeyID, params.Limit(), params.Offset(), filters.APIKeyID)
	if err != nil {
		return nil, nil, nil, fundingStorageError("list filtered organization report members", err)
	}
	defer rows.Close()
	items := make([]service.OrganizationReportMemberUsage, 0)
	for rows.Next() {
		var userID int64
		var email, username string
		var item service.OrganizationReportMemberUsage
		if err := rows.Scan(&userID, &email, &username, &item.RequestCount, &item.SuccessCount, &item.ErrorCountTotal, &item.InputTokens, &item.OutputTokens, &item.CacheCreationTokens, &item.CacheReadTokens, &item.Cost); err != nil {
			return nil, nil, nil, fundingStorageError("scan filtered organization report member", err)
		}
		cost, err := parseStoredMoney(item.Cost)
		if err != nil {
			return nil, nil, nil, service.ErrOrganizationFundingInvariant.WithCause(err)
		}
		item.Cost, item.UserID, item.DisplayName = fixedMoney(cost), &userID, strings.TrimSpace(username)
		if item.DisplayName == "" {
			item.DisplayName = email
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, fundingStorageError("iterate filtered organization report members", err)
	}

	// Current member/allocation filters cannot identify departed history. A key
	// filter may, so keep its departed aggregate while never exposing its owner.
	departed := &service.OrganizationReportMemberUsage{DisplayName: "departed_members", Cost: "0.00000000"}
	if filters.UserID == nil && filters.AllocationID == nil {
		err = scanOne(ctx, client, `
			WITH evidence AS (
				SELECT * FROM organization_final_execution_evidences WHERE organization_id = $1 AND occurred_at >= $2 AND occurred_at < $3
				  AND ($4::bigint IS NULL OR api_key_id_snapshot = $4)
			), postings AS (
				SELECT p.final_evidence_id_snapshot, -p.delta_amount AS settled_cost FROM organization_ledger_postings p JOIN organization_ledger_operations op ON op.id = p.operation_ref_id
				WHERE p.organization_id = $1 AND op.operation_type = 'consumption'
			)
			SELECT COUNT(e.id) FILTER (WHERE NOT e.is_count_tokens_probe), COUNT(e.id) FILTER (WHERE NOT e.is_count_tokens_probe AND e.terminal_outcome = 'success'),
				COUNT(e.id) FILTER (WHERE NOT e.is_count_tokens_probe AND e.terminal_outcome = 'error'), COALESCE(SUM(e.input_tokens), 0), COALESCE(SUM(e.output_tokens), 0),
				COALESCE(SUM(e.cache_creation_tokens), 0), COALESCE(SUM(e.cache_read_tokens), 0), COALESCE(SUM(p.settled_cost), 0)::text
			FROM evidence e LEFT JOIN postings p ON p.final_evidence_id_snapshot = e.id
			WHERE NOT EXISTS (
				SELECT 1 FROM users u JOIN organization_allocations a ON a.organization_id = $1 AND a.id = e.allocation_id_snapshot AND a.user_id_snapshot = e.user_id_snapshot AND a.user_ref_id = u.id AND a.status = 'active'
				WHERE u.id = e.user_id_snapshot AND u.organization_id = $1 AND u.deleted_at IS NULL
			)
		`, []any{organizationID, reportRange.Start.UTC(), reportRange.End.UTC(), filters.APIKeyID}, &departed.RequestCount, &departed.SuccessCount, &departed.ErrorCountTotal, &departed.InputTokens, &departed.OutputTokens, &departed.CacheCreationTokens, &departed.CacheReadTokens, &departed.Cost)
		if err != nil {
			return nil, nil, nil, fundingStorageError("aggregate filtered departed organization report members", err)
		}
		cost, parseErr := parseStoredMoney(departed.Cost)
		if parseErr != nil {
			return nil, nil, nil, service.ErrOrganizationFundingInvariant.WithCause(parseErr)
		}
		departed.Cost = fixedMoney(cost)
	}
	return items, departed, paginationResultFromTotal(total, params), nil
}

func (r *organizationFundingRepository) ListOrganizationAPIKeyMetadata(ctx context.Context, organizationID int64, params pagination.PaginationParams, filters service.OrganizationReportMemberFilters) ([]service.OrganizationAPIKeyMetadata, *pagination.PaginationResult, error) {
	if r == nil || r.client == nil {
		return nil, nil, service.ErrOrganizationFundingRepositoryUnavailable
	}
	client := clientFromContext(ctx, r.client)
	if err := validateOrganizationReportFilters(ctx, client, organizationID, filters); err != nil {
		return nil, nil, err
	}
	var total int64
	if err := scanOne(ctx, client, `
		SELECT COUNT(*) FROM api_keys k JOIN users u ON u.id = k.user_id
		WHERE k.organization_id = $1 AND k.deleted_at IS NULL AND u.organization_id = $1 AND u.deleted_at IS NULL
		  AND ($2::bigint IS NULL OR k.user_id = $2) AND ($3::bigint IS NULL OR k.id = $3)
		  AND ($4::bigint IS NULL OR k.user_id = (SELECT user_id_snapshot FROM organization_allocations WHERE id = $4))
	`, []any{organizationID, filters.UserID, filters.APIKeyID, filters.AllocationID}, &total); err != nil {
		return nil, nil, fundingStorageError("count organization key metadata", err)
	}
	rows, err := client.QueryContext(ctx, `
		SELECT k.id, k.name, k.status, k.user_id, COALESCE(NULLIF(BTRIM(u.username), ''), u.email), k.organization_id, k.created_at, k.last_used_at, k.expires_at, k.quota, k.quota_used
		FROM api_keys k JOIN users u ON u.id = k.user_id
		WHERE k.organization_id = $1 AND k.deleted_at IS NULL AND u.organization_id = $1 AND u.deleted_at IS NULL
		  AND ($2::bigint IS NULL OR k.user_id = $2) AND ($3::bigint IS NULL OR k.id = $3)
		  AND ($4::bigint IS NULL OR k.user_id = (SELECT user_id_snapshot FROM organization_allocations WHERE id = $4))
		ORDER BY k.id ASC LIMIT $5 OFFSET $6
	`, organizationID, filters.UserID, filters.APIKeyID, filters.AllocationID, params.Limit(), params.Offset())
	if err != nil {
		return nil, nil, fundingStorageError("list organization key metadata", err)
	}
	defer rows.Close()
	items := make([]service.OrganizationAPIKeyMetadata, 0)
	for rows.Next() {
		var item service.OrganizationAPIKeyMetadata
		if err := rows.Scan(&item.ID, &item.Name, &item.Status, &item.UserID, &item.MemberName, &item.OrganizationID, &item.CreatedAt, &item.LastUsedAt, &item.ExpiresAt, &item.Quota, &item.QuotaUsed); err != nil {
			return nil, nil, fundingStorageError("scan organization key metadata", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fundingStorageError("iterate organization key metadata", err)
	}
	return items, paginationResultFromTotal(total, params), nil
}

func validateOrganizationReportFilters(ctx context.Context, client *dbent.Client, organizationID int64, filters service.OrganizationReportMemberFilters) error {
	if filters.UserID != nil {
		var exists bool
		if err := scanOne(ctx, client, `SELECT EXISTS(SELECT 1 FROM users WHERE id = $1 AND organization_id = $2 AND deleted_at IS NULL)`, []any{*filters.UserID, organizationID}, &exists); err != nil {
			return fundingStorageError("validate organization member filter", err)
		}
		if !exists {
			return service.ErrOrganizationMemberNotFound
		}
	}
	if filters.AllocationID != nil {
		var exists bool
		if err := scanOne(ctx, client, `SELECT EXISTS(SELECT 1 FROM organization_allocations WHERE id = $1 AND organization_id = $2 AND status = 'active' AND user_ref_id IS NOT NULL AND ($3::bigint IS NULL OR user_id_snapshot = $3))`, []any{*filters.AllocationID, organizationID, filters.UserID}, &exists); err != nil {
			return fundingStorageError("validate organization allocation filter", err)
		}
		if !exists {
			return service.ErrOrganizationAllocationTargetNotFound
		}
	}
	if filters.APIKeyID != nil {
		var exists bool
		if err := scanOne(ctx, client, `SELECT EXISTS(SELECT 1 FROM api_keys WHERE id = $1 AND organization_id = $2 AND deleted_at IS NULL AND ($3::bigint IS NULL OR user_id = $3))`, []any{*filters.APIKeyID, organizationID, filters.UserID}, &exists); err != nil {
			return fundingStorageError("validate organization API key filter", err)
		}
		if !exists {
			return service.ErrAPIKeyNotFound
		}
	}
	return nil
}

func (r *organizationFundingRepository) ListLedger(ctx context.Context, organizationID int64, params pagination.PaginationParams) ([]service.OrganizationLedgerEntry, *pagination.PaginationResult, error) {
	if r == nil || r.client == nil {
		return nil, nil, service.ErrOrganizationFundingRepositoryUnavailable
	}
	client := clientFromContext(ctx, r.client)
	var total int64
	if err := scanOne(ctx, client, `
		SELECT COUNT(p.id)
		FROM organizations org
		LEFT JOIN organization_ledger_operations o
		  ON o.organization_id = org.id
		 AND o.no_op_result_snapshot IS NULL
		LEFT JOIN organization_ledger_postings p ON p.operation_ref_id = o.id AND p.organization_id = o.organization_id
		WHERE org.id = $1
		GROUP BY org.id
	`, []any{organizationID}, &total); errors.Is(err, sql.ErrNoRows) {
		return nil, nil, service.ErrOrganizationNotFound
	} else if err != nil {
		return nil, nil, fundingStorageError("count organization ledger", err)
	}
	if total == 0 {
		return []service.OrganizationLedgerEntry{}, paginationResultFromTotal(0, params), nil
	}
	order := "DESC"
	if params.NormalizedSortOrder(pagination.SortOrderDesc) == pagination.SortOrderAsc {
		order = "ASC"
	}
	query := fmt.Sprintf(`
		SELECT
			o.operation_id::text,
			o.correlation_id::text,
			o.organization_id,
			o.operation_type,
			o.authorization_scope,
			o.actor_id_snapshot,
			o.actor_display_snapshot,
			o.actor_role_snapshot,
			o.currency,
			o.idempotency_key_hash,
			o.canonical_payload_hash,
			o.public_reason,
			o.internal_reason,
			o.occurred_at,
			p.id,
			p.posting_sequence,
			p.bucket_type,
			p.bucket_id_snapshot,
			p.allocation_id_snapshot,
			p.target_user_id_snapshot,
			p.before_amount::text,
			p.delta_amount::text,
			p.after_amount::text,
			p.balance_version_before,
			p.balance_version_after,
			p.created_at
		FROM organization_ledger_postings p
		JOIN organization_ledger_operations o ON o.id = p.operation_ref_id AND p.organization_id = o.organization_id
		WHERE p.organization_id = $1 AND o.no_op_result_snapshot IS NULL
		ORDER BY p.occurred_at %s, p.id %s
		LIMIT $2 OFFSET $3
	`, order, order)
	rows, err := client.QueryContext(ctx, query, organizationID, params.Limit(), params.Offset())
	if err != nil {
		return nil, nil, fundingStorageError("list organization ledger", err)
	}
	defer rows.Close()
	entries := make([]service.OrganizationLedgerEntry, 0)
	for rows.Next() {
		entry, err := scanLedgerRow(rows)
		if err != nil {
			return nil, nil, fundingStorageError("scan organization ledger", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fundingStorageError("iterate organization ledger", err)
	}
	return entries, paginationResultFromTotal(total, params), nil
}

func (r *organizationFundingRepository) findOperation(ctx context.Context, client *dbent.Client, input service.OrganizationCreditCommand) (*service.OrganizationFundingResult, error) {
	var row service.OrganizationFundingResult
	var entry service.OrganizationLedgerEntry
	var publicReason sql.NullString
	var allocationID, targetUserID sql.NullInt64
	err := scanOne(ctx, client, `
		SELECT
			o.operation_id::text,
			o.correlation_id::text,
			o.organization_id,
			o.currency,
			o.canonical_payload_hash,
			o.actor_id_snapshot,
			o.actor_display_snapshot,
			o.actor_role_snapshot,
			o.authorization_scope,
			o.idempotency_key_hash,
			o.public_reason,
			o.internal_reason,
			o.operation_type,
			o.occurred_at,
			p.id,
			p.posting_sequence,
			p.bucket_type,
			p.bucket_id_snapshot,
			p.allocation_id_snapshot,
			p.target_user_id_snapshot,
			p.before_amount::text,
			p.delta_amount::text,
			p.after_amount::text,
			p.balance_version_before,
			p.balance_version_after,
			p.created_at
		FROM organization_ledger_operations o
		JOIN organization_ledger_postings p ON p.operation_ref_id = o.id
		WHERE o.authorization_scope = $1
		  AND o.organization_id = $2
		  AND o.operation_type = $3
		  AND o.idempotency_key_hash = $4
		ORDER BY p.posting_sequence ASC
		LIMIT 1
		FOR UPDATE OF o
	`, []any{
		input.AuthorizationScope,
		input.OrganizationID,
		input.OperationType,
		input.IdempotencyKeyHash,
	},
		&row.OperationID,
		&row.CorrelationID,
		&row.OrganizationID,
		&row.Currency,
		&row.CanonicalPayloadHash,
		&entry.ActorIDSnapshot,
		&entry.ActorDisplaySnapshot,
		&entry.ActorRoleSnapshot,
		&entry.AuthorizationScope,
		&entry.IdempotencyKeyHash,
		&publicReason,
		&entry.InternalReason,
		&entry.OperationType,
		&entry.OccurredAt,
		&entry.PostingID,
		&entry.PostingSequence,
		&entry.BucketType,
		&entry.BucketIDSnapshot,
		&allocationID,
		&targetUserID,
		&entry.BeforeAmount,
		&entry.DeltaAmount,
		&entry.AfterAmount,
		&entry.BalanceVersionBefore,
		&entry.BalanceVersionAfter,
		&entry.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fundingStorageError("read organization funding idempotency record", err)
	}
	if publicReason.Valid {
		entry.PublicReason = &publicReason.String
	}
	entry.OperationID = row.OperationID
	entry.CorrelationID = row.CorrelationID
	entry.OrganizationID = row.OrganizationID
	entry.Currency = row.Currency
	entry.CanonicalPayloadHash = row.CanonicalPayloadHash
	entry.AllocationIDSnapshot = nullableInt64(allocationID)
	entry.TargetUserIDSnapshot = nullableInt64(targetUserID)
	row.Ledger = entry
	row.Amount = entry.DeltaAmount
	row.PoolBefore = entry.BeforeAmount
	row.PoolAfter = entry.AfterAmount
	row.BalanceVersionBefore = entry.BalanceVersionBefore
	row.BalanceVersionAfter = entry.BalanceVersionAfter
	if err := service.ValidateOrganizationFundingResult(&row); err != nil {
		return nil, err
	}
	return &row, nil
}

func scanLedgerRow(rows *sql.Rows) (service.OrganizationLedgerEntry, error) {
	var entry service.OrganizationLedgerEntry
	var publicReason sql.NullString
	var allocationID, targetUserID sql.NullInt64
	err := rows.Scan(
		&entry.OperationID,
		&entry.CorrelationID,
		&entry.OrganizationID,
		&entry.OperationType,
		&entry.AuthorizationScope,
		&entry.ActorIDSnapshot,
		&entry.ActorDisplaySnapshot,
		&entry.ActorRoleSnapshot,
		&entry.Currency,
		&entry.IdempotencyKeyHash,
		&entry.CanonicalPayloadHash,
		&publicReason,
		&entry.InternalReason,
		&entry.OccurredAt,
		&entry.PostingID,
		&entry.PostingSequence,
		&entry.BucketType,
		&entry.BucketIDSnapshot,
		&allocationID,
		&targetUserID,
		&entry.BeforeAmount,
		&entry.DeltaAmount,
		&entry.AfterAmount,
		&entry.BalanceVersionBefore,
		&entry.BalanceVersionAfter,
		&entry.CreatedAt,
	)
	if err != nil {
		return entry, err
	}
	if publicReason.Valid {
		entry.PublicReason = &publicReason.String
	}
	entry.AllocationIDSnapshot = nullableInt64(allocationID)
	entry.TargetUserIDSnapshot = nullableInt64(targetUserID)
	return entry, nil
}

func validateCreditCommand(input service.OrganizationCreditCommand) error {
	if input.OrganizationID <= 0 || input.OperationType != domain.OrganizationFundingOperationCredit ||
		input.AuthorizationScope != domain.OrganizationFundingAuthorizationPlatformAdminJWT ||
		input.Currency != domain.OrganizationCurrencyUSD || input.Actor.UserID <= 0 ||
		input.Actor.PlatformRole != domain.RoleAdmin || !input.Actor.Active ||
		input.Actor.AuthorizationScope != input.AuthorizationScope {
		return service.ErrOrganizationFundingForbidden
	}
	if !validSHA256(input.IdempotencyKeyHash) || !validSHA256(input.CanonicalPayloadHash) {
		return service.ErrIdempotencyKeyInvalid
	}
	if strings.TrimSpace(input.InternalReason) == "" || strings.TrimSpace(input.Actor.DisplayName) == "" ||
		len(strings.TrimSpace(input.InternalReason)) > 10_000 || len(strings.TrimSpace(input.Actor.DisplayName)) > 255 {
		return service.ErrOrganizationFundingReasonRequired
	}
	if input.PublicReason != nil && len(strings.TrimSpace(*input.PublicReason)) > 2_000 {
		return service.ErrOrganizationFundingReasonRequired
	}
	if _, err := uuid.Parse(input.OperationID); err != nil {
		return service.ErrOrganizationFundingInvariant.WithCause(err)
	}
	if _, err := uuid.Parse(input.CorrelationID); err != nil {
		return service.ErrOrganizationFundingInvariant.WithCause(err)
	}
	canonicalAmount, err := service.NormalizeOrganizationMoney(input.Amount, true)
	if err != nil {
		return err
	}
	if canonicalAmount != input.Amount {
		return service.ErrOrganizationFundingInvariant
	}
	return nil
}

func parseCreditAmount(raw string) (decimal.Decimal, error) {
	canonical, err := service.NormalizeOrganizationMoney(raw, true)
	if err != nil {
		return decimal.Zero, err
	}
	parsed, err := decimal.NewFromString(canonical)
	if err != nil {
		return decimal.Zero, service.ErrOrganizationFundingInvariant.WithCause(err)
	}
	return parsed, nil
}

func parseStoredMoney(raw string) (decimal.Decimal, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		value = "0"
	}
	parsed, err := decimal.NewFromString(value)
	if err != nil || parsed.IsNegative() || parsed.Truncate(domain.OrganizationMoneyScale).Cmp(parsed) != 0 {
		if err == nil {
			err = fmt.Errorf("invalid stored organization amount %q", raw)
		}
		return decimal.Zero, err
	}
	return parsed, nil
}

func fixedMoney(value decimal.Decimal) string {
	return value.StringFixed(domain.OrganizationMoneyScale)
}

func maxOrganizationMoney() decimal.Decimal {
	return decimal.RequireFromString("999999999999.99999999")
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func nullableInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	v := value.Int64
	return &v
}

func cloneOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func scanOne(ctx context.Context, client *dbent.Client, query string, args []any, destinations ...any) error {
	rows, err := client.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	return rows.Scan(destinations...)
}

func fundingStorageError(operation string, err error) error {
	return service.ErrOrganizationFundingRepositoryUnavailable.WithCause(fmt.Errorf("%s: %w", operation, err))
}

func translateFundingWriteError(err error) error {
	if err == nil {
		return nil
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Constraint == "org_ledger_operations_idempotency_scope_unique" {
		return service.ErrIdempotencyKeyConflict.WithCause(err)
	}
	return fundingStorageError("write organization ledger", err)
}
