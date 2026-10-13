package dto

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Organization is the non-secret platform organization representation.
type Organization struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Code        string    `json:"code"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	CreatedBy   int64     `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// OrganizationSummary is the secret-safe current-organization identity used
// by auth responses and organization-admin endpoints. Actor IDs and platform
// audit fields are deliberately omitted.
type OrganizationSummary struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Code        string `json:"code"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

func OrganizationFromService(organization *service.Organization) *Organization {
	if organization == nil {
		return nil
	}
	return &Organization{
		ID:          organization.ID,
		Name:        organization.Name,
		Code:        organization.Code,
		Description: organization.Description,
		Status:      organization.Status,
		CreatedBy:   organization.CreatedBy,
		CreatedAt:   organization.CreatedAt,
		UpdatedAt:   organization.UpdatedAt,
	}
}

type OrganizationArchivePreview struct {
	OrganizationID int64     `json:"organization_id"`
	Status         string    `json:"status"`
	Unallocated    string    `json:"unallocated"`
	BalanceVersion int64     `json:"balance_version"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func OrganizationArchivePreviewFromService(preview *service.OrganizationArchivePreview) *OrganizationArchivePreview {
	if preview == nil {
		return nil
	}
	return &OrganizationArchivePreview{
		OrganizationID: preview.OrganizationID,
		Status:         preview.Status,
		Unallocated:    preview.Unallocated,
		BalanceVersion: preview.BalanceVersion,
		UpdatedAt:      preview.UpdatedAt,
	}
}

func OrganizationSummaryFromService(organization *service.Organization) *OrganizationSummary {
	if organization == nil {
		return nil
	}
	return &OrganizationSummary{
		ID:          organization.ID,
		Name:        organization.Name,
		Code:        organization.Code,
		Description: organization.Description,
		Status:      organization.Status,
	}
}

type OrganizationPendingReconciliation struct {
	ExecutionID                string                                      `json:"execution_id"`
	OrganizationID             int64                                       `json:"organization_id"`
	AllocationID               int64                                       `json:"allocation_id"`
	ReservationID              int64                                       `json:"reservation_id"`
	CandidateCeiling           string                                      `json:"candidate_ceiling"`
	ReservedAmount             string                                      `json:"reserved_amount"`
	AcceptedAt                 time.Time                                   `json:"accepted_at"`
	AttemptCount               int                                         `json:"attempt_count"`
	Attempts                   []OrganizationPendingReconciliationAttempt  `json:"attempts"`
	Evidence                   []OrganizationPendingReconciliationEvidence `json:"evidence"`
	AllowedResolution          string                                      `json:"allowed_resolution,omitempty"`
	ComputedOrganizationAmount string                                      `json:"computed_organization_amount,omitempty"`
	Conflict                   bool                                        `json:"conflict"`
}

type OrganizationPendingReconciliationAttempt struct {
	AttemptID       string `json:"attempt_id"`
	AttemptSequence int    `json:"attempt_sequence"`
	Status          string `json:"status"`
}

type OrganizationPendingReconciliationEvidence struct {
	AttemptID           string    `json:"attempt_id"`
	AttemptSequence     int       `json:"attempt_sequence"`
	EvidenceSource      string    `json:"evidence_source"`
	TerminalOutcome     string    `json:"terminal_outcome"`
	RawActualCost       string    `json:"raw_actual_cost"`
	InputTokens         int64     `json:"input_tokens"`
	OutputTokens        int64     `json:"output_tokens"`
	CacheCreationTokens int64     `json:"cache_creation_tokens"`
	CacheReadTokens     int64     `json:"cache_read_tokens"`
	ProviderObservedAt  time.Time `json:"provider_observed_at"`
}

func OrganizationPendingReconciliationFromService(item *service.OrganizationPendingReconciliation) *OrganizationPendingReconciliation {
	if item == nil {
		return nil
	}
	evidence := make([]OrganizationPendingReconciliationEvidence, 0, len(item.Evidence))
	for _, record := range item.Evidence {
		evidence = append(evidence, OrganizationPendingReconciliationEvidence{
			AttemptID: record.AttemptID, AttemptSequence: record.AttemptSequence,
			EvidenceSource: record.EvidenceSource, TerminalOutcome: record.TerminalOutcome,
			RawActualCost: record.RawActualCost, InputTokens: record.InputTokens,
			OutputTokens: record.OutputTokens, CacheCreationTokens: record.CacheCreationTokens,
			CacheReadTokens: record.CacheReadTokens, ProviderObservedAt: record.ProviderObservedAt,
		})
	}
	attempts := make([]OrganizationPendingReconciliationAttempt, 0, len(item.Attempts))
	for _, attempt := range item.Attempts {
		attempts = append(attempts, OrganizationPendingReconciliationAttempt{
			AttemptID: attempt.AttemptID, AttemptSequence: attempt.AttemptSequence, Status: attempt.Status,
		})
	}
	return &OrganizationPendingReconciliation{
		ExecutionID: item.ExecutionID, OrganizationID: item.OrganizationID, AllocationID: item.AllocationID,
		ReservationID: item.ReservationID, CandidateCeiling: item.CandidateCeiling, ReservedAmount: item.ReservedAmount,
		AcceptedAt: item.AcceptedAt, AttemptCount: item.AttemptCount, Attempts: attempts, Evidence: evidence,
		AllowedResolution: item.AllowedResolution, ComputedOrganizationAmount: item.ComputedOrganizationAmount,
		Conflict: item.Conflict,
	}
}

type OrganizationPendingReconciliationResolution struct {
	ExecutionID             string     `json:"execution_id"`
	OrganizationID          int64      `json:"organization_id"`
	ReservationID           int64      `json:"reservation_id"`
	ReservationState        string     `json:"reservation_state"`
	TerminalOutcome         string     `json:"terminal_outcome"`
	OrganizationSettledCost string     `json:"organization_settled_cost"`
	OccurredAt              time.Time  `json:"occurred_at"`
	ReconciledAt            *time.Time `json:"reconciled_at,omitempty"`
	Replayed                bool       `json:"replayed"`
}

// OrganizationPendingReconciliationSummary is intentionally the only
// reconciliation representation exposed to organization administrators.
type OrganizationPendingReconciliationSummary struct {
	Count  int64  `json:"count"`
	Status string `json:"status"`
}

func OrganizationPendingReconciliationSummaryFromService(summary *service.OrganizationPendingReconciliationSummary) *OrganizationPendingReconciliationSummary {
	if summary == nil {
		return nil
	}
	return &OrganizationPendingReconciliationSummary{Count: summary.Count, Status: summary.Status}
}

func OrganizationPendingReconciliationResolutionFromService(result *service.OrganizationFinalExecutionEvidenceResult) *OrganizationPendingReconciliationResolution {
	if result == nil {
		return nil
	}
	return &OrganizationPendingReconciliationResolution{
		ExecutionID: result.ExecutionID, OrganizationID: result.OrganizationID, ReservationID: result.ReservationID,
		ReservationState: result.ReservationState, TerminalOutcome: result.TerminalOutcome,
		OrganizationSettledCost: result.OrganizationSettledCost, OccurredAt: result.OccurredAt,
		ReconciledAt: result.ReconciledAt, Replayed: result.Replayed,
	}
}
