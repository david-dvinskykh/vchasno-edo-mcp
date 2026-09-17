package auth

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/vchasno"
)

// Routes mounts the OAuth discovery, registration, authorization and token
// endpoints on mux. defaultBaseURL pre-fills the login form.
func (s *Server) Routes(mux *http.ServeMux, defaultBaseURL string) {
	writeJSON := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	oauthError := func(w http.ResponseWriter, status int, code, desc string) {
		writeJSON(w, status, map[string]string{"error": code, "error_description": desc})
	}

	protected := map[string]any{"resource": s.ResourceURL, "authorization_servers": []string{s.Issuer},
		"bearer_methods_supported": []string{"header"}}
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, protected) })
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, protected) })

	authMeta := map[string]any{
		"issuer":                                s.Issuer,
		"authorization_endpoint":                s.Issuer + "/authorize",
		"token_endpoint":                        s.Issuer + "/token",
		"registration_endpoint":                 s.Issuer + "/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"code_challenge_methods_supported":      []string{"S256"},
	}
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, authMeta) })
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, authMeta) })

	// RFC 7591 dynamic client registration.
	mux.HandleFunc("POST /register", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ClientName   string   `json:"client_name"`
			RedirectURIs []string `json:"redirect_uris"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
			oauthError(w, 400, "invalid_request", "Invalid JSON")
			return
		}
		if len(body.RedirectURIs) == 0 {
			oauthError(w, 400, "invalid_client_metadata", "redirect_uris is required")
			return
		}
		c := s.RegisterClient(body.ClientName, body.RedirectURIs)
		writeJSON(w, 201, map[string]any{
			"client_id":                  c.ClientID,
			"client_name":                c.ClientName,
			"redirect_uris":              c.RedirectURIs,
			"token_endpoint_auth_method": "none",
			"grant_types":                []string{"authorization_code"},
			"response_types":             []string{"code"},
		})
	})

	// Authorization endpoint: render the login page.
	mux.HandleFunc("GET /authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("response_type") != "code" || q.Get("client_id") == "" || q.Get("redirect_uri") == "" || q.Get("code_challenge") == "" {
			http.Error(w, "Missing required parameters: response_type=code, client_id, redirect_uri, code_challenge", 400)
			return
		}
		if !s.IsValidClient(q.Get("client_id"), q.Get("redirect_uri")) {
			http.Error(w, "Invalid client_id or redirect_uri", 400)
			return
		}
		method := q.Get("code_challenge_method")
		if method == "" {
			method = "S256"
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(LoginPage(LoginPageParams{ClientID: q.Get("client_id"), RedirectURI: q.Get("redirect_uri"), State: q.Get("state"),
			CodeChallenge: q.Get("code_challenge"), CodeChallengeMethod: method, BaseURL: defaultBaseURL})))
	})

	// Login form submit: validate 1C credentials, issue code, redirect back.
	mux.HandleFunc("POST /authorize/callback", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", 400)
			return
		}
		p := LoginPageParams{ClientID: r.FormValue("client_id"), RedirectURI: r.FormValue("redirect_uri"), State: r.FormValue("state"),
			CodeChallenge: r.FormValue("code_challenge"), CodeChallengeMethod: r.FormValue("code_challenge_method"), BaseURL: strings.TrimSpace(r.FormValue("base_url"))}
		if p.CodeChallengeMethod == "" {
			p.CodeChallengeMethod = "S256"
		}
		apiToken := strings.TrimSpace(r.FormValue("token"))
		render := func(msg string) {
			p.Error = msg
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(LoginPage(p)))
		}
		if !s.IsValidClient(p.ClientID, p.RedirectURI) {
			render("Невідомий client_id або redirect_uri. Почніть вхід заново з вашого MCP-клієнта.")
			return
		}
		if p.BaseURL == "" || apiToken == "" {
			render("Адреса сервісу і токен обов'язкові")
			return
		}
		sess, err := s.Connect(r.Context(), vchasno.Credentials{BaseURL: p.BaseURL, Token: apiToken})
		if err != nil {
			s.Logger.Warn("login failed", "base", p.BaseURL, "err", err)
			render("Не вдалося підключитися: " + err.Error())
			return
		}
		code := s.CreateAuthCode(p.ClientID, p.RedirectURI, p.CodeChallenge, p.CodeChallengeMethod, sess)
		u, err := url.Parse(p.RedirectURI)
		if err != nil {
			render("Некоректний redirect_uri")
			return
		}
		qv := u.Query()
		qv.Set("code", code)
		if p.State != "" {
			qv.Set("state", p.State)
		}
		u.RawQuery = qv.Encode()
		http.Redirect(w, r, u.String(), http.StatusFound)
	})

	// Token endpoint.
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			oauthError(w, 400, "invalid_request", "bad form")
			return
		}
		if r.FormValue("grant_type") != "authorization_code" {
			oauthError(w, 400, "unsupported_grant_type", "Only authorization_code grant type is supported")
			return
		}
		token, err := s.ExchangeCode(r.FormValue("code"), r.FormValue("client_id"), r.FormValue("redirect_uri"), r.FormValue("code_verifier"))
		if err != nil {
			s.Logger.Warn("token exchange failed", "err", err)
			oauthError(w, 400, "invalid_grant", "Invalid or expired authorization code")
			return
		}
		writeJSON(w, 200, map[string]any{"access_token": token, "token_type": "Bearer", "expires_in": s.Tokens.ExpirySeconds()})
	})
}
