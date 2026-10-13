package dto

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type OrganizationReport struct {
	OrganizationID       int64                     `json:"organization_id"`
	Start                time.Time                 `json:"start"`
	End                  time.Time                 `json:"end"`
	CurrentMemberCount   int64                     `json:"current_member_count"`
	ActiveMemberCount    int64                     `json:"active_member_count"`
	APIKeyCount          int64                     `json:"api_key_count"`
	RequestCountTotal    int64                     `json:"request_count_total"`
	SuccessCount         int64                     `json:"success_count"`
	ErrorCountTotal      int64                     `json:"error_count_total"`
	BusinessLimitedCount int64                     `json:"business_limited_count"`
	ErrorCountSLA        int64                     `json:"error_count_sla"`
	SuccessRate          float64                   `json:"success_rate"`
	ErrorRate            float64                   `json:"error_rate"`
	InputTokens          int64                     `json:"input_tokens"`
	OutputTokens         int64                     `json:"output_tokens"`
	CacheCreationTokens  int64                     `json:"cache_creation_tokens"`
	CacheReadTokens      int64                     `json:"cache_read_tokens"`
	Cost                 string                    `json:"cost"`
	Models               []OrganizationReportModel `json:"models"`
	Trends               []OrganizationReportTrend `json:"trends"`
}

type OrganizationReportModel struct {
	Model               string `json:"model"`
	RequestCount        int64  `json:"request_count"`
	InputTokens         int64  `json:"input_tokens"`
	OutputTokens        int64  `json:"output_tokens"`
	CacheCreationTokens int64  `json:"cache_creation_tokens"`
	CacheReadTokens     int64  `json:"cache_read_tokens"`
	Cost                string `json:"cost"`
}

type OrganizationReportTrend struct {
	OccurredOn          string `json:"occurred_on"`
	RequestCount        int64  `json:"request_count"`
	SuccessCount        int64  `json:"success_count"`
	ErrorCountTotal     int64  `json:"error_count_total"`
	InputTokens         int64  `json:"input_tokens"`
	OutputTokens        int64  `json:"output_tokens"`
	CacheCreationTokens int64  `json:"cache_creation_tokens"`
	CacheReadTokens     int64  `json:"cache_read_tokens"`
	Cost                string `json:"cost"`
}

type OrganizationReportMemberUsage struct {
	UserID              *int64 `json:"user_id,omitempty"`
	DisplayName         string `json:"display_name"`
	RequestCount        int64  `json:"request_count"`
	SuccessCount        int64  `json:"success_count"`
	ErrorCountTotal     int64  `json:"error_count_total"`
	InputTokens         int64  `json:"input_tokens"`
	OutputTokens        int64  `json:"output_tokens"`
	CacheCreationTokens int64  `json:"cache_creation_tokens"`
	CacheReadTokens     int64  `json:"cache_read_tokens"`
	Cost                string `json:"cost"`
}

// OrganizationAPIKeyMetadata is the only key representation available from
// current-organization routes. It must remain narrower than the user/admin
// key DTO, which deliberately contains mutable and secret-adjacent fields.
type OrganizationAPIKeyMetadata struct {
	ID             int64      `json:"id"`
	Name           string     `json:"name"`
	Status         string     `json:"status"`
	UserID         int64      `json:"user_id"`
	MemberName     string     `json:"member_name"`
	OrganizationID int64      `json:"organization_id"`
	CreatedAt      time.Time  `json:"created_at"`
	LastUsedAt     *time.Time `json:"last_used_at"`
	ExpiresAt      *time.Time `json:"expires_at"`
	Quota          float64    `json:"quota"`
	QuotaUsed      float64    `json:"quota_used"`
}

// OrganizationPublicLedgerEntry excludes actor IDs, authorization metadata,
// internal reasons, hashes, and any provider/evidence details.
type OrganizationPublicLedgerEntry struct {
	EntryKey      string    `json:"entry_key"`
	OperationType string    `json:"operation_type"`
	PublicReason  *string   `json:"public_reason"`
	ActorDisplay  string    `json:"actor_display"`
	BucketType    string    `json:"bucket_type"`
	BeforeAmount  string    `json:"before_amount"`
	DeltaAmount   string    `json:"delta_amount"`
	AfterAmount   string    `json:"after_amount"`
	OccurredAt    time.Time `json:"occurred_at"`
}

func OrganizationReportFromService(report *service.OrganizationReport) *OrganizationReport {
	if report == nil {
		return nil
	}
	models := make([]OrganizationReportModel, 0, len(report.Models))
	for _, model := range report.Models {
		models = append(models, OrganizationReportModel{Model: model.Model, RequestCount: model.RequestCount, InputTokens: model.InputTokens, OutputTokens: model.OutputTokens, CacheCreationTokens: model.CacheCreationTokens, CacheReadTokens: model.CacheReadTokens, Cost: model.Cost})
	}
	trends := make([]OrganizationReportTrend, 0, len(report.Trends))
	for _, trend := range report.Trends {
		trends = append(trends, OrganizationReportTrend{OccurredOn: trend.OccurredOn, RequestCount: trend.RequestCount, SuccessCount: trend.SuccessCount, ErrorCountTotal: trend.ErrorCountTotal, InputTokens: trend.InputTokens, OutputTokens: trend.OutputTokens, CacheCreationTokens: trend.CacheCreationTokens, CacheReadTokens: trend.CacheReadTokens, Cost: trend.Cost})
	}
	return &OrganizationReport{OrganizationID: report.OrganizationID, Start: report.Start, End: report.End, CurrentMemberCount: report.CurrentMemberCount, ActiveMemberCount: report.ActiveMemberCount, APIKeyCount: report.APIKeyCount, RequestCountTotal: report.RequestCountTotal, SuccessCount: report.SuccessCount, ErrorCountTotal: report.ErrorCountTotal, BusinessLimitedCount: report.BusinessLimitedCount, ErrorCountSLA: report.ErrorCountSLA, SuccessRate: report.SuccessRate, ErrorRate: report.ErrorRate, InputTokens: report.InputTokens, OutputTokens: report.OutputTokens, CacheCreationTokens: report.CacheCreationTokens, CacheReadTokens: report.CacheReadTokens, Cost: report.Cost, Models: models, Trends: trends}
}

func OrganizationReportMemberUsageFromService(item *service.OrganizationReportMemberUsage) *OrganizationReportMemberUsage {
	if item == nil {
		return nil
	}
	return &OrganizationReportMemberUsage{UserID: item.UserID, DisplayName: item.DisplayName, RequestCount: item.RequestCount, SuccessCount: item.SuccessCount, ErrorCountTotal: item.ErrorCountTotal, InputTokens: item.InputTokens, OutputTokens: item.OutputTokens, CacheCreationTokens: item.CacheCreationTokens, CacheReadTokens: item.CacheReadTokens, Cost: item.Cost}
}

func OrganizationAPIKeyMetadataFromService(item *service.OrganizationAPIKeyMetadata) *OrganizationAPIKeyMetadata {
	if item == nil {
		return nil
	}
	return &OrganizationAPIKeyMetadata{ID: item.ID, Name: item.Name, Status: item.Status, UserID: item.UserID, MemberName: item.MemberName, OrganizationID: item.OrganizationID, CreatedAt: item.CreatedAt, LastUsedAt: item.LastUsedAt, ExpiresAt: item.ExpiresAt, Quota: item.Quota, QuotaUsed: item.QuotaUsed}
}

func OrganizationPublicLedgerEntryFromService(item *service.OrganizationLedgerEntry) *OrganizationPublicLedgerEntry {
	if item == nil {
		return nil
	}
	sum := sha256.Sum256([]byte(item.OperationID))
	return &OrganizationPublicLedgerEntry{EntryKey: hex.EncodeToString(sum[:8]) + ":" + strconv.Itoa(item.PostingSequence), OperationType: item.OperationType, PublicReason: item.PublicReason, ActorDisplay: publicLedgerActorDisplay(item), BucketType: item.BucketType, BeforeAmount: item.BeforeAmount, DeltaAmount: item.DeltaAmount, AfterAmount: item.AfterAmount, OccurredAt: item.OccurredAt}
}

func publicLedgerActorDisplay(item *service.OrganizationLedgerEntry) string {
	if item == nil || item.OperationType == domain.OrganizationFundingOperationConsumption || item.OperationType == domain.OrganizationFundingOperationReservation || item.OperationType == domain.OrganizationFundingOperationRelease {
		return "system"
	}
	switch item.AuthorizationScope {
	case domain.OrganizationFundingAuthorizationPlatformAdminJWT:
		return "platform administrator"
	case domain.OrganizationFundingAuthorizationOrganizationJWT:
		return "organization administrator"
	default:
		return "organization actor"
	}
}
