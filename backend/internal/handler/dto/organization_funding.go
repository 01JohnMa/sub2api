package dto

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type OrganizationBalanceSummary struct {
	OrganizationID int64  `json:"organization_id"`
	Status         string `json:"status"`
	Currency       string `json:"currency"`
	Unallocated    string `json:"unallocated"`
	Available      string `json:"available"`
	Reserved       string `json:"reserved"`
	Consumed       string `json:"consumed"`
	TotalRemaining string `json:"total_remaining"`
	BalanceVersion int64  `json:"balance_version"`
}

type OrganizationLedgerEntry struct {
	PostingID            int64     `json:"posting_id"`
	PostingSequence      int       `json:"posting_sequence"`
	OperationID          string    `json:"operation_id"`
	CorrelationID        string    `json:"correlation_id"`
	OrganizationID       int64     `json:"organization_id"`
	OperationType        string    `json:"operation_type"`
	AuthorizationScope   string    `json:"authorization_scope"`
	ActorIDSnapshot      int64     `json:"actor_id_snapshot"`
	ActorDisplaySnapshot string    `json:"actor_display_snapshot"`
	ActorRoleSnapshot    string    `json:"actor_role_snapshot"`
	Currency             string    `json:"currency"`
	IdempotencyKeyHash   string    `json:"idempotency_key_hash"`
	CanonicalPayloadHash string    `json:"canonical_payload_hash"`
	PublicReason         *string   `json:"public_reason"`
	InternalReason       string    `json:"internal_reason"`
	BucketType           string    `json:"bucket_type"`
	BucketIDSnapshot     int64     `json:"bucket_id_snapshot"`
	AllocationIDSnapshot *int64    `json:"allocation_id_snapshot"`
	TargetUserIDSnapshot *int64    `json:"target_user_id_snapshot"`
	BeforeAmount         string    `json:"before_amount"`
	DeltaAmount          string    `json:"delta_amount"`
	AfterAmount          string    `json:"after_amount"`
	BalanceVersionBefore int64     `json:"balance_version_before"`
	BalanceVersionAfter  int64     `json:"balance_version_after"`
	OccurredAt           time.Time `json:"occurred_at"`
	CreatedAt            time.Time `json:"created_at"`
}

type OrganizationFundingResult struct {
	OperationID          string                  `json:"operation_id"`
	CorrelationID        string                  `json:"correlation_id"`
	OrganizationID       int64                   `json:"organization_id"`
	Currency             string                  `json:"currency"`
	Amount               string                  `json:"amount"`
	PoolBefore           string                  `json:"pool_before"`
	PoolAfter            string                  `json:"pool_after"`
	BalanceVersionBefore int64                   `json:"balance_version_before"`
	BalanceVersionAfter  int64                   `json:"balance_version_after"`
	Ledger               OrganizationLedgerEntry `json:"ledger"`
	Replayed             bool                    `json:"replayed"`
}

func OrganizationBalanceSummaryFromService(summary *service.OrganizationBalanceSummary) *OrganizationBalanceSummary {
	if summary == nil {
		return nil
	}
	return &OrganizationBalanceSummary{
		OrganizationID: summary.OrganizationID,
		Status:         summary.Status,
		Currency:       summary.Currency,
		Unallocated:    summary.Unallocated,
		Available:      summary.Available,
		Reserved:       summary.Reserved,
		Consumed:       summary.Consumed,
		TotalRemaining: summary.TotalRemaining,
		BalanceVersion: summary.BalanceVersion,
	}
}

func OrganizationLedgerEntryFromService(entry *service.OrganizationLedgerEntry) *OrganizationLedgerEntry {
	if entry == nil {
		return nil
	}
	return &OrganizationLedgerEntry{
		PostingID:            entry.PostingID,
		PostingSequence:      entry.PostingSequence,
		OperationID:          entry.OperationID,
		CorrelationID:        entry.CorrelationID,
		OrganizationID:       entry.OrganizationID,
		OperationType:        entry.OperationType,
		AuthorizationScope:   entry.AuthorizationScope,
		ActorIDSnapshot:      entry.ActorIDSnapshot,
		ActorDisplaySnapshot: entry.ActorDisplaySnapshot,
		ActorRoleSnapshot:    entry.ActorRoleSnapshot,
		Currency:             entry.Currency,
		IdempotencyKeyHash:   entry.IdempotencyKeyHash,
		CanonicalPayloadHash: entry.CanonicalPayloadHash,
		PublicReason:         entry.PublicReason,
		InternalReason:       entry.InternalReason,
		BucketType:           entry.BucketType,
		BucketIDSnapshot:     entry.BucketIDSnapshot,
		AllocationIDSnapshot: entry.AllocationIDSnapshot,
		TargetUserIDSnapshot: entry.TargetUserIDSnapshot,
		BeforeAmount:         entry.BeforeAmount,
		DeltaAmount:          entry.DeltaAmount,
		AfterAmount:          entry.AfterAmount,
		BalanceVersionBefore: entry.BalanceVersionBefore,
		BalanceVersionAfter:  entry.BalanceVersionAfter,
		OccurredAt:           entry.OccurredAt,
		CreatedAt:            entry.CreatedAt,
	}
}

func OrganizationFundingResultFromService(result *service.OrganizationFundingResult) *OrganizationFundingResult {
	if result == nil {
		return nil
	}
	ledger := OrganizationLedgerEntryFromService(&result.Ledger)
	return &OrganizationFundingResult{
		OperationID:          result.OperationID,
		CorrelationID:        result.CorrelationID,
		OrganizationID:       result.OrganizationID,
		Currency:             result.Currency,
		Amount:               result.Amount,
		PoolBefore:           result.PoolBefore,
		PoolAfter:            result.PoolAfter,
		BalanceVersionBefore: result.BalanceVersionBefore,
		BalanceVersionAfter:  result.BalanceVersionAfter,
		Ledger:               *ledger,
		Replayed:             result.Replayed,
	}
}
