package auth

import (
	"html/template"
	"strings"
)

var loginTmpl = template.Must(template.New("login").Parse(`<!DOCTYPE html>
<html lang="uk">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Вчасно.ЕДО MCP — Вхід</title>
<style>
  *{box-sizing:border-box;margin:0;padding:0}
  body{font-family:-apple-system,system-ui,sans-serif;background:#f3f5f8;display:flex;justify-content:center;align-items:center;min-height:100vh;color:#1a1f2b}
  .card{background:#fff;border-radius:12px;padding:2rem;width:100%;max-width:460px;box-shadow:0 4px 24px rgba(0,0,0,.08)}
  h1{font-size:1.4rem;margin-bottom:.3rem}
  .sub{color:#667;margin-bottom:1.4rem;font-size:.9rem}
  label{display:block;font-weight:500;margin-bottom:.25rem;font-size:.9rem}
  input[type=text],input[type=password],input[type=url]{width:100%;padding:.6rem .8rem;border:1px solid #d6dbe3;border-radius:6px;font-size:1rem;margin-bottom:1rem}
  input:focus{outline:none;border-color:#186b4f;box-shadow:0 0 0 3px rgba(24,107,79,.15)}
  button{width:100%;padding:.75rem;background:#186b4f;color:#fff;border:none;border-radius:6px;font-size:1rem;cursor:pointer;font-weight:500}
  button:hover{background:#12543e}
  .error{background:#fdecea;color:#a33b2c;padding:.75rem;border-radius:6px;margin-bottom:1rem;font-size:.9rem}
  .hint{color:#889;font-size:.8rem;margin-top:1rem}
  .hint a{color:#186b4f}
</style>
</head>
<body>
<div class="card">
  <h1>📄 Вчасно.ЕДО MCP</h1>
  <p class="sub">Введіть токен інтеграції користувача «Вчасно.ЕДО». Токен зв'язує одного співробітника з однією компанією.</p>
  {{if .Error}}<div class="error">{{.Error}}</div>{{end}}
  <form method="POST" action="/authorize/callback">
    <input type="hidden" name="client_id" value="{{.ClientID}}">
    <input type="hidden" name="redirect_uri" value="{{.RedirectURI}}">
    <input type="hidden" name="state" value="{{.State}}">
    <input type="hidden" name="code_challenge" value="{{.CodeChallenge}}">
    <input type="hidden" name="code_challenge_method" value="{{.CodeChallengeMethod}}">
    <label for="base_url">Адреса сервісу</label>
    <input type="url" id="base_url" name="base_url" required value="{{.BaseURL}}" placeholder="https://edo.vchasno.ua">
    <label for="token">Токен «Вчасно»</label>
    <input type="password" id="token" name="token" required autocomplete="off" placeholder="Authorization-токен із налаштувань компанії">
    <button type="submit">Підключити</button>
  </form>
  <p class="hint">Токен не зберігається на сервері у відкритому вигляді: він шифрується у токен доступу, який отримує лише ваш MCP-клієнт. Створити токен — у налаштуваннях співробітника в кабінеті «Вчасно».</p>
</div>
</body>
</html>
`))

// LoginPageParams fill the login form.
type LoginPageParams struct {
	ClientID, RedirectURI, State, CodeChallenge, CodeChallengeMethod string
	BaseURL                                                          string
	Error                                                            string
}

// LoginPage renders the credential form of the authorization endpoint.
func LoginPage(p LoginPageParams) string {
	var b strings.Builder
	if err := loginTmpl.Execute(&b, p); err != nil {
		return "<html><body><h1>Template error</h1><p>" + template.HTMLEscapeString(err.Error()) + "</p></body></html>"
	}
	return b.String()
}
