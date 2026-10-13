package dto

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type OrganizationAllocationMemberPreview struct {
	UserID           int64  `json:"user_id"`
	UserStatus       string `json:"user_status"`
	OrganizationRole string `json:"organization_role"`
	AllocationID     int64  `json:"allocation_id"`
	AllocationStatus string `json:"allocation_status"`
	Available        string `json:"available"`
	Reserved         string `json:"reserved"`
	ExcludedReason   string `json:"excluded_reason,omitempty"`
}

type OrganizationAllocationTargetPreview struct {
	UserID       int64  `json:"user_id"`
	AllocationID int64  `json:"allocation_id"`
	Before       string `json:"before"`
	Delta        string `json:"delta"`
	After        string `json:"after"`
}

type OrganizationAllocationConfirmation struct {
	OrganizationID     int64                                 `json:"organization_id"`
	AuthorizationScope string                                `json:"authorization_scope"`
	OperationType      string                                `json:"operation_type"`
	Currency           string                                `json:"currency"`
	MoneyScale         int                                   `json:"money_scale"`
	OrganizationAt     string                                `json:"organization_at"`
	BalanceVersion     int64                                 `json:"balance_version"`
	SnapshotHash       string                                `json:"snapshot_hash"`
	PoolBefore         string                                `json:"pool_before"`
	PoolAfter          string                                `json:"pool_after"`
	TotalDelta         string                                `json:"total_delta"`
	RoundingRemainder  string                                `json:"rounding_remainder"`
	PublicReason       string                                `json:"public_reason,omitempty"`
	NoOpReason         string                                `json:"no_op_reason,omitempty"`
	Targets            []OrganizationAllocationTargetPreview `json:"targets"`
}

type OrganizationAllocationPreview struct {
	Confirmation         OrganizationAllocationConfirmation    `json:"confirmation"`
	EligibleMembers      []OrganizationAllocationMemberPreview `json:"eligible_members"`
	ExcludedMembers      []OrganizationAllocationMemberPreview `json:"excluded_members"`
	CanonicalPayloadHash string                                `json:"canonical_payload_hash"`
	PreviewToken         string                                `json:"preview_token"`
	ExpiresAt            time.Time                             `json:"expires_at"`
}

// OrganizationAllocationResult intentionally omits internal ledger postings,
// actor IDs, idempotency hashes, and internal audit reasons.
type OrganizationAllocationResult struct {
	OperationID          string `json:"operation_id,omitempty"`
	CorrelationID        string `json:"correlation_id,omitempty"`
	OrganizationID       int64  `json:"organization_id"`
	OperationType        string `json:"operation_type"`
	Currency             string `json:"currency"`
	Outcome              string `json:"outcome"`
	NoOpReason           string `json:"no_op_reason,omitempty"`
	PoolBefore           string `json:"pool_before"`
	PoolAfter            string `json:"pool_after"`
	TotalDelta           string `json:"total_delta"`
	BalanceVersionBefore int64  `json:"balance_version_before"`
	BalanceVersionAfter  int64  `json:"balance_version_after"`
	Replayed             bool   `json:"replayed"`
}

func OrganizationAllocationPreviewFromService(preview *service.OrganizationAllocationPreview) *OrganizationAllocationPreview {
	if preview == nil {
		return nil
	}
	return &OrganizationAllocationPreview{
		Confirmation:         organizationAllocationConfirmationFromService(preview.Confirmation),
		EligibleMembers:      organizationAllocationMembersFromService(preview.EligibleMembers),
		ExcludedMembers:      organizationAllocationMembersFromService(preview.ExcludedMembers),
		CanonicalPayloadHash: preview.CanonicalPayloadHash,
		PreviewToken:         preview.PreviewToken,
		ExpiresAt:            preview.ExpiresAt,
	}
}

func OrganizationAllocationResultFromService(result *service.OrganizationAllocationResult) *OrganizationAllocationResult {
	if result == nil {
		return nil
	}
	return &OrganizationAllocationResult{
		OperationID:          result.OperationID,
		CorrelationID:        result.CorrelationID,
		OrganizationID:       result.OrganizationID,
		OperationType:        result.OperationType,
		Currency:             result.Currency,
		Outcome:              result.Outcome,
		NoOpReason:           result.NoOpReason,
		PoolBefore:           result.PoolBefore,
		PoolAfter:            result.PoolAfter,
		TotalDelta:           result.TotalDelta,
		BalanceVersionBefore: result.BalanceVersionBefore,
		BalanceVersionAfter:  result.BalanceVersionAfter,
		Replayed:             result.Replayed,
	}
}

func organizationAllocationConfirmationFromService(input service.OrganizationAllocationConfirmation) OrganizationAllocationConfirmation {
	targets := make([]OrganizationAllocationTargetPreview, 0, len(input.Targets))
	for _, target := range input.Targets {
		targets = append(targets, OrganizationAllocationTargetPreview{
			UserID:       target.UserID,
			AllocationID: target.AllocationID,
			Before:       target.Before,
			Delta:        target.Delta,
			After:        target.After,
		})
	}
	return OrganizationAllocationConfirmation{
		OrganizationID:     input.OrganizationID,
		AuthorizationScope: input.AuthorizationScope,
		OperationType:      input.OperationType,
		Currency:           input.Currency,
		MoneyScale:         input.MoneyScale,
		OrganizationAt:     input.OrganizationAt,
		BalanceVersion:     input.BalanceVersion,
		SnapshotHash:       input.SnapshotHash,
		PoolBefore:         input.PoolBefore,
		PoolAfter:          input.PoolAfter,
		TotalDelta:         input.TotalDelta,
		RoundingRemainder:  input.RoundingRemainder,
		PublicReason:       input.PublicReason,
		NoOpReason:         input.NoOpReason,
		Targets:            targets,
	}
}

func organizationAllocationMembersFromService(input []service.OrganizationAllocationMemberPreview) []OrganizationAllocationMemberPreview {
	result := make([]OrganizationAllocationMemberPreview, 0, len(input))
	for _, member := range input {
		result = append(result, OrganizationAllocationMemberPreview{
			UserID:           member.UserID,
			UserStatus:       member.UserStatus,
			OrganizationRole: member.OrganizationRole,
			AllocationID:     member.AllocationID,
			AllocationStatus: member.AllocationStatus,
			Available:        member.Available,
			Reserved:         member.Reserved,
			ExcludedReason:   member.ExcludedReason,
		})
	}
	return result
}
