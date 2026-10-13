package service

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReportOrganizationKeyAttributionAlertIsSecretSafe(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	ReportOrganizationKeyAttributionAlert("bad reason raw-secret", &OrganizationKeyAuthorization{
		APIKeyID:                 11,
		UserID:                   22,
		OrganizationID:           33,
		AllocationID:             44,
		AllocationUserID:         22,
		AllocationOrganizationID: 33,
		Available:                "do-not-log-available",
		Reserved:                 "do-not-log-reserved",
	})

	logged := output.String()
	require.Contains(t, logged, `"reason":"invalid_reason"`)
	require.Contains(t, logged, `"api_key_id":11`)
	require.NotContains(t, logged, "raw-secret")
	require.NotContains(t, logged, "do-not-log-available")
	require.NotContains(t, logged, "do-not-log-reserved")
	require.False(t, strings.Contains(strings.ToLower(logged), "email"))
}
