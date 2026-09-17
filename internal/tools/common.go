// Package tools registers every MCP tool, resource and prompt of the
// Vchasno.EDO server.
//
// The tools follow three rules that make the surface usable by a model:
// every numeric code the API returns is answered together with its meaning,
// every place Vchasno wants a GUID also accepts the human name behind it
// (employee email, label name, team name, document type title), and every
// call that changes something in Vchasno says so in its annotations and,
// when it cannot be undone, demands an explicit confirmation flag.
package tools

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/config"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/session"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/vchasno"
)

// Version is reported in the MCP implementation info.
const Version = "1.0.0"

// Deps is everything a tool needs.
type Deps struct {
	Sess   *session.Session
	Cfg    config.Config
	Logger *slog.Logger
}

// NewServer builds an MCP server bound to one Vchasno company session with
// every tool, resource and prompt registered.
func NewServer(sess *session.Session, cfg config.Config, logger *slog.Logger) *mcp.Server {
	if logger == nil {
		logger = slog.Default()
	}
	d := &Deps{Sess: sess, Cfg: cfg, Logger: logger}
	srv := mcp.NewServer(&mcp.Implementation{Name: "vchasno-edo-mcp", Version: Version}, &mcp.ServerOptions{
		Instructions: instructions(sess, cfg),
	})
	d.registerDocuments(srv)
	d.registerFiles(srv)
	d.registerWorkflow(srv)
	d.registerLifecycle(srv)
	d.registerCompany(srv)
	d.registerReports(srv)
	d.registerSelfCheck(srv)
	d.registerResources(srv)
	d.registerPrompts(srv)
	return srv
}

func instructions(sess *session.Session, cfg config.Config) string {
	var b strings.Builder
	b.WriteString("Vchasno.EDO (Вчасно.ЕДО) electronic document exchange. ")
	fmt.Fprintf(&b, "Host: %s. ", sess.Creds.BaseURL)
	if !sess.APIOpen {
		fmt.Fprintf(&b, "WARNING: the API is currently closed for this company (%s) — every data call will fail with access_denied until an 'Інтеграція' tariff is active. Start with self_check. ", sess.APINote)
	}
	if cfg.ReadOnly {
		b.WriteString("This server runs READ-ONLY: only tools that read data are registered. ")
	}
	b.WriteString("Core model: a document is outgoing (your company uploaded it) or incoming (a counterparty sent it); ")
	b.WriteString("list_documents reads the first, list_incoming_documents the second, and get_document reads one by id. ")
	b.WriteString("Status codes 7000…7011 are answered together with their meaning; you may also filter by name ('signed', 'rejected', 'waiting'). ")
	b.WriteString("Amounts are integers in kopiykas — every answer adds amount_uah next to them. ")
	b.WriteString("Dates are YYYY-MM-DD or YYYY-MM-DDTHH:MM. ")
	b.WriteString("Anywhere a role id, label id, team id or document type is wanted you may pass the employee email, label name, team name or type title instead. ")
	b.WriteString("Typical flows: upload_document → set_document_recipient → send_document → poll list_documents; ")
	b.WriteString("sync_changed_documents for incremental integration; download_document for the original, the signed ZIP or the .p7s container. ")
	b.WriteString("Signing needs a ready detached .p7s (add_signature) or a Vchasno.KEP cloud key (cloud_sign_* tools) — this server never holds private keys. ")
	b.WriteString("Irreversible tools (delete_document, delete_role, revoke_public_link, reset_user_tokens, activate_integration_trial …) require confirm=true. ")
	b.WriteString("Rate limit: 10 requests/second per company; the client paces itself, so prefer one paged call over many small ones. ")
	b.WriteString("Resources: vchasno://company/overview, vchasno://company/reference, vchasno://guide/workflow, vchasno://guide/statuses, vchasno://guide/categories, vchasno://guide/errors, vchasno://guide/integration, vchasno://document/{id}. ")
	b.WriteString("Prompts: onboard_company, outgoing_overview, incoming_inbox, send_document_flow, chase_unsigned, counterparty_dossier, monthly_report, sync_integration, troubleshoot_access.")
	return b.String()
}

// ── result helpers ─────────────────────────────────────────────

type toolError struct {
	Error   string `json:"error"`
	Hint    string `json:"hint,omitempty"`
	Code    string `json:"code,omitempty"`
	Status  int    `json:"status,omitempty"`
	Details any    `json:"details,omitempty"`
}

func fail(err error) (*mcp.CallToolResult, any, error) {
	te := toolError{Error: err.Error()}
	var ae *vchasno.Error
	if errors.As(err, &ae) {
		te.Hint, te.Code, te.Status, te.Details = ae.Hint(), ae.Code, ae.Status, ae.Details
	}
	data, _ := json.MarshalIndent(te, "", "  ")
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil, nil
}

func failf(format string, args ...any) (*mcp.CallToolResult, any, error) {
	return fail(fmt.Errorf(format, args...))
}

func ok(v any) (*mcp.CallToolResult, any, error) {
	data, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return fail(err)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil, nil
}

// done is the answer of a tool whose API call returns no body.
func done(action string, extra map[string]any) (*mcp.CallToolResult, any, error) {
	out := map[string]any{"ok": true, "action": action}
	for k, v := range extra {
		out[k] = v
	}
	return ok(out)
}

// ── annotations ────────────────────────────────────────────────

func readOnly(title string) *mcp.ToolAnnotations {
	f, t := false, true
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, DestructiveHint: &f, IdempotentHint: true, OpenWorldHint: &t}
}

func writes(title string) *mcp.ToolAnnotations {
	f, t := false, true
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: false, DestructiveHint: &f, IdempotentHint: false, OpenWorldHint: &t}
}

func destructive(title string) *mcp.ToolAnnotations {
	t := true
	f := false
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: false, DestructiveHint: &t, IdempotentHint: false, OpenWorldHint: &f}
}

// addRead registers a tool that only reads.
func addRead[In, Out any](srv *mcp.Server, name, title, desc string, h mcp.ToolHandlerFor[In, Out]) {
	mcp.AddTool(srv, &mcp.Tool{Name: name, Description: desc, Annotations: readOnly(title)}, h)
}

// addWrite registers a tool that changes data in Vchasno, unless the server
// runs read-only.
func (d *Deps) addWrite(srv *mcp.Server, name, title, desc string, ann *mcp.ToolAnnotations, register func()) {
	if d.Cfg.ReadOnly {
		d.Logger.Debug("read-only mode: tool not registered", "tool", name)
		return
	}
	_ = ann
	register()
}

// confirmed guards an irreversible action.
func confirmed(flag bool, what string) error {
	if flag {
		return nil
	}
	return fmt.Errorf("%s is irreversible; call again with confirm=true once the user has agreed to it", what)
}

// ── argument helpers ───────────────────────────────────────────

// boolPtr turns a tri-state string argument ("", "true", "false") into a
// pointer, so that a filter left out stays out of the query.
func boolPtr(s string) (*bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return nil, nil
	case "true", "1", "yes":
		t := true
		return &t, nil
	case "false", "0", "no":
		f := false
		return &f, nil
	}
	return nil, fmt.Errorf("expected true or false, got %q", s)
}

// amountPtr accepts either kopiykas (integer) or hryvnias with a decimal
// point, and always returns kopiykas, which is what the API wants.
func amountPtr(s string) (*int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	v, err := vchasno.ParseAmountUAH(s)
	if err != nil {
		return nil, fmt.Errorf("amount %q: %w (pass hryvnias like 1234.56)", s, err)
	}
	return &v, nil
}

func intPtr(v int) *int {
	if v == 0 {
		return nil
	}
	return &v
}

// splitList accepts a comma-separated string or an already-split list.
func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// mergeIDs joins an array argument with its comma-separated string form so
// that a model may use whichever it finds natural.
func mergeIDs(list []string, csv string) []string {
	out := make([]string, 0, len(list)+2)
	for _, v := range list {
		if t := strings.TrimSpace(v); t != "" {
			out = append(out, t)
		}
	}
	out = append(out, splitList(csv)...)
	return dedup(out)
}

func dedup(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := in[:0]
	for _, v := range in {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// ── page size ──────────────────────────────────────────────────

func (d *Deps) limit(requested int) int {
	if requested <= 0 {
		return d.Cfg.DefaultPage
	}
	if requested > d.Cfg.MaxPageSize {
		return d.Cfg.MaxPageSize
	}
	return requested
}

func (d *Deps) pages(requested int) int {
	if requested <= 0 {
		return 1
	}
	if requested > d.Cfg.MaxPages {
		return d.Cfg.MaxPages
	}
	return requested
}

// ── files in and out ───────────────────────────────────────────

// fileInput is the shared way a tool accepts a file: a path on the machine
// running this server, or inline base64. Exactly one of them must be set.
type fileInput struct {
	Path   string `json:"file_path,omitempty" jsonschema:"Path to the file on the machine running this MCP server"`
	Base64 string `json:"file_base64,omitempty" jsonschema:"File content as base64, an alternative to file_path"`
	Name   string `json:"file_name,omitempty" jsonschema:"File name to send to Vchasno; required with file_base64, defaults to the base name of file_path"`
}

func (d *Deps) readFile(in fileInput) (name string, content []byte, err error) {
	switch {
	case strings.TrimSpace(in.Base64) != "":
		content, err = base64.StdEncoding.DecodeString(strings.TrimSpace(in.Base64))
		if err != nil {
			return "", nil, fmt.Errorf("file_base64 is not valid base64: %w", err)
		}
		name = strings.TrimSpace(in.Name)
		if name == "" {
			return "", nil, errors.New("file_name is required together with file_base64")
		}
	case strings.TrimSpace(in.Path) != "":
		if !d.Cfg.AllowUploads {
			return "", nil, errors.New("reading local files is disabled on this server (MCP_ALLOW_LOCAL_FILES=false); pass file_base64 instead")
		}
		path := strings.TrimSpace(in.Path)
		content, err = os.ReadFile(path)
		if err != nil {
			return "", nil, fmt.Errorf("cannot read %s: %w", path, err)
		}
		name = strings.TrimSpace(in.Name)
		if name == "" {
			name = filepath.Base(path)
		}
	default:
		return "", nil, errors.New("pass either file_path or file_base64 + file_name")
	}
	if int64(len(content)) > d.Cfg.MaxUploadBytes {
		return "", nil, fmt.Errorf("file is %d bytes, over the %d byte limit of this server", len(content), d.Cfg.MaxUploadBytes)
	}
	return name, content, nil
}

// savedFile is what a download tool answers: where the bytes landed, how big
// they are and what they are, plus the content itself when it is small enough
// to be useful inline.
type savedFile struct {
	Path        string `json:"path"`
	Filename    string `json:"filename"`
	Size        int    `json:"size_bytes"`
	ContentType string `json:"content_type,omitempty"`
	SHA256      string `json:"sha256"`
	Base64      string `json:"base64,omitempty"`
	Text        string `json:"text,omitempty"`
	Note        string `json:"note,omitempty"`
}

// maxInline is the largest payload returned inside the tool answer itself.
const maxInline = 256 << 10

func (d *Deps) saveDownload(resp *vchasno.Response, fallbackName string, inline bool) (*savedFile, error) {
	name := resp.Filename
	if name == "" {
		name = fallbackName
	}
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "" || name == "." || name == "/" {
		name = "download.bin"
	}
	dir := d.Cfg.DownloadDir
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("cannot create download dir %s: %w", dir, err)
	}
	path := filepath.Join(dir, fmt.Sprintf("%d-%s", time.Now().UnixNano(), name))
	if err := os.WriteFile(path, resp.Body, 0o600); err != nil {
		return nil, fmt.Errorf("cannot write %s: %w", path, err)
	}
	sum := sha256.Sum256(resp.Body)
	out := &savedFile{Path: path, Filename: name, Size: len(resp.Body), ContentType: resp.ContentType, SHA256: hex.EncodeToString(sum[:])}
	switch {
	case isTextual(resp.ContentType) && len(resp.Body) <= maxInline:
		out.Text = string(resp.Body)
	case inline && len(resp.Body) <= maxInline:
		out.Base64 = base64.StdEncoding.EncodeToString(resp.Body)
	case inline:
		out.Note = fmt.Sprintf("file is %d bytes, too large to inline; read it from path", len(resp.Body))
	}
	return out, nil
}

func isTextual(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.Contains(ct, "json") || strings.Contains(ct, "text/") || strings.Contains(ct, "xml")
}

// ── context ────────────────────────────────────────────────────

func (d *Deps) api(ctx context.Context) *vchasno.Client { return d.Sess.Client }

// requireOpen refuses early, with a useful message, when the company has no
// API tariff — otherwise every tool would answer with the same raw 403.
func (d *Deps) requireOpen() error {
	if d.Sess.APIOpen {
		return nil
	}
	return &vchasno.Error{Status: 403, Code: "access_denied", Reason: d.Sess.APINote,
		Details: map[string]any{"fix": "activate the 'Інтеграція' tariff, or its one-time 30-day trial via activate_integration_trial"}}
}

// categoryHint names the document types closest to one that did not resolve,
// so a near-miss spelling is corrected in one step instead of a round trip
// through list_document_categories.
func (d *Deps) categoryHint(ctx context.Context, asked string) string {
	near := d.Sess.SuggestCategories(ctx, asked, 3)
	if len(near) == 0 {
		return ""
	}
	return " (did you mean: " + strings.Join(near, "; ") + ")"
}
