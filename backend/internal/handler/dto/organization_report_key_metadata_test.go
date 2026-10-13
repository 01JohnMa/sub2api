//go:build unit

package dto

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOrganizationAPIKeyMetadataDTOIsSecretFree(t *testing.T) {
	createdAt := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	payload, err := json.Marshal(OrganizationAPIKeyMetadataFromService(&service.OrganizationAPIKeyMetadata{
		ID: 8, Name: "build", Status: service.StatusAPIKeyActive, UserID: 11, MemberName: "member", OrganizationID: 7,
		CreatedAt: createdAt, Quota: 12.5, QuotaUsed: 1.25,
	}))
	require.NoError(t, err)
	body := string(payload)
	require.Contains(t, body, `"id":8`)
	require.Contains(t, body, `"quota_used":1.25`)
	for _, forbidden := range []string{"\"key\"", "ip_whitelist", "ip_blacklist", "credential", "rate_limit", "usage_5h", "balance"} {
		require.NotContains(t, body, forbidden)
	}
}
