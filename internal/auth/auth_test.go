package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func testKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestTokenRoundTrip(t *testing.T) {
	m := NewTokenManager("https://mcp.example", testKey(t), 30)
	token, err := m.CreateToken("https://edo.vchasno.ua", "vchasno-api-token")
	if err != nil {
		t.Fatal(err)
	}
	creds, err := m.Validate(token)
	if err != nil {
		t.Fatalf("a freshly issued token must validate: %v", err)
	}
	if creds.BaseURL != "https://edo.vchasno.ua" || creds.Token != "vchasno-api-token" {
		t.Errorf("credentials did not survive the round trip: %+v", creds)
	}
	if creds.Subject == "vchasno-api-token" {
		t.Error("the subject must be a fingerprint, not the token itself")
	}
}

func TestTokenHidesTheAPIToken(t *testing.T) {
	m := NewTokenManager("https://mcp.example", testKey(t), 30)
	token, err := m.CreateToken("https://edo.vchasno.ua", "SUPER-SECRET-VCHASNO-TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	// The client stores this string; it must not be readable from it.
	if strings.Contains(token, "SUPER-SECRET") {
		t.Fatal("the access token carries the API token in clear text")
	}
	decoded, _ := b64.DecodeString(strings.Split(token, ".")[0])
	if !strings.Contains(string(decoded), "RSA-OAEP-256") {
		t.Errorf("unexpected JWE header: %s", decoded)
	}
}

func TestTokenRejectsForeignIssuer(t *testing.T) {
	mine := NewTokenManager("https://mcp.example", testKey(t), 30)
	theirs := NewTokenManager("https://evil.example", testKey(t), 30)
	token, err := theirs.CreateToken("https://edo.vchasno.ua", "t")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mine.Validate(token); err == nil {
		t.Error("a token signed by another key must not validate")
	}
}

func TestTokenRejectsExpired(t *testing.T) {
	m := NewTokenManager("https://mcp.example", testKey(t), -1)
	// A negative expiry falls back to the default, so build the expired case
	// explicitly through the manager's own clock-independent path.
	m.expiry = -1
	token, err := m.CreateToken("https://edo.vchasno.ua", "t")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Validate(token); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Errorf("an expired token must be rejected, got %v", err)
	}
}

func TestTokenRejectsGarbage(t *testing.T) {
	m := NewTokenManager("https://mcp.example", testKey(t), 30)
	for _, bad := range []string{"", "not-a-token", "a.b.c", "a.b.c.d.e"} {
		if _, err := m.Validate(bad); err == nil {
			t.Errorf("%q should not validate", bad)
		}
	}
}

func TestFingerprintIsStableAndOpaque(t *testing.T) {
	a := Fingerprint("token-one")
	if a != Fingerprint("token-one") {
		t.Error("the fingerprint is not stable")
	}
	if a == Fingerprint("token-two") {
		t.Error("different tokens share a fingerprint")
	}
	if strings.Contains(a, "token") {
		t.Error("the fingerprint leaks the token")
	}
}

func TestPKCEVerification(t *testing.T) {
	// The challenge of "verifier" under S256, as an MCP client would compute it.
	const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	const challenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if !verifyPKCE(verifier, challenge, "S256") {
		t.Error("a correct verifier was rejected")
	}
	if verifyPKCE("wrong", challenge, "S256") {
		t.Error("a wrong verifier was accepted")
	}
	if verifyPKCE(verifier, challenge, "plain") {
		t.Error("only S256 may be accepted")
	}
}

func TestKeyPersistsAcrossRestarts(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	first, err := LoadOrGenerateKey(dir, logger)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrGenerateKey(dir, logger)
	if err != nil {
		t.Fatal(err)
	}
	if first.N.Cmp(second.N) != 0 {
		t.Error("the key was not reused, so tokens would not survive a restart")
	}
	// Tokens issued before a restart still validate after it.
	token, err := NewTokenManager("i", first, 30).CreateToken("https://edo.vchasno.ua", "t")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewTokenManager("i", second, 30).Validate(token); err != nil {
		t.Errorf("a token issued before the restart no longer validates: %v", err)
	}
}

func TestLoginPageRendersFields(t *testing.T) {
	html := LoginPage(LoginPageParams{ClientID: "cid", RedirectURI: "https://client/cb", State: "st",
		CodeChallenge: "ch", CodeChallengeMethod: "S256", BaseURL: "https://edo.vchasno.ua", Error: "боom"})
	for _, want := range []string{`name="client_id" value="cid"`, `name="redirect_uri" value="https://client/cb"`,
		`name="code_challenge" value="ch"`, `name="token"`, "https://edo.vchasno.ua", "боom"} {
		if !strings.Contains(html, want) {
			t.Errorf("the login page lacks %q", want)
		}
	}
	if strings.Contains(html, `name="password"`) {
		t.Error("this server authenticates with a token, not a password")
	}
}

func TestLoginPageEscapesInput(t *testing.T) {
	html := LoginPage(LoginPageParams{ClientID: `"><script>alert(1)</script>`})
	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Error("the login page does not escape its inputs")
	}
}
