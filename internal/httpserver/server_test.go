package httpserver_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/auth"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/config"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/httpserver"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/mockvchasno"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/session"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/vchasno"
)

const apiToken = "test-token"

func newStack(t *testing.T, preAuth bool) (*httptest.Server, string) {
	t.Helper()
	mock := httptest.NewServer(mockvchasno.New(mockvchasno.Options{Token: apiToken}))
	t.Cleanup(mock.Close)

	cfg := config.Load()
	cfg.JWTKeyDir = t.TempDir()
	cfg.DownloadDir = cfg.JWTKeyDir
	cfg.BaseURL = mock.URL
	cfg.MaxRPS = 100
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	connect := func(ctx context.Context, creds vchasno.Credentials) (*session.Session, error) {
		return session.Connect(ctx, cfg, creds, logger)
	}
	var pre *session.Session
	if preAuth {
		s, err := connect(context.Background(), vchasno.Credentials{BaseURL: mock.URL, Token: apiToken})
		if err != nil {
			t.Fatal(err)
		}
		pre = s
		cfg.Token = apiToken
	}
	key, err := auth.LoadOrGenerateKey(cfg.JWTKeyDir, logger)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(nil)
	srv.Start()
	cfg.IssuerURL = srv.URL
	oauth := auth.NewServer(cfg.IssuerURL, cfg.IssuerURL+"/mcp", auth.NewTokenManager(cfg.IssuerURL, key, 1), connect, logger)
	srv.Config.Handler = httpserver.New(cfg, logger, oauth, pre).Handler()
	t.Cleanup(srv.Close)
	return srv, mock.URL
}

func mcpInitialize(t *testing.T, srvURL string, headers map[string]string) (*http.Response, string) {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`
	req, _ := http.NewRequest("POST", srvURL+"/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, string(data)
}

func TestHealthAndRoot(t *testing.T) {
	srv, _ := newStack(t, true)
	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("/health: %d", resp.StatusCode)
	}

	root, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Body.Close()
	var info map[string]any
	if err := json.NewDecoder(root.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	if info["name"] != "vchasno-edo-mcp" {
		t.Errorf("name: %v", info["name"])
	}
	if info["mcp"] == nil || info["auth"] == nil {
		t.Errorf("the root document should describe the endpoint and the auth modes: %v", info)
	}
}

func TestReadyReportsAPIState(t *testing.T) {
	srv, _ := newStack(t, true)
	resp, err := http.Get(srv.URL + "/ready")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("/ready with an open API should be 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["api_open"] != true {
		t.Errorf("api_open: %v", body["api_open"])
	}
}

func TestPreAuthNeedsNoCredentials(t *testing.T) {
	srv, _ := newStack(t, true)
	resp, text := mcpInitialize(t, srv.URL, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("pre-auth mode should serve MCP without headers, got %d: %s", resp.StatusCode, text)
	}
	if !strings.Contains(text, "vchasno-edo-mcp") {
		t.Errorf("unexpected initialize answer: %s", text)
	}
}

func TestUnauthenticatedIsChallenged(t *testing.T) {
	srv, _ := newStack(t, false)
	resp, _ := mcpInitialize(t, srv.URL, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	challenge := resp.Header.Get("WWW-Authenticate")
	if !strings.Contains(challenge, "resource_metadata=") {
		t.Errorf("the 401 must point at the resource metadata, got %q", challenge)
	}
}

func TestHeaderModeAuthenticates(t *testing.T) {
	srv, mockURL := newStack(t, false)
	resp, text := mcpInitialize(t, srv.URL, map[string]string{"X-Vchasno-Token": apiToken, "X-Vchasno-URL": mockURL})
	if resp.StatusCode != 200 {
		t.Fatalf("header mode failed: %d %s", resp.StatusCode, text)
	}
}

func TestHeaderModeRejectsBadToken(t *testing.T) {
	srv, mockURL := newStack(t, false)
	resp, _ := mcpInitialize(t, srv.URL, map[string]string{"X-Vchasno-Token": "wrong", "X-Vchasno-URL": mockURL})
	if resp.StatusCode == 200 {
		t.Fatal("a wrong token was accepted")
	}
	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unexpected status %d", resp.StatusCode)
	}
}

func TestOAuthDiscovery(t *testing.T) {
	srv, _ := newStack(t, false)
	for _, path := range []string{"/.well-known/oauth-authorization-server", "/.well-known/oauth-protected-resource",
		"/.well-known/oauth-protected-resource/mcp", "/.well-known/openid-configuration"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		_ = resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("%s: %d", path, resp.StatusCode)
		}
		if len(body) == 0 {
			t.Errorf("%s: empty document", path)
		}
	}
}

// TestOAuthFullFlow walks the whole authorization code + PKCE exchange the way
// an MCP client does, and then uses the resulting token against /mcp.
func TestOAuthFullFlow(t *testing.T) {
	srv, mockURL := newStack(t, false)

	// 1. Dynamic client registration.
	regBody := `{"client_name":"test client","redirect_uris":["http://localhost:9999/callback"]}`
	resp, err := http.Post(srv.URL+"/register", "application/json", strings.NewReader(regBody))
	if err != nil {
		t.Fatal(err)
	}
	var reg map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&reg)
	_ = resp.Body.Close()
	if resp.StatusCode != 201 || reg["client_id"] == nil {
		t.Fatalf("registration failed: %d %v", resp.StatusCode, reg)
	}
	clientID := reg["client_id"].(string)

	// 2. PKCE pair.
	verifier := "test-verifier-0123456789-0123456789-0123456789"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	// 3. The authorization page renders the login form.
	authURL := srv.URL + "/authorize?response_type=code&client_id=" + clientID +
		"&redirect_uri=" + url.QueryEscape("http://localhost:9999/callback") +
		"&code_challenge=" + challenge + "&code_challenge_method=S256&state=xyz"
	page, err := http.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	html, _ := io.ReadAll(page.Body)
	_ = page.Body.Close()
	if page.StatusCode != 200 || !strings.Contains(string(html), `name="token"`) {
		t.Fatalf("the login page did not render: %d", page.StatusCode)
	}

	// 4. Submitting the form validates the credentials and redirects with a code.
	form := url.Values{"client_id": {clientID}, "redirect_uri": {"http://localhost:9999/callback"},
		"state": {"xyz"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"},
		"base_url": {mockURL}, "token": {apiToken}}
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	cb, err := noRedirect.PostForm(srv.URL+"/authorize/callback", form)
	if err != nil {
		t.Fatal(err)
	}
	_ = cb.Body.Close()
	if cb.StatusCode != http.StatusFound {
		t.Fatalf("expected a redirect, got %d", cb.StatusCode)
	}
	loc, err := url.Parse(cb.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	code := loc.Query().Get("code")
	if code == "" {
		t.Fatalf("no authorization code in %s", loc)
	}
	if loc.Query().Get("state") != "xyz" {
		t.Error("state was not echoed back")
	}

	// 5. Exchanging the code for a token, with the verifier.
	tokenForm := url.Values{"grant_type": {"authorization_code"}, "code": {code},
		"client_id": {clientID}, "redirect_uri": {"http://localhost:9999/callback"}, "code_verifier": {verifier}}
	tr, err := http.PostForm(srv.URL+"/token", tokenForm)
	if err != nil {
		t.Fatal(err)
	}
	var tok map[string]any
	_ = json.NewDecoder(tr.Body).Decode(&tok)
	_ = tr.Body.Close()
	if tr.StatusCode != 200 || tok["access_token"] == nil {
		t.Fatalf("token exchange failed: %d %v", tr.StatusCode, tok)
	}
	access := tok["access_token"].(string)
	if strings.Contains(access, apiToken) {
		t.Error("the access token carries the Vchasno token in clear text")
	}

	// 6. The token opens /mcp.
	resp2, text := mcpInitialize(t, srv.URL, map[string]string{"Authorization": "Bearer " + access})
	if resp2.StatusCode != 200 {
		t.Fatalf("the issued token was refused: %d %s", resp2.StatusCode, text)
	}

	// 7. A code is single use.
	again, err := http.PostForm(srv.URL+"/token", tokenForm)
	if err != nil {
		t.Fatal(err)
	}
	_ = again.Body.Close()
	if again.StatusCode == 200 {
		t.Error("an authorization code was accepted twice")
	}
}

func TestOAuthRejectsWrongVerifier(t *testing.T) {
	srv, mockURL := newStack(t, false)
	regBody := `{"client_name":"c","redirect_uris":["http://localhost:9999/cb"]}`
	resp, _ := http.Post(srv.URL+"/register", "application/json", strings.NewReader(regBody))
	var reg map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&reg)
	_ = resp.Body.Close()
	clientID := reg["client_id"].(string)

	sum := sha256.Sum256([]byte("the-real-verifier"))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	form := url.Values{"client_id": {clientID}, "redirect_uri": {"http://localhost:9999/cb"},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}, "base_url": {mockURL}, "token": {apiToken}}
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	cb, _ := noRedirect.PostForm(srv.URL+"/authorize/callback", form)
	_ = cb.Body.Close()
	loc, _ := url.Parse(cb.Header.Get("Location"))
	code := loc.Query().Get("code")

	bad := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID},
		"redirect_uri": {"http://localhost:9999/cb"}, "code_verifier": {"some-other-verifier"}}
	tr, _ := http.PostForm(srv.URL+"/token", bad)
	_ = tr.Body.Close()
	if tr.StatusCode == 200 {
		t.Error("PKCE verification was skipped")
	}
}

func TestOAuthRejectsUnknownClient(t *testing.T) {
	srv, _ := newStack(t, false)
	resp, err := http.Get(srv.URL + "/authorize?response_type=code&client_id=nope&redirect_uri=http://x/cb&code_challenge=abc")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("an unregistered client should be refused, got %d", resp.StatusCode)
	}
}

func TestLoginFailsOnBadToken(t *testing.T) {
	srv, mockURL := newStack(t, false)
	regBody := `{"client_name":"c","redirect_uris":["http://localhost:9999/cb"]}`
	resp, _ := http.Post(srv.URL+"/register", "application/json", strings.NewReader(regBody))
	var reg map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&reg)
	_ = resp.Body.Close()

	form := url.Values{"client_id": {reg["client_id"].(string)}, "redirect_uri": {"http://localhost:9999/cb"},
		"code_challenge": {"abc"}, "code_challenge_method": {"S256"}, "base_url": {mockURL}, "token": {"wrong-token"}}
	cb, err := http.PostForm(srv.URL+"/authorize/callback", form)
	if err != nil {
		t.Fatal(err)
	}
	html, _ := io.ReadAll(cb.Body)
	_ = cb.Body.Close()
	if !strings.Contains(string(html), "Не вдалося підключитися") {
		t.Errorf("a bad token should re-render the form with an error, got %d", cb.StatusCode)
	}
}

// ── the REST bridge ────────────────────────────────────────────

func TestRESTToolListAndCall(t *testing.T) {
	srv, _ := newStack(t, true)
	resp, err := http.Get(srv.URL + "/tools")
	if err != nil {
		t.Fatal(err)
	}
	var listed map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&listed)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("/tools: %d", resp.StatusCode)
	}
	tools, _ := listed["tools"].([]any)
	if len(tools) < 50 {
		t.Errorf("/tools listed only %d tools", len(tools))
	}

	call, err := http.Post(srv.URL+"/tools/list_documents", "application/json", strings.NewReader(`{"limit":3}`))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(call.Body)
	_ = call.Body.Close()
	if call.StatusCode != 200 {
		t.Fatalf("calling a tool over REST: %d %s", call.StatusCode, data)
	}
	if !strings.Contains(string(data), "documents") {
		t.Errorf("unexpected REST answer: %s", data)
	}
}

func TestRESTResourcesAndPrompts(t *testing.T) {
	srv, _ := newStack(t, true)
	resp, err := http.Get(srv.URL + "/resources")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(data), "vchasno://") {
		t.Errorf("/resources: %d %s", resp.StatusCode, data)
	}

	read, err := http.Get(srv.URL + "/resources/read?uri=" + url.QueryEscape("vchasno://guide/statuses"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(read.Body)
	_ = read.Body.Close()
	if read.StatusCode != 200 || !strings.Contains(string(body), "7008") {
		t.Errorf("/resources/read: %d %s", read.StatusCode, truncate(string(body)))
	}

	prompts, err := http.Get(srv.URL + "/prompts")
	if err != nil {
		t.Fatal(err)
	}
	pdata, _ := io.ReadAll(prompts.Body)
	_ = prompts.Body.Close()
	if prompts.StatusCode != 200 || !strings.Contains(string(pdata), "onboard_company") {
		t.Errorf("/prompts: %d %s", prompts.StatusCode, truncate(string(pdata)))
	}
}

func TestOpenAPIDescribesTools(t *testing.T) {
	srv, _ := newStack(t, true)
	resp, err := http.Get(srv.URL + "/mcp/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&spec)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("/mcp/openapi.json: %d", resp.StatusCode)
	}
	paths, _ := spec["paths"].(map[string]any)
	if len(paths) < 50 {
		t.Errorf("the OpenAPI document describes only %d paths", len(paths))
	}
	if _, ok := paths["/tools/list_documents"]; !ok {
		t.Error("list_documents is missing from the OpenAPI document")
	}
	info, _ := spec["info"].(map[string]any)
	if !strings.Contains(strings.ToLower(info["title"].(string)), "вчасно") {
		t.Errorf("unexpected OpenAPI title: %v", info["title"])
	}
}

func truncate(s string) string {
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
