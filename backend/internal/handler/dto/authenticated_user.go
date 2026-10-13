package dto

import "time"

// AuthenticatedUserIdentity is the stable identity payload required by the
// frontend for every signed-in user.
type AuthenticatedUserIdentity struct {
	ID           int64      `json:"id"`
	Email        string     `json:"email"`
	Username     string     `json:"username"`
	Role         string     `json:"role"`
	Status       string     `json:"status"`
	LastActiveAt *time.Time `json:"last_active_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	DeletedAt    *time.Time `json:"deleted_at,omitempty"`
}

// AuthenticatedUserPersonalFields is embedded only when the current identity
// may use personal-account APIs. A nil embedded pointer omits every field from
// JSON instead of fabricating zero values for organization administrators.
type AuthenticatedUserPersonalFields struct {
	Balance                    float64            `json:"balance"`
	Concurrency                int                `json:"concurrency"`
	AllowedGroups              []int64            `json:"allowed_groups"`
	BalanceNotifyEnabled       bool               `json:"balance_notify_enabled"`
	BalanceNotifyThresholdType string             `json:"balance_notify_threshold_type"`
	BalanceNotifyThreshold     *float64           `json:"balance_notify_threshold"`
	BalanceNotifyExtraEmails   []NotifyEmailEntry `json:"balance_notify_extra_emails"`
	TotalRecharged             float64            `json:"total_recharged"`
	RPMLimit                   int                `json:"rpm_limit"`
	APIKeys                    []APIKey           `json:"api_keys,omitempty"`
	Subscriptions              []UserSubscription `json:"subscriptions,omitempty"`
}

// AuthenticatedUser keeps identity and optional personal-account fields flat
// for compatibility with the existing auth response shape.
type AuthenticatedUser struct {
	AuthenticatedUserIdentity
	*AuthenticatedUserPersonalFields
}
