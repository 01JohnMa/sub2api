package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
)

// chatgptCodexModelsURL is the ChatGPT Codex models manifest endpoint.
// Package-level variable so tests can point it at a stub server.
var chatgptCodexModelsURL = "https://chatgpt.com/backend-api/codex/models"

const (
	codexModelsManifestBodyLimit int64 = 8 << 20
	CodexModelsManifestHopHeader       = "X-Codex-Manifest-Hop"
)

// CodexModelsManifest carries the raw upstream manifest payload plus caching
// metadata so handlers can pass both through to the client untouched.
type CodexModelsManifest struct {
	Body        []byte
	ETag        string
	NotModified bool
}

// FetchCodexModelsManifest fetches the live Codex models manifest using the
// selected account's OAuth credentials or configured API-key upstream.
//
// OAuth responses are passed through verbatim. API-key upstream responses are
// validated and filtered against the account's model whitelist before they are
// returned to the client.
func (s *OpenAIGatewayService) FetchCodexModelsManifest(ctx context.Context, account *Account, clientVersion, ifNoneMatch string) (*CodexModelsManifest, error) {
	if account == nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "OPENAI_CODEX_MODELS_ACCOUNT_REQUIRED", "account is required")
	}
	credAccount, err := resolveCredentialAccount(ctx, s.accountRepo, account)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "OPENAI_CODEX_MODELS_CREDENTIALS_FAILED", "resolve credential account: %v", err)
	}
	clientVersion = strings.TrimSpace(clientVersion)
	if clientVersion == "" {
		clientVersion = openAICodexProbeVersion
	}
	accessToken := credAccount.GetOpenAIAccessToken()
	if accessToken == "" {
		if credAccount.IsOpenAIApiKey() {
			return s.fetchCodexModelsManifestFromAPIKeyUpstream(ctx, account, credAccount, clientVersion)
		}
		return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_CODEX_MODELS_TOKEN_MISSING", "account has no Codex backend access token")
	}

	requestURL := chatgptCodexModelsURL + "?client_version=" + url.QueryEscape(clientVersion)

	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "OPENAI_CODEX_MODELS_REQUEST_FAILED", "create codex models request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Originator", "codex_cli_rs")
	req.Header.Set("Version", clientVersion)
	req.Header.Set("User-Agent", codexCLIUserAgent)
	if ifNoneMatch = strings.TrimSpace(ifNoneMatch); ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	setOpenAIChatGPTAccountHeaders(req.Header, credAccount)

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	client, err := httpclient.GetClient(httpclient.Options{
		ProxyURL:              proxyURL,
		Timeout:               15 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
	})
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "OPENAI_CODEX_MODELS_PROXY_INVALID", "invalid proxy configuration: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_CODEX_MODELS_UPSTREAM_FAILED", "codex models manifest request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotModified {
		return &CodexModelsManifest{ETag: resp.Header.Get("ETag"), NotModified: true}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = resp.Status
		}
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_CODEX_MODELS_UPSTREAM_FAILED", "codex models manifest upstream error %d: %s", resp.StatusCode, message)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, codexModelsManifestBodyLimit))
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_CODEX_MODELS_UPSTREAM_FAILED", "read codex models manifest response: %v", err)
	}
	return &CodexModelsManifest{Body: body, ETag: resp.Header.Get("ETag")}, nil
}

func (s *OpenAIGatewayService) fetchCodexModelsManifestFromAPIKeyUpstream(ctx context.Context, account, credAccount *Account, clientVersion string) (*CodexModelsManifest, error) {
	apiKey := strings.TrimSpace(credAccount.GetOpenAIApiKey())
	if apiKey == "" {
		return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_CODEX_MODELS_API_KEY_MISSING", "account has no OpenAI API key")
	}
	if s.cfg == nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "OPENAI_CODEX_MODELS_CONFIG_REQUIRED", "gateway security configuration is required")
	}

	baseURL, err := s.validateUpstreamBaseURL(credAccount.GetOpenAIBaseURL())
	if err != nil {
		return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_CODEX_MODELS_BASE_URL_INVALID", "configured OpenAI upstream base URL is not allowed")
	}
	parsedURL, err := url.Parse(buildOpenAIModelsURL(baseURL))
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "OPENAI_CODEX_MODELS_REQUEST_FAILED", "create codex models request URL: %v", err)
	}
	query := parsedURL.Query()
	query.Set("client_version", clientVersion)
	parsedURL.RawQuery = query.Encode()

	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, parsedURL.String(), nil)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "OPENAI_CODEX_MODELS_REQUEST_FAILED", "create codex models request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Originator", "codex_cli_rs")
	req.Header.Set("Version", clientVersion)
	req.Header.Set("User-Agent", codexCLIUserAgent)
	account.ApplyHeaderOverrides(req.Header)
	req.Header.Set(CodexModelsManifestHopHeader, "1")

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	client, err := httpclient.GetClient(httpclient.Options{
		ProxyURL:              proxyURL,
		Timeout:               15 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		ValidateResolvedIP:    s.cfg.Security.URLAllowlist.Enabled,
		AllowPrivateHosts:     s.cfg.Security.URLAllowlist.AllowPrivateHosts,
	})
	if err != nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "OPENAI_CODEX_MODELS_PROXY_INVALID", "invalid proxy configuration")
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_CODEX_MODELS_UPSTREAM_FAILED", "codex models manifest request failed")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_CODEX_MODELS_UPSTREAM_FAILED", "codex models manifest API-key upstream error %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, codexModelsManifestBodyLimit+1))
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_CODEX_MODELS_UPSTREAM_FAILED", "read codex models manifest response: %v", err)
	}
	if int64(len(body)) > codexModelsManifestBodyLimit {
		return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_CODEX_MODELS_UPSTREAM_FAILED", "codex models manifest response exceeds size limit")
	}

	filteredBody, err := filterCodexModelsManifestForAccount(body, credAccount)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_CODEX_MODELS_INVALID_MANIFEST", "invalid Codex models manifest: %v", err)
	}
	return &CodexModelsManifest{Body: filteredBody}, nil
}

func filterCodexModelsManifestForAccount(body []byte, account *Account) ([]byte, error) {
	var document map[string]json.RawMessage
	if err := json.Unmarshal(body, &document); err != nil {
		return nil, err
	}
	modelsValue, ok := document["models"]
	if !ok {
		return nil, errors.New("missing models array")
	}
	trimmedModelsValue := bytes.TrimSpace(modelsValue)
	if len(trimmedModelsValue) == 0 || trimmedModelsValue[0] != '[' {
		return nil, errors.New("models must be an array")
	}

	var models []json.RawMessage
	if err := json.Unmarshal(modelsValue, &models); err != nil {
		return nil, err
	}
	shouldFilter := account != nil && len(account.GetModelMapping()) > 0
	filtered := make([]json.RawMessage, 0, len(models))
	for _, model := range models {
		var entry struct {
			Slug string `json:"slug"`
		}
		if err := json.Unmarshal(model, &entry); err != nil {
			return nil, err
		}
		entry.Slug = strings.TrimSpace(entry.Slug)
		if entry.Slug == "" {
			return nil, errors.New("model entry is missing slug")
		}
		if !shouldFilter || account.IsModelSupported(entry.Slug) {
			filtered = append(filtered, model)
		}
	}
	if !shouldFilter {
		return body, nil
	}

	filteredModels, err := json.Marshal(filtered)
	if err != nil {
		return nil, err
	}
	document["models"] = filteredModels
	return json.Marshal(document)
}
