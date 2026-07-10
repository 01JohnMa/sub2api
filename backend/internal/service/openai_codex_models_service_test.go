package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func newCodexModelsTestAccount() *Account {
	return &Account{
		ID:       1,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":       "test-access-token",
			"chatgpt_account_id": "acc-123",
		},
	}
}

func newCodexModelsAPIKeyTestAccount(baseURL string) *Account {
	return &Account{
		ID:       2,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "test-upstream-key",
			"base_url": baseURL,
			"model_mapping": map[string]any{
				"gpt-5.5":     "gpt-5.5",
				"gpt-5.6-sol": "gpt-5.6-sol",
			},
		},
	}
}

func newCodexModelsAPIKeyTestService() *OpenAIGatewayService {
	return &OpenAIGatewayService{cfg: &config.Config{
		Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{
				Enabled:           false,
				AllowInsecureHTTP: true,
			},
		},
	}}
}

func TestFetchCodexModelsManifestPassthrough(t *testing.T) {
	manifestBody := `{"models":[{"slug":"gpt-5.5","display_name":"GPT-5.5"}]}`

	var gotAuth, gotAccountID, gotOriginator, gotClientVersion string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccountID = r.Header.Get("chatgpt-account-id")
		gotOriginator = r.Header.Get("Originator")
		gotClientVersion = r.URL.Query().Get("client_version")
		w.Header().Set("ETag", `W/"abc123"`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(manifestBody))
	}))
	defer server.Close()

	original := chatgptCodexModelsURL
	chatgptCodexModelsURL = server.URL
	defer func() { chatgptCodexModelsURL = original }()

	s := &OpenAIGatewayService{}
	manifest, err := s.FetchCodexModelsManifest(context.Background(), newCodexModelsTestAccount(), "0.137.0", "")
	if err != nil {
		t.Fatalf("FetchCodexModelsManifest returned error: %v", err)
	}

	if string(manifest.Body) != manifestBody {
		t.Errorf("body not passed through verbatim: got %q", manifest.Body)
	}
	if manifest.ETag != `W/"abc123"` {
		t.Errorf("etag not passed through: got %q", manifest.ETag)
	}
	if gotAuth != "Bearer test-access-token" {
		t.Errorf("authorization header: got %q", gotAuth)
	}
	if gotAccountID != "acc-123" {
		t.Errorf("chatgpt-account-id header: got %q", gotAccountID)
	}
	if gotOriginator != "codex_cli_rs" {
		t.Errorf("originator header: got %q", gotOriginator)
	}
	if gotClientVersion != "0.137.0" {
		t.Errorf("client_version query: got %q", gotClientVersion)
	}
}

func TestFetchCodexModelsManifestDefaultClientVersion(t *testing.T) {
	var gotClientVersion string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotClientVersion = r.URL.Query().Get("client_version")
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	defer server.Close()

	original := chatgptCodexModelsURL
	chatgptCodexModelsURL = server.URL
	defer func() { chatgptCodexModelsURL = original }()

	s := &OpenAIGatewayService{}
	if _, err := s.FetchCodexModelsManifest(context.Background(), newCodexModelsTestAccount(), "", ""); err != nil {
		t.Fatalf("FetchCodexModelsManifest returned error: %v", err)
	}
	if gotClientVersion != openAICodexProbeVersion {
		t.Errorf("default client_version: got %q, want %q", gotClientVersion, openAICodexProbeVersion)
	}
}

func TestFetchCodexModelsManifestNotModified(t *testing.T) {
	var gotIfNoneMatch string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIfNoneMatch = r.Header.Get("If-None-Match")
		w.Header().Set("ETag", `W/"abc123"`)
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()

	original := chatgptCodexModelsURL
	chatgptCodexModelsURL = server.URL
	defer func() { chatgptCodexModelsURL = original }()

	s := &OpenAIGatewayService{}
	manifest, err := s.FetchCodexModelsManifest(context.Background(), newCodexModelsTestAccount(), "0.137.0", `W/"abc123"`)
	if err != nil {
		t.Fatalf("FetchCodexModelsManifest returned error: %v", err)
	}
	if !manifest.NotModified {
		t.Error("expected NotModified to be true")
	}
	if gotIfNoneMatch != `W/"abc123"` {
		t.Errorf("if-none-match header: got %q", gotIfNoneMatch)
	}
}

func TestFetchCodexModelsManifestUpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"detail":"boom"}`, http.StatusInternalServerError)
	}))
	defer server.Close()

	original := chatgptCodexModelsURL
	chatgptCodexModelsURL = server.URL
	defer func() { chatgptCodexModelsURL = original }()

	s := &OpenAIGatewayService{}
	if _, err := s.FetchCodexModelsManifest(context.Background(), newCodexModelsTestAccount(), "0.137.0", ""); err == nil {
		t.Fatal("expected error for upstream 500, got nil")
	}
}

func TestFetchCodexModelsManifestMissingToken(t *testing.T) {
	account := newCodexModelsTestAccount()
	delete(account.Credentials, "access_token")

	s := &OpenAIGatewayService{}
	if _, err := s.FetchCodexModelsManifest(context.Background(), account, "0.137.0", ""); err == nil {
		t.Fatal("expected error for missing access token, got nil")
	}
}

func TestFetchCodexModelsManifestAPIKeyUpstreamFallback(t *testing.T) {
	manifestBody := `{"models":[{"slug":"gpt-5.6-sol","display_name":"GPT-5.6 Sol"},{"slug":"gpt-5.5","display_name":"GPT-5.5"},{"slug":"gpt-5.6-terra","display_name":"GPT-5.6 Terra"}],"metadata":{"source":"cpa"}}`

	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("request path: got %q, want /v1/models", r.URL.Path)
		}
		if got := r.URL.Query().Get("client_version"); got != "0.144.0" {
			t.Errorf("client_version query: got %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-upstream-key" {
			t.Errorf("authorization header: got %q", got)
		}
		if got := r.Header.Get("Originator"); got != "codex_cli_rs" {
			t.Errorf("originator header: got %q", got)
		}
		if got := r.Header.Get("Version"); got != "0.144.0" {
			t.Errorf("version header: got %q", got)
		}
		if got := r.Header.Get("If-None-Match"); got != "" {
			t.Errorf("if-none-match must not be forwarded on filtered fallback: got %q", got)
		}
		if got := r.Header.Get(CodexModelsManifestHopHeader); got != "1" {
			t.Errorf("manifest hop header: got %q, want 1", got)
		}
		w.Header().Set("ETag", `W/"cpa-manifest"`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(manifestBody))
	}))
	defer server.Close()
	serverURL = server.URL

	baseURLs := []string{
		serverURL,
		serverURL + "/",
		serverURL + "/v1",
		serverURL + "/v1/",
		serverURL + "/v1/models",
	}
	for _, baseURL := range baseURLs {
		t.Run(strings.TrimPrefix(baseURL, serverURL), func(t *testing.T) {
			account := newCodexModelsAPIKeyTestAccount(baseURL)
			manifest, err := newCodexModelsAPIKeyTestService().FetchCodexModelsManifest(
				context.Background(), account, "0.144.0", `W/"client-cache"`,
			)
			if err != nil {
				t.Fatalf("FetchCodexModelsManifest returned error: %v", err)
			}
			if manifest.NotModified {
				t.Error("API Key fallback must not reuse upstream 304 state")
			}
			if manifest.ETag != "" {
				t.Errorf("API Key fallback must not reuse upstream ETag: got %q", manifest.ETag)
			}

			var body struct {
				Models   []map[string]any `json:"models"`
				Metadata map[string]any   `json:"metadata"`
			}
			if err := json.Unmarshal(manifest.Body, &body); err != nil {
				t.Fatalf("unmarshal filtered manifest: %v", err)
			}
			if len(body.Models) != 2 {
				t.Fatalf("filtered model count: got %d, want 2; body=%s", len(body.Models), manifest.Body)
			}
			if body.Models[0]["slug"] != "gpt-5.6-sol" || body.Models[1]["slug"] != "gpt-5.5" {
				t.Errorf("filtered models: got %v", body.Models)
			}
			if body.Metadata["source"] != "cpa" {
				t.Errorf("top-level metadata not preserved: got %v", body.Metadata)
			}
		})
	}
}

func TestFetchCodexModelsManifestAPIKeyRejectsInvalidManifest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	defer server.Close()

	account := newCodexModelsAPIKeyTestAccount(server.URL)
	_, err := newCodexModelsAPIKeyTestService().FetchCodexModelsManifest(context.Background(), account, "0.144.0", "")
	if err == nil {
		t.Fatal("expected invalid Codex manifest to be rejected")
	}
	if !strings.Contains(err.Error(), "invalid Codex models manifest") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFetchCodexModelsManifestAPIKeyErrorDoesNotLeakCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `upstream rejected test-upstream-key`, http.StatusUnauthorized)
	}))
	defer server.Close()

	account := newCodexModelsAPIKeyTestAccount(server.URL)
	_, err := newCodexModelsAPIKeyTestService().FetchCodexModelsManifest(context.Background(), account, "0.144.0", "")
	if err == nil {
		t.Fatal("expected upstream error")
	}
	if strings.Contains(err.Error(), "test-upstream-key") {
		t.Fatalf("error leaked API key: %v", err)
	}
}

func TestFetchCodexModelsManifestAPIKeyMissingCredential(t *testing.T) {
	account := newCodexModelsAPIKeyTestAccount("https://example.com")
	delete(account.Credentials, "api_key")

	_, err := newCodexModelsAPIKeyTestService().FetchCodexModelsManifest(context.Background(), account, "0.144.0", "")
	if err == nil {
		t.Fatal("expected missing API key error")
	}
}

func TestFetchCodexModelsManifestAPIKeyInvalidBaseURLDoesNotLeakConfig(t *testing.T) {
	const sensitiveBaseURL = "not-a-url-with-private-hostname.example"
	account := newCodexModelsAPIKeyTestAccount(sensitiveBaseURL)

	_, err := newCodexModelsAPIKeyTestService().FetchCodexModelsManifest(context.Background(), account, "0.144.0", "")
	if err == nil {
		t.Fatal("expected invalid base URL error")
	}
	if strings.Contains(err.Error(), sensitiveBaseURL) {
		t.Fatalf("error leaked configured base URL: %v", err)
	}
}
