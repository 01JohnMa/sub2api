package service

import (
	"log/slog"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/ip"
)

// API Key status constants
const (
	StatusAPIKeyActive         = "active"
	StatusAPIKeyDisabled       = "disabled"
	StatusAPIKeyQuotaExhausted = "quota_exhausted"
	StatusAPIKeyExpired        = "expired"
)

// Rate limit window durations
const (
	RateLimitWindow5h = 5 * time.Hour
	RateLimitWindow1d = 24 * time.Hour
	RateLimitWindow7d = 7 * 24 * time.Hour
)

// IsWindowExpired returns true if the window starting at windowStart has exceeded the given duration.
// A nil windowStart is treated as expired — no initialized window means any accumulated usage is stale.
func IsWindowExpired(windowStart *time.Time, duration time.Duration) bool {
	return windowStart == nil || time.Since(*windowStart) >= duration
}

type APIKey struct {
	ID      int64
	UserID  int64
	Key     string
	Name    string
	GroupID *int64

	OrganizationID *int64
	// OrganizationAuthorization is populated only by the uncached authoritative
	// authentication lookup. A nil value denotes a verified personal key.
	OrganizationAuthorization *OrganizationKeyAuthorization

	Status      string
	IPWhitelist []string
	IPBlacklist []string
	// 预编译的 IP 规则，用于认证热路径避免重复 ParseIP/ParseCIDR。
	CompiledIPWhitelist *ip.CompiledIPRules `json:"-"`
	CompiledIPBlacklist *ip.CompiledIPRules `json:"-"`
	LastUsedAt          *time.Time
	LastUsedIP          *string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	User                *User
	Group               *Group
	CurrentConcurrency  int

	// Quota fields
	Quota     float64    // Quota limit in USD (0 = unlimited)
	QuotaUsed float64    // Used quota amount
	ExpiresAt *time.Time // Expiration time (nil = never expires)

	// Rate limit fields
	RateLimit5h   float64    // Rate limit in USD per 5h (0 = unlimited)
	RateLimit1d   float64    // Rate limit in USD per 1d (0 = unlimited)
	RateLimit7d   float64    // Rate limit in USD per 7d (0 = unlimited)
	Usage5h       float64    // Used amount in current 5h window
	Usage1d       float64    // Used amount in current 1d window
	Usage7d       float64    // Used amount in current 7d window
	Window5hStart *time.Time // Start of current 5h window
	Window1dStart *time.Time // Start of current 1d window
	Window7dStart *time.Time // Start of current 7d window
}

// OrganizationKeyAuthorization is the trusted, secret-free organization
// attribution snapshot for one API key execution. It is intentionally limited
// to authorization and funding identity fields; callers must not derive it
// from request data or cached API-key snapshots.
type OrganizationKeyAuthorization struct {
	APIKeyID                   int64
	UserID                     int64
	OrganizationID             int64
	OrganizationStatus         string
	OrganizationBalanceVersion int64
	AllocationID               int64
	AllocationUserID           int64
	AllocationOrganizationID   int64
	AllocationStatus           string
	AllocationVersion          int64
	Available                  string
	Reserved                   string
}

// ReportOrganizationKeyAttributionAlert emits the minimal diagnostic payload
// allowed on the authentication path. In particular it deliberately omits the
// raw API key, display names, email addresses, and monetary values.
func ReportOrganizationKeyAttributionAlert(reason string, auth *OrganizationKeyAuthorization) {
	attrs := []any{"reason", organizationKeyAttributionAlertReason(reason)}
	if auth != nil {
		attrs = append(attrs,
			"api_key_id", auth.APIKeyID,
			"user_id", auth.UserID,
			"organization_id", auth.OrganizationID,
			"allocation_id", auth.AllocationID,
			"allocation_user_id", auth.AllocationUserID,
			"allocation_organization_id", auth.AllocationOrganizationID,
		)
	}
	slog.Warn("organization API key attribution rejected", attrs...)
}

func organizationKeyAttributionAlertReason(reason string) string {
	if len(reason) == 0 || len(reason) > 64 {
		return "invalid_reason"
	}
	for _, r := range reason {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return "invalid_reason"
	}
	return reason
}

func (k *APIKey) IsActive() bool {
	return k.Status == StatusActive
}

// HasRateLimits returns true if any rate limit window is configured
func (k *APIKey) HasRateLimits() bool {
	return k.RateLimit5h > 0 || k.RateLimit1d > 0 || k.RateLimit7d > 0
}

// IsExpired checks if the API key has expired
func (k *APIKey) IsExpired() bool {
	if k.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*k.ExpiresAt)
}

// IsQuotaExhausted checks if the API key quota is exhausted
func (k *APIKey) IsQuotaExhausted() bool {
	if k.Quota <= 0 {
		return false // unlimited
	}
	return k.QuotaUsed >= k.Quota
}

// GetQuotaRemaining returns remaining quota (-1 for unlimited)
func (k *APIKey) GetQuotaRemaining() float64 {
	if k.Quota <= 0 {
		return -1 // unlimited
	}
	remaining := k.Quota - k.QuotaUsed
	if remaining < 0 {
		return 0
	}
	return remaining
}

// GetDaysUntilExpiry returns days until expiry (-1 for never expires)
func (k *APIKey) GetDaysUntilExpiry() int {
	if k.ExpiresAt == nil {
		return -1 // never expires
	}
	duration := time.Until(*k.ExpiresAt)
	if duration < 0 {
		return 0
	}
	return int(duration.Hours() / 24)
}

// EffectiveUsage5h returns the 5h window usage, or 0 if the window has expired.
func (k *APIKey) EffectiveUsage5h() float64 {
	if IsWindowExpired(k.Window5hStart, RateLimitWindow5h) {
		return 0
	}
	return k.Usage5h
}

// EffectiveUsage1d returns the 1d window usage, or 0 if the window has expired.
func (k *APIKey) EffectiveUsage1d() float64 {
	if IsWindowExpired(k.Window1dStart, RateLimitWindow1d) {
		return 0
	}
	return k.Usage1d
}

// EffectiveUsage7d returns the 7d window usage, or 0 if the window has expired.
func (k *APIKey) EffectiveUsage7d() float64 {
	if IsWindowExpired(k.Window7dStart, RateLimitWindow7d) {
		return 0
	}
	return k.Usage7d
}

// APIKeyListFilters holds optional filtering parameters for listing API keys.
type APIKeyListFilters struct {
	Search  string
	Status  string
	GroupID *int64 // nil=不筛选, 0=无分组, >0=指定分组
}
