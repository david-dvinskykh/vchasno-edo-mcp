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
  input[type=text],input[type=password],input[type=url],select{width:100%;padding:.6rem .8rem;border:1px solid #d6dbe3;border-radius:6px;font-size:1rem;margin-bottom:1rem}
  input:focus{outline:none;border-color:#186b4f;box-shadow:0 0 0 3px rgba(24,107,79,.15)}
  button{width:100%;padding:.75rem;background:#186b4f;color:#fff;border:none;border-radius:6px;font-size:1rem;cursor:pointer;font-weight:500}
  button:hover{background:#12543e}
  select{background:#fff}
  .error{background:#fdecea;color:#a33b2c;padding:.75rem;border-radius:6px;margin-bottom:1rem;font-size:.9rem}
  .notice{background:#e8f3ee;color:#12543e;padding:.75rem;border-radius:6px;margin-bottom:1rem;font-size:.9rem}
  .hint{color:#889;font-size:.8rem;margin-top:1rem}
  .hint a{color:#186b4f}
</style>
</head>
<body>
<div class="card">
  <h1>📄 Вчасно.ЕДО MCP</h1>
  <p class="sub">Введіть ліцензійний ключ і токен інтеграції користувача «Вчасно.ЕДО». Адреса сервісу береться з ліцензії; токен зв'язує одного співробітника з однією компанією.</p>
  {{if .Error}}<div class="error">{{.Error}}</div>{{end}}
  {{if .Notice}}<div class="notice">{{.Notice}}</div>{{end}}
  <form method="POST" action="/authorize/callback">
    <input type="hidden" name="client_id" value="{{.ClientID}}">
    <input type="hidden" name="redirect_uri" value="{{.RedirectURI}}">
    <input type="hidden" name="state" value="{{.State}}">
    <input type="hidden" name="code_challenge" value="{{.CodeChallenge}}">
    <input type="hidden" name="code_challenge_method" value="{{.CodeChallengeMethod}}">
    <label for="license_key">Ліцензійний ключ</label>
    <input type="password" id="license_key" name="license_key" required autocomplete="off" spellcheck="false" value="{{.LicenseKey}}" placeholder="lic_…">
    {{if .Connections}}<label for="connection">Підключення</label>
    <select id="connection" name="connection" required>
      <option value="">— оберіть підключення —</option>
      {{range .Connections}}<option value="{{.ID}}"{{if .Selected}} selected{{end}}>{{.Name}}</option>
      {{end}}
    </select>{{end}}
    <label for="token">Токен «Вчасно»</label>
    <input type="password" id="token" name="token" required autocomplete="off" value="{{.Token}}" placeholder="Authorization-токен із налаштувань компанії">
    <button type="submit">Підключити</button>
  </form>
  <p class="hint">Ліцензію перевіряє сервер ліцензій раз на добу; токен «Вчасно» туди не передається. Ключ і токен не зберігаються на сервері у відкритому вигляді: вони шифруються у токен доступу, який отримує лише ваш MCP-клієнт. Створити токен — у налаштуваннях співробітника в кабінеті «Вчасно».</p>
</div>
</body>
</html>
`))

// LoginPageParams fill the login form. LicenseKey, Connection and Token are
// echoed back when the form is re-rendered, so that the user does not have to
// type them again after choosing a connection or fixing a mistake.
type LoginPageParams struct {
	ClientID, RedirectURI, State, CodeChallenge, CodeChallengeMethod string
	LicenseKey, Connection, Token                                    string
	// Connections, when set, renders the connection select (a key with
	// several connections).
	Connections []ConnectionOption
	Error       string
	Notice      string
}

// ConnectionOption is one entry of the connection select.
type ConnectionOption struct {
	ID, Name string
	Selected bool
}

// LoginPage renders the credential form of the authorization endpoint.
func LoginPage(p LoginPageParams) string {
	var b strings.Builder
	if err := loginTmpl.Execute(&b, p); err != nil {
		return "<html><body><h1>Template error</h1><p>" + template.HTMLEscapeString(err.Error()) + "</p></body></html>"
	}
	return b.String()
}

// licenseReasonUA explains a license refusal code in the page's language.
var licenseReasonUA = map[string]string{
	"LICENSE_SUSPENDED":    "ліцензію призупинено",
	"LICENSE_EXPIRED":      "строк дії ліцензії минув",
	"UNPAID":               "ліцензію не оплачено",
	"PENDING_PLAN":         "для ліцензії ще не налаштовано тариф",
	"PRODUCT_NOT_LICENSED": "ліцензія не поширюється на Вчасно.ЕДО MCP",
	"INSTANCE_LIMIT":       "вичерпано ліміт серверів для цієї ліцензії",
	"INSTANCE_BLOCKED":     "цей сервер заблоковано для ліцензії",
	"NO_CONNECTION":        "для ліцензії не налаштовано жодного підключення",
	"NOT_CONFIGURED":       "ліцензування не налаштоване на цьому сервері (MCP_LICENSE_SERVER_URL / MCP_LICENSE_PUBLIC_KEY)",
	"NO_KEY":               "ліцензійний ключ обов'язковий",
	"INVALID_KEY":          "ліцензійний ключ недійсний або відкликаний",
	"OFFLINE":              "не вдалося зв'язатися із сервером ліцензій",
	"CONNECTION":           "обраного підключення немає в ліцензії",
	"CHOOSE_CONNECTION":    "оберіть підключення",
}

// licenseRefusalUA is the page text for a license refusal: the reason in
// Ukrainian, the code, and the license server's own message when it sent one.
func licenseRefusalUA(code, message string) string {
	text, known := licenseReasonUA[code]
	if !known {
		text = "доступ заборонено"
	}
	out := "Ліцензія не дозволяє доступ: " + text
	if code != "" {
		out += " (" + code + ")"
	}
	if message != "" {
		out += ". " + message
	}
	return out
}
