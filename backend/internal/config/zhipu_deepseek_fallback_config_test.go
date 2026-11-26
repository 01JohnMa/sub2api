package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const zhipuDeepSeekFallbackYAML = `gateway:
  zhipu_deepseek_fallback:
    enabled: true
    source_group_id: 3
    source_key_id: 42
    target_group_id: 7
    target_model: deepseek-reasoner
`

var validZhipuDeepSeekFallback = GatewayZhipuDeepSeekFallbackConfig{
	Enabled:       true,
	SourceGroupID: 3,
	SourceKeyID:   42,
	TargetGroupID: 7,
	TargetModel:   "deepseek-reasoner",
}

// loadZhipuDeepSeekFallback loads the config through the real pipeline with an
// optional config file and optional gateway.zhipu_deepseek_fallback.* environment
// variables. The remaining environment variables are cleared so each case stays
// independent of the developer's shell.
func loadZhipuDeepSeekFallback(t *testing.T, configYAML string, env map[string]string) (*Config, error) {
	t.Helper()
	resetViperWithJWTSecret(t)
	for _, key := range []string{
		"GATEWAY_ZHIPU_DEEPSEEK_FALLBACK_ENABLED",
		"GATEWAY_ZHIPU_DEEPSEEK_FALLBACK_SOURCE_GROUP_ID",
		"GATEWAY_ZHIPU_DEEPSEEK_FALLBACK_SOURCE_KEY_ID",
		"GATEWAY_ZHIPU_DEEPSEEK_FALLBACK_TARGET_GROUP_ID",
		"GATEWAY_ZHIPU_DEEPSEEK_FALLBACK_TARGET_MODEL",
	} {
		t.Setenv(key, env[key])
	}
	if configYAML != "" {
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(configYAML), 0o600))
		t.Setenv("CONFIG_FILE", path)
	}
	return Load()
}

func TestLoadZhipuDeepSeekFallbackIsDisabledByDefault(t *testing.T) {
	cfg, err := loadZhipuDeepSeekFallback(t, "", nil)

	require.NoError(t, err)
	require.Equal(t, GatewayZhipuDeepSeekFallbackConfig{}, cfg.Gateway.ZhipuDeepSeekFallback)
}

func TestLoadZhipuDeepSeekFallbackParsesYAMLAndEnvironment(t *testing.T) {
	t.Run("yaml", func(t *testing.T) {
		cfg, err := loadZhipuDeepSeekFallback(t, zhipuDeepSeekFallbackYAML, nil)

		require.NoError(t, err)
		require.Equal(t, validZhipuDeepSeekFallback, cfg.Gateway.ZhipuDeepSeekFallback)
	})

	t.Run("environment", func(t *testing.T) {
		cfg, err := loadZhipuDeepSeekFallback(t, "", map[string]string{
			"GATEWAY_ZHIPU_DEEPSEEK_FALLBACK_ENABLED":         "true",
			"GATEWAY_ZHIPU_DEEPSEEK_FALLBACK_SOURCE_GROUP_ID": "3",
			"GATEWAY_ZHIPU_DEEPSEEK_FALLBACK_SOURCE_KEY_ID":   "42",
			"GATEWAY_ZHIPU_DEEPSEEK_FALLBACK_TARGET_GROUP_ID": "7",
			"GATEWAY_ZHIPU_DEEPSEEK_FALLBACK_TARGET_MODEL":    "deepseek-reasoner",
		})

		require.NoError(t, err)
		require.Equal(t, validZhipuDeepSeekFallback, cfg.Gateway.ZhipuDeepSeekFallback)
	})
}

func TestValidateZhipuDeepSeekFallback(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*GatewayZhipuDeepSeekFallbackConfig)
		wantErr string
	}{
		{name: "valid", mutate: func(*GatewayZhipuDeepSeekFallbackConfig) {}},
		{
			name: "disabled ignores the remaining fields",
			mutate: func(c *GatewayZhipuDeepSeekFallbackConfig) {
				*c = GatewayZhipuDeepSeekFallbackConfig{SourceGroupID: -1, TargetModel: "glm-4.6"}
			},
		},
		{
			name:    "source group id zero",
			mutate:  func(c *GatewayZhipuDeepSeekFallbackConfig) { c.SourceGroupID = 0 },
			wantErr: "gateway.zhipu_deepseek_fallback.source_group_id must be positive",
		},
		{
			name:    "source group id negative",
			mutate:  func(c *GatewayZhipuDeepSeekFallbackConfig) { c.SourceGroupID = -3 },
			wantErr: "gateway.zhipu_deepseek_fallback.source_group_id must be positive",
		},
		{
			name:    "target group id zero",
			mutate:  func(c *GatewayZhipuDeepSeekFallbackConfig) { c.TargetGroupID = 0 },
			wantErr: "gateway.zhipu_deepseek_fallback.target_group_id must be positive",
		},
		{
			name:    "target group id negative",
			mutate:  func(c *GatewayZhipuDeepSeekFallbackConfig) { c.TargetGroupID = -7 },
			wantErr: "gateway.zhipu_deepseek_fallback.target_group_id must be positive",
		},
		{
			name:    "same source and target group",
			mutate:  func(c *GatewayZhipuDeepSeekFallbackConfig) { c.TargetGroupID = c.SourceGroupID },
			wantErr: "gateway.zhipu_deepseek_fallback.source_group_id and target_group_id must differ",
		},
		{
			name:    "source key id zero means the whole group",
			mutate:  func(c *GatewayZhipuDeepSeekFallbackConfig) { c.SourceKeyID = 0 },
			wantErr: "",
		},
		{
			name:    "source key id negative",
			mutate:  func(c *GatewayZhipuDeepSeekFallbackConfig) { c.SourceKeyID = -42 },
			wantErr: "gateway.zhipu_deepseek_fallback.source_key_id must be zero or positive",
		},
		{
			name:    "target model empty",
			mutate:  func(c *GatewayZhipuDeepSeekFallbackConfig) { c.TargetModel = "" },
			wantErr: "gateway.zhipu_deepseek_fallback.target_model must be",
		},
		{
			name:    "target model is only the prefix",
			mutate:  func(c *GatewayZhipuDeepSeekFallbackConfig) { c.TargetModel = "deepseek-" },
			wantErr: "gateway.zhipu_deepseek_fallback.target_model must be",
		},
		{
			name:    "target model leading space",
			mutate:  func(c *GatewayZhipuDeepSeekFallbackConfig) { c.TargetModel = " deepseek-reasoner" },
			wantErr: "gateway.zhipu_deepseek_fallback.target_model must be",
		},
		{
			name:    "target model trailing space",
			mutate:  func(c *GatewayZhipuDeepSeekFallbackConfig) { c.TargetModel = "deepseek-reasoner " },
			wantErr: "gateway.zhipu_deepseek_fallback.target_model must be",
		},
		{
			name:    "target model is not a deepseek model",
			mutate:  func(c *GatewayZhipuDeepSeekFallbackConfig) { c.TargetModel = "glm-4.6" },
			wantErr: "gateway.zhipu_deepseek_fallback.target_model must be",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := loadZhipuDeepSeekFallback(t, "", nil)
			require.NoError(t, err)
			cfg.Gateway.ZhipuDeepSeekFallback = validZhipuDeepSeekFallback
			tt.mutate(&cfg.Gateway.ZhipuDeepSeekFallback)

			err = cfg.Validate()

			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestLoadRejectsInvalidZhipuDeepSeekFallback(t *testing.T) {
	// An environment variable outranks the config file, and a non-canonical model
	// must fail startup instead of being trimmed silently.
	_, err := loadZhipuDeepSeekFallback(t, zhipuDeepSeekFallbackYAML, map[string]string{
		"GATEWAY_ZHIPU_DEEPSEEK_FALLBACK_TARGET_MODEL": " deepseek-reasoner ",
	})

	require.ErrorContains(t, err, "validate config error: gateway.zhipu_deepseek_fallback.target_model must be")
}
