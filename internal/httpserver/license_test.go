package httpserver_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/auth"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/config"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/filestore"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/httpserver"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/license"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/mockvchasno"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/session"
)

const (
	apiToken   = "test-token"
	licenseKey = "lic_test-key-0123456789"
)

// ── a tiny license server ──────────────────────────────────────

// fakeLicense speaks protocol v2 with an Ed25519 key generated per test. Its
// state may be changed between requests: refuse the key, move or remove a
// connection, or go down.
type fakeLicense struct {
	priv   ed25519.PrivateKey
	pubB64 string

	mu     sync.Mutex
	now    time.Time
	down   bool
	keys   map[string]*fakeKey
	checks int
}

type fakeKey struct {
	access   bool
	reason   string
	message  string
	warnings []string
	conns    []license.Connection
}

func newFakeLicense(t *testing.T) *fakeLicense {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return &fakeLicense{priv: priv, pubB64: base64.StdEncoding.EncodeToString(der), now: time.Now(), keys: map[string]*fakeKey{}}
}

func (f *fakeLicense) clock() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeLicense) advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.mu.Unlock()
}

func (f *fakeLicense) set(key string, k *fakeKey) {
	f.mu.Lock()
	f.keys[key] = k
	f.mu.Unlock()
}

func (f *fakeLicense) setDown(down bool) {
	f.mu.Lock()
	f.down = down
	f.mu.Unlock()
}

func (f *fakeLicense) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/api/v2/license/check" {
		http.NotFound(w, r)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks++
	if f.down {
		http.Error(w, "down", http.StatusServiceUnavailable)
		return
	}
	k := f.keys[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	if k == nil {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"INVALID_LICENSE_KEY","message":"unknown key"}`)
		return
	}
	var req struct {
		Product    string `json:"product"`
		InstanceID string `json:"instanceId"`
		Nonce      string `json:"nonce"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var reason, message any
	if k.reason != "" {
		reason = k.reason
	}
	if k.message != "" {
		message = k.message
	}
	conns := k.conns
	if !k.access || conns == nil {
		conns = []license.Connection{}
	}
	payload, _ := json.Marshal(map[string]any{
		"protocolVersion": 2, "licenseId": "license-1", "product": req.Product, "instanceId": req.InstanceID,
		"nonce": req.Nonce, "issuedAt": f.now,
		// checkAfter = issuedAt: every request re-checks, so state changes in a
		// test take effect immediately.
		"checkAfter": f.now, "validUntil": f.now.Add(25 * time.Hour),
		"access": k.access, "reason": reason, "warnings": k.warnings,
		"licenseType": "subscription", "licenseStatus": "ACTIVE", "customerName": "Тест",
		"planName": "MCP Standard", "paidUntil": "2026-12-31", "expiresAt": nil, "message": message,
		"connections": conns,
	})
	sig := ed25519.Sign(f.priv, payload)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"payload": base64.StdEncoding.EncodeToString(payload),
		"signature": base64.StdEncoding.EncodeToString(sig), "keyId": "k1"})
}

// ── the stack under test ───────────────────────────────────────

type stackOptions struct {
	preAuth bool
	// noLicense leaves MCP_LICENSE_SERVER_URL / MCP_LICENSE_PUBLIC_KEY unset.
	noLicense bool
	// conns are the connections of licenseKey; default: one, pointing at the mock.
	conns func(mockURL string) []license.Connection
}

type stack struct {
	srv     *httptest.Server
	mockURL string
	files   *filestore.Store
	lic     *fakeLicense
}

func buildStack(t *testing.T, opt stackOptions) *stack {
	t.Helper()
	mock := httptest.NewServer(mockvchasno.New(mockvchasno.Options{Token: apiToken}))
	t.Cleanup(mock.Close)

	fake := newFakeLicense(t)
	conns := []license.Connection{{ID: "conn-main", Name: "Основна", Params: map[string]string{"url": mock.URL}}}
	if opt.conns != nil {
		conns = opt.conns(mock.URL)
	}
	fake.set(licenseKey, &fakeKey{access: true, conns: conns})
	licSrv := httptest.NewServer(fake)
	t.Cleanup(licSrv.Close)

	cfg := config.Load()
	cfg.JWTKeyDir = t.TempDir()
	cfg.DownloadDir = cfg.JWTKeyDir
	cfg.MaxRPS = 100
	cfg.Token, cfg.LicenseKey, cfg.LicenseConnection = "", "", ""
	cfg.LicenseServerURL, cfg.LicensePublicKey = "", ""
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	var lic *session.Licensing
	if opt.noLicense {
		lic = session.NewLicensing(cfg, "test", logger)
	} else {
		cfg.LicenseServerURL, cfg.LicensePublicKey = licSrv.URL, fake.pubB64
		m, err := license.NewManager(license.Config{ServerURL: licSrv.URL, PublicKey: fake.pubB64, Product: config.LicenseProduct,
			AppVersion: "test", StateDir: cfg.JWTKeyDir, Now: fake.clock})
		if err != nil {
			t.Fatal(err)
		}
		lic = session.NewLicensingFromManager(m, logger)
	}
	connect := func(ctx context.Context, login session.Login) (*session.Session, error) {
		return session.Open(ctx, cfg, lic, login, logger)
	}
	var pre *session.Session
	if opt.preAuth {
		cfg.Token, cfg.LicenseKey = apiToken, licenseKey
		if !opt.noLicense {
			s, err := connect(context.Background(), session.Login{LicenseKey: licenseKey, Token: apiToken})
			if err != nil {
				t.Fatal(err)
			}
			pre = s
		}
	}
	key, err := auth.LoadOrGenerateKey(cfg.JWTKeyDir, logger)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(nil)
	srv.Start()
	cfg.IssuerURL = srv.URL
	oauth := auth.NewServer(cfg.IssuerURL, cfg.IssuerURL+"/mcp", auth.NewTokenManager(cfg.IssuerURL, key, 1), connect, logger)
	store, err := filestore.New(t.TempDir(), cfg.FileTTL, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	srv.Config.Handler = httpserver.New(cfg, logger, oauth, lic, pre, store).Handler()
	t.Cleanup(srv.Close)
	return &stack{srv: srv, mockURL: mock.URL, files: store, lic: fake}
}

// restCall calls a tool over the REST bridge and decodes the JSON answer.
func restCall(t *testing.T, srvURL, tool, args string, headers map[string]string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srvURL+"/tools/"+tool, strings.NewReader(args))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func registerClient(t *testing.T, srvURL, redirect string) string {
	t.Helper()
	resp, err := http.Post(srvURL+"/register", "application/json", strings.NewReader(`{"client_name":"c","redirect_uris":["`+redirect+`"]}`))
	if err != nil {
		t.Fatal(err)
	}
	var reg map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&reg)
	_ = resp.Body.Close()
	return reg["client_id"].(string)
}

func postLogin(t *testing.T, srvURL string, form url.Values) (*http.Response, string) {
	t.Helper()
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := noRedirect.PostForm(srvURL+"/authorize/callback", form)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, string(body)
}

// ── the gate ───────────────────────────────────────────────────

func TestGateRefusesToolCallsWhenLicenseIsRefused(t *testing.T) {
	st := buildStack(t, stackOptions{preAuth: true})
	if code, out := restCall(t, st.srv.URL, "list_documents", `{"limit":1}`, nil); code != 200 {
		t.Fatalf("a valid license should let the call through: %d %v", code, out)
	}
	st.lic.set(licenseKey, &fakeKey{access: false, reason: "UNPAID", message: "pay the invoice"})
	code, out := restCall(t, st.srv.URL, "list_documents", `{"limit":1}`, nil)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("a refused license must turn the call into a tool error, got %d %v", code, out)
	}
	if out["code"] != "UNPAID" || !strings.Contains(out["error"].(string), "UNPAID") || out["message"] != "pay the invoice" {
		t.Errorf("the tool error should carry the reason and message: %v", out)
	}
	// self_check is let through and explains the refusal, without the key.
	code, out = restCall(t, st.srv.URL, "self_check", `{}`, nil)
	if code != 200 {
		t.Fatalf("self_check: %d %v", code, out)
	}
	lic, _ := out["license"].(map[string]any)
	if lic["ok"] != false || lic["reason"] != "UNPAID" || lic["license_id"] != "license-1" {
		t.Errorf("self_check should report the refused license: %v", lic)
	}
	data, _ := json.Marshal(out)
	if strings.Contains(string(data), licenseKey) {
		t.Error("self_check leaks the license key")
	}
	// Access restored: work resumes without a new login.
	st.lic.set(licenseKey, &fakeKey{access: true, conns: []license.Connection{{ID: "conn-main", Name: "Основна", Params: map[string]string{"url": st.mockURL}}}})
	if code, out := restCall(t, st.srv.URL, "list_documents", `{"limit":1}`, nil); code != 200 {
		t.Fatalf("after the license is restored the call should pass: %d %v", code, out)
	}
}

func TestGateStopsWhenLicenseServerIsUnreachablePastValidity(t *testing.T) {
	st := buildStack(t, stackOptions{preAuth: true})
	st.lic.setDown(true)
	// Inside validUntil the last confirmation is honoured.
	if code, out := restCall(t, st.srv.URL, "list_documents", `{"limit":1}`, nil); code != 200 {
		t.Fatalf("an unreachable server inside validUntil must not stop work: %d %v", code, out)
	}
	st.lic.advance(26 * time.Hour)
	code, out := restCall(t, st.srv.URL, "list_documents", `{"limit":1}`, nil)
	if code != http.StatusUnprocessableEntity || !strings.Contains(out["error"].(string), "unreachable") {
		t.Fatalf("past validUntil every call must fail with 'license server unreachable', got %d %v", code, out)
	}
	if out["code"] != session.LicenseOffline {
		t.Errorf("code: %v", out["code"])
	}
}

func TestGateFollowsMovedConnectionAndStopsOnRemovedOne(t *testing.T) {
	st := buildStack(t, stackOptions{preAuth: true})
	second := httptest.NewServer(mockvchasno.New(mockvchasno.Options{Token: apiToken}))
	t.Cleanup(second.Close)

	st.lic.set(licenseKey, &fakeKey{access: true, conns: []license.Connection{{ID: "conn-main", Name: "Основна", Params: map[string]string{"url": second.URL}}}})
	code, out := restCall(t, st.srv.URL, "self_check", `{}`, nil)
	if code != 200 || out["host"] != second.URL {
		t.Fatalf("the session should reconnect to the moved host: %d host=%v", code, out["host"])
	}

	st.lic.set(licenseKey, &fakeKey{access: true, conns: []license.Connection{{ID: "conn-other", Name: "Інша", Params: map[string]string{"url": second.URL}}}})
	code, out = restCall(t, st.srv.URL, "list_documents", `{"limit":1}`, nil)
	if code != http.StatusUnprocessableEntity || out["code"] != session.LicenseConnection {
		t.Fatalf("a connection gone from the license must stop the session: %d %v", code, out)
	}
}

func TestGateCoversResourcesAndDownloads(t *testing.T) {
	st := buildStack(t, stackOptions{preAuth: true})
	st.lic.set(licenseKey, &fakeKey{access: false, reason: "LICENSE_SUSPENDED"})
	resp, err := http.Get(st.srv.URL + "/resources/read?uri=" + url.QueryEscape("vchasno://company/overview"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode == 200 || !strings.Contains(string(body), "LICENSE_SUSPENDED") {
		t.Errorf("reading a resource must be refused with the reason: %d %s", resp.StatusCode, body)
	}
	code, out := restCall(t, st.srv.URL, "download_document", `{"id":"anything"}`, nil)
	if code != http.StatusUnprocessableEntity || out["code"] != "LICENSE_SUSPENDED" {
		t.Errorf("issuing a download link must be refused: %d %v", code, out)
	}
}

func TestLicenseNotConfiguredRefusesEverythingButHealth(t *testing.T) {
	st := buildStack(t, stackOptions{noLicense: true})
	resp, err := http.Get(st.srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("/health must stay up without a license: %d", resp.StatusCode)
	}
	ready, err := http.Get(st.srv.URL + "/ready")
	if err != nil {
		t.Fatal(err)
	}
	_ = ready.Body.Close()
	if ready.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("/ready without a license: %d", ready.StatusCode)
	}

	h, text := mcpInitialize(t, st.srv.URL, map[string]string{"X-License-Key": licenseKey, "X-Vchasno-Token": apiToken})
	if h.StatusCode != http.StatusForbidden || !strings.Contains(text, "MCP_LICENSE_SERVER_URL") {
		t.Errorf("header login without licensing: %d %s", h.StatusCode, text)
	}

	clientID := registerClient(t, st.srv.URL, "http://localhost:9999/cb")
	_, page := postLogin(t, st.srv.URL, url.Values{"client_id": {clientID}, "redirect_uri": {"http://localhost:9999/cb"},
		"code_challenge": {"abc"}, "license_key": {licenseKey}, "token": {apiToken}})
	if !strings.Contains(page, "MCP_LICENSE_SERVER_URL") || !strings.Contains(page, "Ліцензія не дозволяє доступ") {
		t.Errorf("the login page should explain that licensing is not configured: %s", truncate(page))
	}
}

func TestPreAuthWithoutLicenseStartsAndRefuses(t *testing.T) {
	st := buildStack(t, stackOptions{preAuth: true, noLicense: true})
	resp, text := mcpInitialize(t, st.srv.URL, nil)
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(text, "license is not configured") {
		t.Errorf("pre-auth without licensing: %d %s", resp.StatusCode, text)
	}
}

func TestHeaderModeNeedsLicenseKey(t *testing.T) {
	st := buildStack(t, stackOptions{})
	resp, _ := mcpInitialize(t, st.srv.URL, map[string]string{"X-Vchasno-Token": apiToken})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("a token without a license key must be refused, got %d", resp.StatusCode)
	}
	resp, _ = mcpInitialize(t, st.srv.URL, map[string]string{"X-License-Key": "lic_unknown", "X-Vchasno-Token": apiToken})
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("an unknown license key must be refused, got %d", resp.StatusCode)
	}
}

func severalConnections(mockURL string) []license.Connection {
	return []license.Connection{
		{ID: "c-kyiv", Name: "Київ", Params: map[string]string{"url": mockURL}},
		{ID: "c-kharkiv", Name: "Харків", Params: map[string]string{"url": mockURL}},
	}
}

func TestHeaderModePicksConnection(t *testing.T) {
	st := buildStack(t, stackOptions{conns: severalConnections})
	resp, text := mcpInitialize(t, st.srv.URL, map[string]string{"X-License-Key": licenseKey, "X-Vchasno-Token": apiToken})
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(text, "several connections") {
		t.Errorf("several connections without a choice: %d %s", resp.StatusCode, text)
	}
	resp, text = mcpInitialize(t, st.srv.URL, map[string]string{"X-License-Key": licenseKey, "X-Vchasno-Token": apiToken, "X-License-Connection": "c-kharkiv"})
	if resp.StatusCode != 200 {
		t.Errorf("choosing by id: %d %s", resp.StatusCode, text)
	}
}

func TestLoginPageOffersConnectionsAndKeepsValues(t *testing.T) {
	st := buildStack(t, stackOptions{conns: severalConnections})
	clientID := registerClient(t, st.srv.URL, "http://localhost:9999/cb")
	form := url.Values{"client_id": {clientID}, "redirect_uri": {"http://localhost:9999/cb"},
		"code_challenge": {"abc"}, "code_challenge_method": {"S256"}, "license_key": {licenseKey}, "token": {apiToken}}
	resp, page := postLogin(t, st.srv.URL, form)
	if resp.StatusCode != 200 {
		t.Fatalf("expected the form again, got %d", resp.StatusCode)
	}
	for _, want := range []string{`<select id="connection"`, "Київ", "Харків", `value="` + licenseKey + `"`, `value="` + apiToken + `"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the re-rendered page lacks %q", want)
		}
	}
	form.Set("connection", "c-kharkiv")
	resp, _ = postLogin(t, st.srv.URL, form)
	if resp.StatusCode != http.StatusFound {
		t.Errorf("with a connection chosen the login should complete, got %d", resp.StatusCode)
	}
}

func TestLoginShowsLicenseRefusal(t *testing.T) {
	st := buildStack(t, stackOptions{})
	st.lic.set(licenseKey, &fakeKey{access: false, reason: "INSTANCE_LIMIT", message: "звільніть сервер"})
	clientID := registerClient(t, st.srv.URL, "http://localhost:9999/cb")
	_, page := postLogin(t, st.srv.URL, url.Values{"client_id": {clientID}, "redirect_uri": {"http://localhost:9999/cb"},
		"code_challenge": {"abc"}, "license_key": {licenseKey}, "token": {apiToken}})
	for _, want := range []string{"INSTANCE_LIMIT", "вичерпано ліміт", "звільніть сервер"} {
		if !strings.Contains(page, want) {
			t.Errorf("the refusal page lacks %q", want)
		}
	}
}
