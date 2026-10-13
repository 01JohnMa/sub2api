package service

import "time"

// Organization is the platform tenancy record. Membership, allocations, and
// organization-key attribution are added by later organization tickets.
type Organization struct {
	ID          int64
	Name        string
	Code        string
	Description string
	Status      string
	CreatedBy   int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (o *Organization) IsActive() bool {
	return o != nil && o.Status == OrganizationStatusActive
}
