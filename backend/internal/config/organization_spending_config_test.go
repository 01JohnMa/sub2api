package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestLoadDefaultOrganizationSpendingConfigIsFailClosed(t *testing.T) {
	resetViperWithJWTSecret(t)

	cfg, err := Load()

	require.NoError(t, err)
	require.False(t, cfg.Gateway.OrganizationSpending.Enabled)
	require.Empty(t, cfg.Gateway.OrganizationSpending.Capabilities)
}

func TestConfigExampleYAMLIncludesFailClosedOrganizationSpendingGate(t *testing.T) {
	path := filepath.Join("..", "..", "..", "deploy", "config.example.yaml")
	content, err := os.ReadFile(path)
	require.NoError(t, err)

	var document map[string]any
	require.NoError(t, yaml.Unmarshal(content, &document))
	gateway, ok := document["gateway"].(map[string]any)
	require.True(t, ok)
	spending, ok := gateway["organization_spending"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, false, spending["enabled"])
	require.Empty(t, spending["capabilities"])
}
