package tools_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/config"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/mockvchasno"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/session"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/tools"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/vchasno"
)

type env struct {
	t    *testing.T
	mock *mockvchasno.Server
	cs   *mcp.ClientSession
	dir  string
}

func setupWith(t *testing.T, opt mockvchasno.Options, tune func(*config.Config)) *env {
	t.Helper()
	if opt.Token == "" {
		opt.Token = "test-token"
	}
	mock := mockvchasno.New(opt)
	srv := httptest.NewServer(mock)
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	cfg := config.Load()
	cfg.DownloadDir = dir
	cfg.RequestTimeout = 10 * time.Second
	cfg.MaxRPS = 100
	cfg.DefaultPage = 25
	cfg.MaxPageSize = 100
	cfg.MaxPages = 20
	if tune != nil {
		tune(&cfg)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	sess, err := session.Connect(context.Background(), cfg, vchasno.Credentials{BaseURL: srv.URL, Token: opt.Token}, logger)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	server := tools.NewServer(sess, cfg, logger)
	ct, st := mcp.NewInMemoryTransports()
	if _, err := server.Connect(context.Background(), st, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil)
	cs, err := client.Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return &env{t: t, mock: mock, cs: cs, dir: dir}
}

func setup(t *testing.T) *env { return setupWith(t, mockvchasno.Options{}, nil) }

func (e *env) call(name string, args map[string]any) map[string]any {
	e.t.Helper()
	res, err := e.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		e.t.Fatalf("%s: %v", name, err)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if res.IsError {
		e.t.Fatalf("%s returned an error: %s", name, text)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		e.t.Fatalf("%s: answer is not a JSON object: %s", name, text)
	}
	return out
}

func (e *env) callErr(name string, args map[string]any) string {
	e.t.Helper()
	res, err := e.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		e.t.Fatalf("%s: %v", name, err)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if !res.IsError {
		e.t.Fatalf("%s should have failed, got: %s", name, text)
	}
	return text
}

func (e *env) toolNames() []string {
	e.t.Helper()
	var names []string
	for tl, err := range e.cs.Tools(context.Background(), nil) {
		if err != nil {
			e.t.Fatal(err)
		}
		names = append(names, tl.Name)
	}
	return names
}

func list(out map[string]any, key string) []any {
	v, _ := out[key].([]any)
	return v
}

func row(out map[string]any, key string, i int) map[string]any {
	items := list(out, key)
	if i >= len(items) {
		return nil
	}
	m, _ := items[i].(map[string]any)
	return m
}

// ── surface ────────────────────────────────────────────────────

func TestToolSurfaceIsComplete(t *testing.T) {
	e := setup(t)
	names := e.toolNames()
	have := map[string]bool{}
	for _, n := range names {
		have[n] = true
	}
	want := []string{
		// reading
		"list_documents", "list_incoming_documents", "get_document", "get_document_statuses", "sync_changed_documents",
		"download_document", "get_download_links", "list_signatures", "get_multilateral_route",
		"list_comments", "get_document_comments", "get_review_state", "list_delete_requests", "list_archive_folders",
		"list_structured_data", "download_structured_data", "get_public_link", "get_billing", "list_roles",
		"list_tags", "list_tag_roles", "list_fields", "get_document_fields", "list_document_categories",
		"list_groups", "list_scenarios", "list_document_templates", "get_document_template", "check_counterparty",
		"request_actions_report", "get_actions_report", "self_check",
		// writing
		"upload_document", "upload_document_version", "delete_document_version", "update_document_info",
		"set_document_recipient", "set_document_recipients", "set_document_access", "set_document_viewers",
		"set_document_signers", "set_multilateral_route", "add_signature", "send_document", "reject_document",
		"add_comment", "add_reviewer", "remove_reviewer", "create_sign_session",
		"cloud_sign_create_session", "cloud_sign_check_session", "cloud_sign_check_refresh_session",
		"cloud_sign_refresh_token", "cloud_sign_document",
		"mark_documents_processed", "delete_document", "create_delete_request", "cancel_delete_request",
		"accept_delete_request", "reject_delete_request", "lock_document_deletion",
		"archive_documents", "unarchive_documents", "upload_scan", "import_signed_document",
		"upload_archive_visualization", "start_structured_data",
		"create_public_link", "update_public_link", "revoke_public_link",
		"attach_child_document", "detach_child_document",
		"tag_documents", "tag_employees", "create_field", "update_field", "set_document_field",
		"create_document_category", "rename_document_category", "delete_document_category",
		"create_group", "rename_group", "delete_group", "group_members", "create_document_from_template",
		"invite_coworkers", "create_coworker", "update_role", "delete_role",
		"create_user_tokens", "reset_user_tokens", "activate_integration_trial",
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("tool %q is missing", w)
		}
	}
	if len(names) < len(want) {
		t.Errorf("registered %d tools, expected at least %d", len(names), len(want))
	}
}

func TestReadOnlyModeHidesWriteTools(t *testing.T) {
	e := setupWith(t, mockvchasno.Options{}, func(c *config.Config) { c.ReadOnly = true })
	for _, n := range e.toolNames() {
		switch n {
		case "upload_document", "delete_document", "send_document", "update_role", "add_signature":
			t.Errorf("read-only mode still registered the write tool %q", n)
		}
	}
	// Reading still works.
	out := e.call("list_documents", map[string]any{"limit": 3})
	if out["count"] == nil {
		t.Error("list_documents answered nothing in read-only mode")
	}
}

// ── reading documents ──────────────────────────────────────────

func TestListDocumentsEnrichesCodes(t *testing.T) {
	e := setup(t)
	out := e.call("list_documents", map[string]any{"limit": 10, "pages": 5})
	docs := list(out, "documents")
	if len(docs) != 7 {
		t.Fatalf("expected the 7 seeded documents, got %d", len(docs))
	}
	first := row(out, "documents", 0)
	if first["status_meaning"] == nil || first["status_meaning"] == "" {
		t.Error("status_meaning was not added")
	}
	if first["amount_uah"] == nil {
		t.Error("amount_uah was not added")
	}
	if first["category_title"] == nil {
		t.Error("category_title was not added")
	}
}

func TestListDocumentsPagination(t *testing.T) {
	e := setup(t)
	// The mock pages three at a time; one page must stop at three and hand
	// back a cursor, several pages must reach further.
	one := e.call("list_documents", map[string]any{"limit": 100, "pages": 1})
	if got := len(list(one, "documents")); got != 3 {
		t.Errorf("one page: got %d documents, want 3", got)
	}
	if one["next_cursor"] == nil {
		t.Error("one page: next_cursor is missing although more documents exist")
	}
	all := e.call("list_documents", map[string]any{"limit": 100, "pages": 10})
	if got := len(list(all, "documents")); got != 7 {
		t.Errorf("ten pages: got %d documents, want 7", got)
	}
	capped := e.call("list_documents", map[string]any{"limit": 2, "pages": 10})
	if got := len(list(capped, "documents")); got != 2 {
		t.Errorf("limit=2: got %d documents", got)
	}
}

func TestStatusFilterAcceptsNameAndCode(t *testing.T) {
	e := setup(t)
	byName := e.call("list_documents", map[string]any{"status": "signed", "limit": 50, "pages": 5})
	byCode := e.call("list_documents", map[string]any{"status": "7008", "limit": 50, "pages": 5})
	if len(list(byName, "documents")) == 0 {
		t.Fatal("status=signed found nothing")
	}
	if len(list(byName, "documents")) != len(list(byCode, "documents")) {
		t.Error("the name and the code must select the same documents")
	}
	msg := e.callErr("list_documents", map[string]any{"status": "nonsense"})
	if !strings.Contains(msg, "unknown status") {
		t.Errorf("an unknown status should be rejected with a useful message, got %s", msg)
	}
}

func TestCategoryFilterAcceptsTitle(t *testing.T) {
	e := setup(t)
	out := e.call("list_documents", map[string]any{"category": "Рахунок", "limit": 50, "pages": 5})
	if len(list(out, "documents")) == 0 {
		t.Fatal("filtering by the type title found nothing")
	}
	notes := list(out, "notes")
	if len(notes) == 0 || !strings.Contains(fmt.Sprint(notes[0]), "resolved to category") {
		t.Error("the answer should say which category the title resolved to")
	}
	for _, d := range list(out, "documents") {
		if got := d.(map[string]any)["category"]; fmt.Sprint(got) != "2" {
			t.Errorf("got a document of category %v", got)
		}
	}
	if msg := e.callErr("list_documents", map[string]any{"category": "Неіснуючий тип"}); !strings.Contains(msg, "unknown document type") {
		t.Errorf("unexpected message: %s", msg)
	}
}

func TestAmountFilterConvertsHryvniasToKopiykas(t *testing.T) {
	e := setup(t)
	// The seeded documents are 1000.00, 2000.00 … 7000.00 hryvnias.
	out := e.call("list_documents", map[string]any{"amount_min": "3000.00", "amount_max": "5000.00", "limit": 50, "pages": 5})
	docs := list(out, "documents")
	if len(docs) != 3 {
		t.Fatalf("expected 3 documents between 3000 and 5000 UAH, got %d", len(docs))
	}
	for _, d := range docs {
		amount := d.(map[string]any)["amount"].(float64)
		if amount < 300000 || amount > 500000 {
			t.Errorf("document outside the range: %v kopiykas", amount)
		}
	}
}

func TestIncomingDocumentsAreSeparate(t *testing.T) {
	e := setup(t)
	out := e.call("list_incoming_documents", map[string]any{"limit": 50, "pages": 5, "with": []any{"recipients"}})
	if out["direction"] != "incoming" {
		t.Errorf("direction: got %v", out["direction"])
	}
	docs := list(out, "documents")
	if len(docs) != 3 {
		t.Fatalf("expected 3 incoming documents, got %d", len(docs))
	}
	if docs[0].(map[string]any)["edrpou_owner"] == nil {
		t.Error("an incoming document must carry the sender's ЄДРПОУ")
	}
}

func TestHasChangedWarns(t *testing.T) {
	e := setup(t)
	e.mock.Touch("doc-001")
	out := e.call("list_documents", map[string]any{"has_changed": "true", "limit": 10})
	notes := fmt.Sprint(out["notes"])
	if !strings.Contains(notes, "sync_changed_documents") {
		t.Errorf("has_changed must warn that it is one-shot, notes: %v", out["notes"])
	}
	if len(list(out, "documents")) != 1 {
		t.Fatalf("expected the one touched document, got %d", len(list(out, "documents")))
	}
	// Reading cleared the flag, so a repeat answers nothing — which is exactly
	// the trap the note warns about.
	again := e.call("list_documents", map[string]any{"has_changed": "true", "limit": 10})
	if len(list(again, "documents")) != 0 {
		t.Error("the mock should have cleared has_changed after the first read")
	}
}

func TestSyncChangedDocumentsIsIdempotent(t *testing.T) {
	e := setup(t)
	from := "2026-07-01T00:00"
	to := "2026-12-31T00:00"
	first := e.call("sync_changed_documents", map[string]any{"changed_from": from, "changed_to": to, "limit": 50, "pages": 10})
	second := e.call("sync_changed_documents", map[string]any{"changed_from": from, "changed_to": to, "limit": 50, "pages": 10})
	if first["outgoing_count"] != second["outgoing_count"] {
		t.Errorf("the same window answered %v then %v documents", first["outgoing_count"], second["outgoing_count"])
	}
	if first["outgoing_count"].(float64) == 0 {
		t.Error("the sync window found nothing although documents exist in it")
	}
	if first["next_window_start"] != to {
		t.Errorf("next_window_start: got %v, want %v", first["next_window_start"], to)
	}
	both := e.call("sync_changed_documents", map[string]any{"changed_from": from, "changed_to": to, "direction": "both", "limit": 50, "pages": 10})
	if both["incoming_count"] == nil {
		t.Error("direction=both must also report incoming documents")
	}
	if notes := fmt.Sprint(both["notes"]); !strings.Contains(notes, "date_created") {
		t.Error("the answer must admit that incoming documents fall back to date_created")
	}
}

func TestGetDocumentFull(t *testing.T) {
	e := setup(t)
	e.call("add_comment", map[string]any{"id": "doc-002", "text": "Перевірено"})
	out := e.call("get_document", map[string]any{"id": "doc-002", "full": true})
	doc, _ := out["document"].(map[string]any)
	if doc == nil || doc["id"] != "doc-002" {
		t.Fatalf("document was not returned: %v", out)
	}
	if doc["status_meaning"] == nil {
		t.Error("status_meaning is missing")
	}
	if len(list(out, "comments")) == 0 {
		t.Error("full=true must gather the comments")
	}
	if !e.mock.Called("/signatures") {
		t.Error("full=true must ask for the signatures")
	}
}

func TestGetDocumentStatusesAddsMeaning(t *testing.T) {
	e := setup(t)
	out := e.call("get_document_statuses", map[string]any{"ids": []any{"doc-001", "doc-006"}})
	rows := list(out, "statuses")
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	for _, r := range rows {
		if r.(map[string]any)["meaning"] == nil {
			t.Error("a status row is missing its meaning")
		}
	}
	if msg := e.callErr("get_document_statuses", map[string]any{"ids": []any{}}); !strings.Contains(msg, "at least one") {
		t.Errorf("unexpected message: %s", msg)
	}
}

// ── the send flow ──────────────────────────────────────────────

func TestUploadSignSendFlow(t *testing.T) {
	e := setup(t)
	path := filepath.Join(e.dir, "akt.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.4 test"), 0o600); err != nil {
		t.Fatal(err)
	}

	up := e.call("upload_document", map[string]any{
		"file_path": path, "recipient_edrpou": "87654321", "recipient_emails": []any{"partner@example.ua"},
		"title": "Акт за серпень", "number": "A-100", "category": "Акт наданих послуг", "amount": "12500.50",
	})
	docs := list(up, "documents")
	if len(docs) != 1 {
		t.Fatalf("upload answered %d documents", len(docs))
	}
	doc := docs[0].(map[string]any)
	id := doc["id"].(string)
	if fmt.Sprint(doc["status"]) != "7001" {
		t.Errorf("a document with a counterparty email should be 7001, got %v", doc["status"])
	}
	if fmt.Sprint(doc["amount"]) != "1.25005e+06" && fmt.Sprint(doc["amount"]) != "1250050" {
		t.Errorf("12500.50 UAH should reach the API as 1250050 kopiykas, got %v", doc["amount"])
	}
	if up["next_step"] == nil {
		t.Error("the upload answer should say what to do next")
	}

	sig := base64.StdEncoding.EncodeToString([]byte("fake p7s"))
	e.call("add_signature", map[string]any{"id": id, "signature_base64": sig})
	if status, _, _ := e.mock.Doc(id); status != 7003 {
		t.Errorf("after signing the mock document is %d, want 7003", status)
	}
	e.call("send_document", map[string]any{"id": id})
	if status, _, _ := e.mock.Doc(id); status != 7004 {
		t.Errorf("after sending the document is %d, want 7004 (waiting for the counterparty)", status)
	}

	got := e.call("get_document", map[string]any{"id": id})
	if fmt.Sprint(got["document"].(map[string]any)["status"]) != "7004" {
		t.Error("get_document does not see the new status")
	}
}

func TestUploadFromBase64(t *testing.T) {
	e := setup(t)
	out := e.call("upload_document", map[string]any{
		"file_base64": base64.StdEncoding.EncodeToString([]byte("%PDF-1.4 inline")),
		"file_name":   "inline.pdf", "title": "Inline",
	})
	if len(list(out, "documents")) != 1 {
		t.Fatalf("upload from base64 failed: %v", out)
	}
	if msg := e.callErr("upload_document", map[string]any{"file_base64": "not base64!!"}); !strings.Contains(msg, "base64") {
		t.Errorf("unexpected message: %s", msg)
	}
	if msg := e.callErr("upload_document", map[string]any{}); !strings.Contains(msg, "file_path") {
		t.Errorf("unexpected message: %s", msg)
	}
}

func TestSetRecipientLiftsStatus(t *testing.T) {
	e := setup(t)
	// doc-001 is seeded in 7000 without a counterparty.
	e.call("set_document_recipient", map[string]any{"id": "doc-001", "edrpou": "87654321", "email": "new@partner.ua"})
	if status, _, _ := e.mock.Doc("doc-001"); status != 7001 {
		t.Errorf("after the counterparty was set the document is %d, want 7001", status)
	}
	if msg := e.callErr("set_document_recipient", map[string]any{"id": "doc-001", "edrpou": "87654321"}); !strings.Contains(msg, "email") {
		t.Errorf("unexpected message: %s", msg)
	}
}

func TestUpdateInfoRefusedAfter7003(t *testing.T) {
	e := setup(t)
	// doc-006 is seeded as 7008.
	msg := e.callErr("update_document_info", map[string]any{"id": "doc-006", "title": "Нова назва"})
	if !strings.Contains(msg, "7003") {
		t.Errorf("the API's reason should reach the caller, got %s", msg)
	}
	// Below 7003 it works, and the amount is converted.
	out := e.call("update_document_info", map[string]any{"id": "doc-002", "title": "Оновлено", "amount": "99.99"})
	doc := out["document"].(map[string]any)
	if doc["title"] != "Оновлено" {
		t.Errorf("title: got %v", doc["title"])
	}
	if fmt.Sprint(doc["amount"]) != "9999" {
		t.Errorf("99.99 UAH should be 9999 kopiykas, got %v", doc["amount"])
	}
	if msg := e.callErr("update_document_info", map[string]any{"id": "doc-002"}); !strings.Contains(msg, "nothing to change") {
		t.Errorf("unexpected message: %s", msg)
	}
}

func TestRejectRequiresConfirmAndReason(t *testing.T) {
	e := setup(t)
	if msg := e.callErr("reject_document", map[string]any{"id": "doc-002", "reason": "Помилка в сумі"}); !strings.Contains(msg, "confirm=true") {
		t.Errorf("rejection must be confirmed: %s", msg)
	}
	if msg := e.callErr("reject_document", map[string]any{"id": "doc-002", "confirm": true}); !strings.Contains(msg, "reason") {
		t.Errorf("unexpected message: %s", msg)
	}
	e.call("reject_document", map[string]any{"id": "doc-002", "reason": "Помилка в сумі", "confirm": true})
	if status, _, _ := e.mock.Doc("doc-002"); status != 7006 {
		t.Errorf("after the rejection the document is %d, want 7006", status)
	}
	out := e.call("get_document_comments", map[string]any{"id": "doc-002"})
	comments := list(out, "comments")
	if len(comments) == 0 || comments[0].(map[string]any)["type"] != "rejection" {
		t.Error("the rejection reason must show up as a comment of type rejection")
	}
}

func TestSignatureRefusedInWrongStatus(t *testing.T) {
	e := setup(t)
	sig := base64.StdEncoding.EncodeToString([]byte("fake"))
	// doc-001 is 7000: no counterparty, so nothing to sign yet.
	msg := e.callErr("add_signature", map[string]any{"id": "doc-001", "signature_base64": sig})
	if !strings.Contains(msg, "7000") {
		t.Errorf("the reason should name the status, got %s", msg)
	}
	if msg := e.callErr("add_signature", map[string]any{"id": "doc-003"}); !strings.Contains(msg, "signature_base64") {
		t.Errorf("unexpected message: %s", msg)
	}
}

func TestAddSignatureCanSendInOneStep(t *testing.T) {
	e := setup(t)
	sig := base64.StdEncoding.EncodeToString([]byte("fake"))
	out := e.call("add_signature", map[string]any{"id": "doc-002", "signature_base64": sig, "send": true})
	if out["sent"] != true {
		t.Errorf("send=true should also have sent the document: %v", out)
	}
	if status, _, _ := e.mock.Doc("doc-002"); status != 7004 {
		t.Errorf("document is %d, want 7004", status)
	}
}

// ── name resolution ────────────────────────────────────────────

func TestEmailsResolveToRoleIDs(t *testing.T) {
	e := setup(t)
	e.call("set_document_signers", map[string]any{"id": "doc-002", "people": []any{"buh@example.ua"}, "parallel": true})
	if !e.mock.Called("/signers") {
		t.Error("set_document_signers did not reach the API")
	}
	msg := e.callErr("set_document_signers", map[string]any{"id": "doc-002", "people": []any{"nobody@example.ua"}})
	if !strings.Contains(msg, "no active employee") {
		t.Errorf("an unknown email should be reported clearly, got %s", msg)
	}
}

func TestTeamNamesResolveToIDs(t *testing.T) {
	e := setup(t)
	e.call("set_document_viewers", map[string]any{"id": "doc-002", "strategy": "add", "teams": []any{"Бухгалтерія"}})
	if msg := e.callErr("set_document_viewers", map[string]any{"id": "doc-002", "strategy": "add", "teams": []any{"Немає такої"}}); !strings.Contains(msg, "no team named") {
		t.Errorf("unexpected message: %s", msg)
	}
	if msg := e.callErr("set_document_viewers", map[string]any{"id": "doc-002", "strategy": "sideways"}); !strings.Contains(msg, "add, remove or replace") {
		t.Errorf("unexpected message: %s", msg)
	}
}

func TestTagNamesResolveToIDs(t *testing.T) {
	e := setup(t)
	e.call("tag_documents", map[string]any{"document_ids": []any{"doc-001"}, "tags": []any{"Термінові"}, "action": "assign"})
	if !e.mock.Called("/tags/documents/connections") {
		t.Error("assigning a label by name did not reach the API")
	}
	out := e.call("list_documents", map[string]any{"ids": []any{"doc-001"}, "with": []any{"tags"}})
	doc := row(out, "documents", 0)
	if len(list(doc, "tags")) == 0 {
		t.Error("the document does not carry the label")
	}
	if msg := e.callErr("tag_documents", map[string]any{"document_ids": []any{"doc-001"}, "tags": []any{"Немає"}}); !strings.Contains(msg, "no label named") {
		t.Errorf("unexpected message: %s", msg)
	}
}

func TestCreateAndUseField(t *testing.T) {
	e := setup(t)
	created := e.call("create_field", map[string]any{"name": "Проєкт", "type": "text"})
	fieldID := created["field"].(map[string]any)["id"].(string)
	// The parameter may then be named instead of given by id.
	e.call("set_document_field", map[string]any{"document_id": "doc-001", "field": "Проєкт", "value": "Альфа"})
	out := e.call("get_document_fields", map[string]any{"id": "doc-001"})
	fields := list(out, "fields")
	if len(fields) != 1 {
		t.Fatalf("expected one parameter on the document, got %d", len(fields))
	}
	if fields[0].(map[string]any)["field_id"] != fieldID {
		t.Errorf("the name did not resolve to the id we created: %v", fields[0])
	}
	if msg := e.callErr("create_field", map[string]any{"name": "X", "type": "colour"}); !strings.Contains(msg, "text, number, date or enum") {
		t.Errorf("unexpected message: %s", msg)
	}
}

// ── confirmations ──────────────────────────────────────────────

func TestIrreversibleToolsDemandConfirmation(t *testing.T) {
	e := setup(t)
	cases := []struct {
		tool string
		args map[string]any
	}{
		{"delete_document", map[string]any{"id": "doc-001"}},
		{"delete_document_version", map[string]any{"document_id": "doc-001", "version_id": "v1"}},
		{"accept_delete_request", map[string]any{"id": "doc-001"}},
		{"revoke_public_link", map[string]any{"shared_id": "link-1"}},
		{"delete_group", map[string]any{"group": "Бухгалтерія"}},
		{"delete_document_category", map[string]any{"category_id": 37}},
		{"delete_role", map[string]any{"person": "buh@example.ua"}},
		{"reset_user_tokens", map[string]any{"emails": []any{"buh@example.ua"}}},
		{"activate_integration_trial", map[string]any{}},
		{"cloud_sign_document", map[string]any{"document_id": "doc-002", "client_id": "k", "password": "p", "session_token": "t"}},
	}
	for _, tc := range cases {
		msg := e.callErr(tc.tool, tc.args)
		if !strings.Contains(msg, "confirm=true") {
			t.Errorf("%s did not demand a confirmation: %s", tc.tool, msg)
		}
	}
	if e.mock.Called("DELETE /api/v2/documents/doc-001") {
		t.Error("an unconfirmed delete still reached the API")
	}
}

func TestDeleteDocumentWithConfirmation(t *testing.T) {
	e := setup(t)
	before := e.mock.DocCount()
	e.call("delete_document", map[string]any{"id": "doc-001", "confirm": true})
	if after := e.mock.DocCount(); after != before-1 {
		t.Errorf("document count went from %d to %d", before, after)
	}
	if _, _, found := e.mock.Doc("doc-001"); found {
		t.Error("the document is still there")
	}
}

func TestSignedDocumentNeedsDeleteRequest(t *testing.T) {
	e := setup(t)
	// doc-006 is 7008: the API refuses a direct delete.
	msg := e.callErr("delete_document", map[string]any{"id": "doc-006", "confirm": true})
	if !strings.Contains(strings.ToLower(msg), "delete request") {
		t.Errorf("the reason should point at the delete-request route, got %s", msg)
	}
	out := e.call("create_delete_request", map[string]any{"id": "doc-006", "message": "Помилковий документ"})
	if len(list(out, "delete_requests")) == 0 {
		t.Fatalf("no delete request came back: %v", out)
	}
	listed := e.call("list_delete_requests", map[string]any{"status": "new"})
	if fmt.Sprint(listed["count"]) != "1" {
		t.Errorf("expected one open request, got %v", listed["count"])
	}
	e.call("reject_delete_request", map[string]any{"id": "doc-006", "message": "Не погоджуємось"})
	rejected := e.call("list_delete_requests", map[string]any{"status": "rejected"})
	if fmt.Sprint(rejected["count"]) != "1" {
		t.Errorf("the request should now be rejected, got %v", rejected["count"])
	}
}

func TestUpdateRoleConfirmsWideningPermissions(t *testing.T) {
	e := setup(t)
	msg := e.callErr("update_role", map[string]any{"person": "buh@example.ua", "is_admin": "true"})
	if !strings.Contains(msg, "confirm=true") {
		t.Errorf("making somebody an administrator must be confirmed: %s", msg)
	}
	msg = e.callErr("update_role", map[string]any{"person": "buh@example.ua", "permissions": map[string]any{"can_delete_document": true}})
	if !strings.Contains(msg, "confirm=true") {
		t.Errorf("granting a permission must be confirmed: %s", msg)
	}
	// Narrowing a permission or setting a position does not.
	out := e.call("update_role", map[string]any{"person": "buh@example.ua", "position": "Головний бухгалтер"})
	if out["ok"] != true {
		t.Errorf("a harmless change should go through: %v", out)
	}
	out = e.call("update_role", map[string]any{"person": "buh@example.ua", "permissions": map[string]any{"can_delete_document": false}})
	if out["ok"] != true {
		t.Errorf("revoking a permission should not need a confirmation: %v", out)
	}
}

// ── downloads ──────────────────────────────────────────────────

func TestDownloadFormats(t *testing.T) {
	e := setup(t)
	for _, format := range []string{"original", "archive", "p7s", "asic", "pdf", "xml_pdf"} {
		out := e.call("download_document", map[string]any{"id": "doc-006", "format": format})
		file, _ := out["file"].(map[string]any)
		if file == nil {
			t.Fatalf("format %s: no file in the answer: %v", format, out)
		}
		path, _ := file["path"].(string)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("format %s: the file was not written: %v", format, err)
		}
		if len(data) == 0 {
			t.Errorf("format %s: the file is empty", format)
		}
		if file["sha256"] == nil || file["size_bytes"] == nil {
			t.Errorf("format %s: the answer lacks the checksum or the size", format)
		}
	}
	if msg := e.callErr("download_document", map[string]any{"id": "doc-006", "format": "docx"}); !strings.Contains(msg, "unknown format") {
		t.Errorf("unexpected message: %s", msg)
	}
	if !e.mock.Called("/xml-to-pdf") {
		t.Error("format=xml_pdf must first ask for the rendering")
	}
}

func TestDownloadInline(t *testing.T) {
	e := setup(t)
	out := e.call("download_document", map[string]any{"id": "doc-006", "format": "p7s", "inline": true})
	file := out["file"].(map[string]any)
	if file["base64"] == nil {
		t.Errorf("inline=true should return small binaries as base64: %v", file)
	}
}

func TestStructuredDataPollingAndExport(t *testing.T) {
	e := setup(t)
	e.call("start_structured_data", map[string]any{"ids": []any{"doc-006"}})
	pending := e.call("download_structured_data", map[string]any{"id": "doc-006"})
	if pending["ready"] != false {
		t.Errorf("recognition is still pending, the export must say so: %v", pending)
	}
	e.mock.Confirm("doc-006")
	ready := e.call("download_structured_data", map[string]any{"id": "doc-006"})
	if ready["ready"] != true {
		t.Fatalf("after confirmation the export should work: %v", ready)
	}
	file := ready["file"].(map[string]any)
	if !strings.Contains(fmt.Sprint(file["text"]), "items") {
		t.Errorf("the exported JSON should carry the recognised data: %v", file["text"])
	}
	if msg := e.callErr("download_structured_data", map[string]any{"id": "doc-006", "format": "csv"}); !strings.Contains(msg, "json, xml or xlsx") {
		t.Errorf("unexpected message: %s", msg)
	}
	if msg := e.callErr("start_structured_data", map[string]any{"ids": []any{}}); !strings.Contains(msg, "between 1 and 100") {
		t.Errorf("unexpected message: %s", msg)
	}
}

// ── public links, archive, reports ─────────────────────────────

func TestPublicLinkLifecycle(t *testing.T) {
	e := setup(t)
	if msg := e.callErr("create_public_link", map[string]any{"document_id": "doc-001", "access_days": 4}); !strings.Contains(msg, "1, 3, 5, 7, 14, 30") {
		t.Errorf("an invalid lifetime must be caught before the API call: %s", msg)
	}
	if msg := e.callErr("create_public_link", map[string]any{"document_id": "doc-001", "access_days": 7, "recipient_email": "a@b.ua"}); !strings.Contains(msg, "email_text") {
		t.Errorf("unexpected message: %s", msg)
	}
	created := e.call("create_public_link", map[string]any{"document_id": "doc-001", "access_days": 7, "type": "view", "single_use": true})
	link := created["link"].(map[string]any)
	sharedID := link["id"].(string)
	if link["link"] == nil {
		t.Error("the answer should carry the public URL")
	}
	got := e.call("get_public_link", map[string]any{"id": "doc-001"})
	if got["has_public_link"] != true {
		t.Errorf("the link was not found again: %v", got)
	}
	e.call("update_public_link", map[string]any{"shared_id": sharedID, "type": "sign", "access_days": 14})
	e.call("revoke_public_link", map[string]any{"shared_id": sharedID, "confirm": true})
	after := e.call("get_public_link", map[string]any{"id": "doc-001"})
	if after["link"].(map[string]any)["is_active"] != false {
		t.Error("the link should be inactive after being revoked")
	}
}

func TestPublicLinkAbsentIsNotAnError(t *testing.T) {
	e := setup(t)
	out := e.call("get_public_link", map[string]any{"id": "doc-003"})
	if out["has_public_link"] != false {
		t.Errorf("a document without a link should answer has_public_link=false, got %v", out)
	}
}

func TestArchiveRoundTrip(t *testing.T) {
	e := setup(t)
	e.call("archive_documents", map[string]any{"ids": []any{"doc-003"}, "directory_id": "11"})
	archived := e.call("list_documents", map[string]any{"is_archived": "true", "limit": 50, "pages": 5})
	if fmt.Sprint(archived["count"]) != "1" {
		t.Errorf("expected one archived document, got %v", archived["count"])
	}
	e.call("unarchive_documents", map[string]any{"ids": []any{"doc-003"}})
	after := e.call("list_documents", map[string]any{"is_archived": "true", "limit": 50, "pages": 5})
	if fmt.Sprint(after["count"]) != "0" {
		t.Errorf("the document should be back out of the archive, got %v", after["count"])
	}
	folders := e.call("list_archive_folders", map[string]any{})
	if len(list(folders, "folders")) == 0 {
		t.Error("no archive folders came back")
	}
}

func TestImportSignedDocument(t *testing.T) {
	e := setup(t)
	original := filepath.Join(e.dir, "orig.pdf")
	sig := filepath.Join(e.dir, "orig.p7s")
	_ = os.WriteFile(original, []byte("%PDF original"), 0o600)
	_ = os.WriteFile(sig, []byte("p7s bytes"), 0o600)

	out := e.call("import_signed_document", map[string]any{
		"original":   map[string]any{"file_path": original},
		"signatures": []any{map[string]any{"file_path": sig}},
		"title":      "Підписаний в іншій системі", "amount": "500.00", "category": "Рахунок",
	})
	imported := out["imported"].(map[string]any)
	if imported["document_id"] == nil {
		t.Fatalf("no document id came back: %v", out)
	}
	if fmt.Sprint(imported["signature_count"]) != "1" {
		t.Errorf("signature_count: got %v", imported["signature_count"])
	}
	// The internal format takes a container instead.
	container := filepath.Join(e.dir, "doc.p7s")
	_ = os.WriteFile(container, []byte("container bytes"), 0o600)
	e.call("import_signed_document", map[string]any{"container": map[string]any{"file_path": container}})

	if msg := e.callErr("import_signed_document", map[string]any{}); !strings.Contains(msg, "container") {
		t.Errorf("unexpected message: %s", msg)
	}
	if msg := e.callErr("import_signed_document", map[string]any{"original": map[string]any{"file_path": original}}); !strings.Contains(msg, "at least one detached") {
		t.Errorf("unexpected message: %s", msg)
	}
}

func TestActionsReportWaitsAndDownloads(t *testing.T) {
	e := setup(t)
	out := e.call("request_actions_report", map[string]any{"kind": "documents", "date_from": "2026-08-01", "date_to": "2026-08-20", "wait": true})
	file, _ := out["file"].(map[string]any)
	if file == nil {
		t.Fatalf("wait=true should have produced the file: %v", out)
	}
	if _, err := os.Stat(file["path"].(string)); err != nil {
		t.Errorf("the report file was not written: %v", err)
	}
	if msg := e.callErr("request_actions_report", map[string]any{"date_from": "2026-01-01", "date_to": "2026-06-01"}); !strings.Contains(msg, "30 days") {
		t.Errorf("an over-long period must be caught before the API call: %s", msg)
	}
	if msg := e.callErr("request_actions_report", map[string]any{"date_from": "2020-01-01", "date_to": "2020-01-20"}); !strings.Contains(msg, "year") {
		t.Errorf("a period older than a year must be caught: %s", msg)
	}
}

// ── company administration ─────────────────────────────────────

func TestGroupLifecycle(t *testing.T) {
	e := setup(t)
	created := e.call("create_group", map[string]any{"name": "Юристи"})
	id := created["team"].(map[string]any)["id"].(string)
	e.call("group_members", map[string]any{"group": "Юристи", "action": "add", "people": []any{"jurist@example.ua"}})
	members := e.call("group_members", map[string]any{"group": id, "action": "list"})
	rows := list(members, "members")
	if len(rows) != 1 {
		t.Fatalf("expected one member, got %d", len(rows))
	}
	if rows[0].(map[string]any)["role_id"] != "role-jurist" {
		t.Errorf("the email did not resolve to the right role: %v", rows[0])
	}
	memberID := rows[0].(map[string]any)["id"].(string)
	e.call("group_members", map[string]any{"group": id, "action": "remove", "people": []any{memberID}})
	after := e.call("group_members", map[string]any{"group": id, "action": "list"})
	if len(list(after, "members")) != 0 {
		t.Error("the member was not removed")
	}
	e.call("rename_group", map[string]any{"group": id, "name": "Юридичний відділ"})
	e.call("delete_group", map[string]any{"group": id, "confirm": true})
}

func TestCategoryLifecycle(t *testing.T) {
	e := setup(t)
	created := e.call("create_document_category", map[string]any{"title": "Службова записка"})
	id := int(created["category"].(map[string]any)["category_id"].(float64))
	listed := e.call("list_document_categories", map[string]any{})
	found := false
	for _, c := range list(listed, "categories") {
		if int(c.(map[string]any)["category_id"].(float64)) == id {
			found = true
		}
	}
	if !found {
		t.Error("the new type is not in the list")
	}
	e.call("rename_document_category", map[string]any{"category_id": id, "title": "Службова записка 2"})
	e.call("delete_document_category", map[string]any{"category_id": id, "confirm": true})
}

func TestReviewFlow(t *testing.T) {
	e := setup(t)
	if msg := e.callErr("add_reviewer", map[string]any{"id": "doc-002", "email": "buh@example.ua", "team": "Бухгалтерія"}); !strings.Contains(msg, "exactly one") {
		t.Errorf("unexpected message: %s", msg)
	}
	e.call("add_reviewer", map[string]any{"id": "doc-002", "email": "buh@example.ua"})
	state := e.call("get_review_state", map[string]any{"id": "doc-002"})
	if state["status"] == nil {
		t.Fatalf("no approval status came back: %v", state)
	}
	if len(list(state, "assigned")) != 1 {
		t.Errorf("expected one assigned approver: %v", state["assigned"])
	}
	e.call("remove_reviewer", map[string]any{"id": "doc-002", "email": "buh@example.ua"})
	after := e.call("get_review_state", map[string]any{"id": "doc-002"})
	if len(list(after, "assigned")) != 0 {
		t.Error("the approver was not removed")
	}
}

func TestCloudSigningFlow(t *testing.T) {
	e := setup(t)
	created := e.call("cloud_sign_create_session", map[string]any{"client_id": "key-1", "duration_seconds": 600})
	sid := created["session"].(map[string]any)["authSessionId"].(string)
	checked := e.call("cloud_sign_check_session", map[string]any{"auth_session_id": sid})
	if checked["status"] != "ready" || checked["token"] == nil {
		t.Fatalf("the session should be ready with a token: %v", checked)
	}
	if msg := e.callErr("cloud_sign_create_session", map[string]any{"client_id": "k", "duration_seconds": 10}); !strings.Contains(msg, "60") {
		t.Errorf("unexpected message: %s", msg)
	}
	if msg := e.callErr("cloud_sign_document", map[string]any{"document_id": "doc-002", "client_id": "k", "password": "p", "confirm": true}); !strings.Contains(msg, "exactly one") {
		t.Errorf("the tool must insist on one of the two token kinds: %s", msg)
	}
	out := e.call("cloud_sign_document", map[string]any{"document_id": "doc-002", "client_id": "key-1", "password": "p",
		"session_token": checked["token"], "confirm": true})
	if out["ok"] != true {
		t.Errorf("signing failed: %v", out)
	}
	if status, _, _ := e.mock.Doc("doc-002"); status != 7004 {
		t.Errorf("after the cloud signature the document is %d", status)
	}
}

func TestChildDocuments(t *testing.T) {
	e := setup(t)
	e.call("attach_child_document", map[string]any{"parent_id": "doc-001", "child_id": "doc-002"})
	out := e.call("get_document", map[string]any{"id": "doc-001", "with": []any{"connections"}})
	doc := out["document"].(map[string]any)
	children := list(doc, "children")
	if len(children) != 1 || children[0] != "doc-002" {
		t.Errorf("the child was not attached: %v", doc["children"])
	}
	e.call("detach_child_document", map[string]any{"parent_id": "doc-001", "child_id": "doc-002"})
	after := e.call("get_document", map[string]any{"id": "doc-001", "with": []any{"connections"}})
	if len(list(after["document"].(map[string]any), "children")) != 0 {
		t.Error("the child was not detached")
	}
}

func TestVersionsRoundTrip(t *testing.T) {
	e := setup(t)
	path := filepath.Join(e.dir, "v2.pdf")
	_ = os.WriteFile(path, []byte("%PDF v2"), 0o600)
	e.call("upload_document_version", map[string]any{"document_id": "doc-001", "file_path": path})
	out := e.call("get_document", map[string]any{"id": "doc-001", "with": []any{"versions"}})
	versions := list(out["document"].(map[string]any), "versions")
	if len(versions) != 1 {
		t.Fatalf("expected one version, got %d", len(versions))
	}
	vid := versions[0].(map[string]any)["id"].(string)
	e.call("delete_document_version", map[string]any{"document_id": "doc-001", "version_id": vid, "confirm": true})
	after := e.call("get_document", map[string]any{"id": "doc-001", "with": []any{"versions"}})
	if len(list(after["document"].(map[string]any), "versions")) != 0 {
		t.Error("the version was not deleted")
	}
}

func TestMarkProcessedAndInboxQuery(t *testing.T) {
	e := setup(t)
	inbox := e.call("list_incoming_documents", map[string]any{"processed": "false", "limit": 50, "pages": 5})
	before := len(list(inbox, "documents"))
	if before != 3 {
		t.Fatalf("expected 3 unprocessed incoming documents, got %d", before)
	}
	out := e.call("mark_documents_processed", map[string]any{"ids": []any{"in-001", "in-002"}})
	if fmt.Sprint(out["updated"]) != "2" {
		t.Errorf("updated: got %v", out["updated"])
	}
	after := e.call("list_incoming_documents", map[string]any{"processed": "false", "limit": 50, "pages": 5})
	if got := len(list(after, "documents")); got != 1 {
		t.Errorf("expected 1 document left unprocessed, got %d", got)
	}
	if msg := e.callErr("mark_documents_processed", map[string]any{"ids": []any{}}); !strings.Contains(msg, "between 1 and 500") {
		t.Errorf("unexpected message: %s", msg)
	}
}

func TestCreateDocumentFromTemplate(t *testing.T) {
	e := setup(t)
	tpl := e.call("get_document_template", map[string]any{"id": "tpl-1"})
	if len(list(tpl, "fields")) == 0 {
		t.Fatal("the template has no fields")
	}
	out := e.call("create_document_from_template", map[string]any{
		"template_id": "tpl-1", "title": "Договір №45", "amount": "25000.00",
		"fields": map[string]any{"contract_number": "45"},
	})
	if out["document"].(map[string]any)["id"] == nil {
		t.Fatalf("no document was created: %v", out)
	}
	if out["next_step"] == nil {
		t.Error("the answer should say what to do next")
	}
}

func TestCheckCounterparty(t *testing.T) {
	e := setup(t)
	out := e.call("check_counterparty", map[string]any{"edrpou": "87654321"})
	if out["is_registered"] != true {
		t.Errorf("expected a registered counterparty: %v", out)
	}
	missing := e.call("check_counterparty", map[string]any{"edrpou": "00000000"})
	if missing["is_registered"] != false {
		t.Errorf("expected an unregistered counterparty: %v", missing)
	}
	if msg := e.callErr("check_counterparty", map[string]any{}); !strings.Contains(msg, "edrpou or file") {
		t.Errorf("unexpected message: %s", msg)
	}
}

// ── diagnostics ────────────────────────────────────────────────

func TestSelfCheckPasses(t *testing.T) {
	e := setup(t)
	out := e.call("self_check", map[string]any{"deep": true})
	if out["api_open"] != true {
		t.Fatalf("the API should be open: %v", out)
	}
	if fmt.Sprint(out["failed"]) != "0" {
		t.Errorf("checks failed: %v", out["checks"])
	}
	if fmt.Sprint(out["passed"]) == "0" {
		t.Error("no check ran")
	}
	if strings.Contains(fmt.Sprint(out["token"]), "test-token") {
		t.Error("self_check leaks the raw token")
	}
}

func TestNoTariffIsExplained(t *testing.T) {
	e := setupWith(t, mockvchasno.Options{NoTariff: true}, nil)
	out := e.call("self_check", map[string]any{})
	if out["api_open"] != false {
		t.Fatalf("expected api_open=false: %v", out)
	}
	if !strings.Contains(fmt.Sprint(out["checks"]), "Інтеграція") {
		t.Errorf("the diagnosis should name the missing tariff: %v", out["checks"])
	}
	// Data tools refuse early, with the same explanation instead of a raw 403.
	msg := e.callErr("list_documents", map[string]any{})
	if !strings.Contains(msg, "access_denied") || !strings.Contains(msg, "activate_integration_trial") {
		t.Errorf("a closed API should be explained, got %s", msg)
	}
	// The trial tool still works — it is the way out of this state.
	if out := e.call("activate_integration_trial", map[string]any{"confirm": true}); out["ok"] != true {
		t.Errorf("the trial could not be activated: %v", out)
	}
}

func TestRetriesSurviveRateLimiting(t *testing.T) {
	e := setupWith(t, mockvchasno.Options{FailEvery: 3}, nil)
	// Every third request answers 429; the client must retry through it.
	for i := 0; i < 5; i++ {
		out := e.call("list_documents", map[string]any{"limit": 3})
		if out["count"] == nil {
			t.Fatalf("call %d lost its answer: %v", i, out)
		}
	}
}

// ── resources and prompts ──────────────────────────────────────

func TestResources(t *testing.T) {
	e := setup(t)
	var uris []string
	for r, err := range e.cs.Resources(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		uris = append(uris, r.URI)
	}
	want := []string{"vchasno://company/overview", "vchasno://company/reference", "vchasno://guide/workflow",
		"vchasno://guide/statuses", "vchasno://guide/categories", "vchasno://guide/permissions",
		"vchasno://guide/errors", "vchasno://guide/integration"}
	for _, w := range want {
		found := false
		for _, u := range uris {
			if u == w {
				found = true
			}
		}
		if !found {
			t.Errorf("resource %q is missing", w)
		}
	}
	for _, uri := range want {
		res, err := e.cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri})
		if err != nil {
			t.Fatalf("%s: %v", uri, err)
		}
		if len(res.Contents) == 0 || len(res.Contents[0].Text) < 100 {
			t.Errorf("%s is empty or too short", uri)
		}
	}
}

func TestResourceContentIsUseful(t *testing.T) {
	e := setup(t)
	statuses, err := e.cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "vchasno://guide/statuses"})
	if err != nil {
		t.Fatal(err)
	}
	text := statuses.Contents[0].Text
	for _, code := range []string{"7000", "7004", "7008", "7011"} {
		if !strings.Contains(text, code) {
			t.Errorf("the status guide does not mention %s", code)
		}
	}
	overview, err := e.cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "vchasno://company/overview"})
	if err != nil {
		t.Fatal(err)
	}
	var ov map[string]any
	if err := json.Unmarshal([]byte(overview.Contents[0].Text), &ov); err != nil {
		t.Fatalf("the overview is not JSON: %v", err)
	}
	if ov["api_open"] != true {
		t.Errorf("the overview should report an open API: %v", ov)
	}
	if ov["counts"] == nil {
		t.Error("the overview has no counts")
	}
}

func TestDocumentResourceTemplate(t *testing.T) {
	e := setup(t)
	res, err := e.cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "vchasno://document/doc-006"})
	if err != nil {
		t.Fatalf("reading one document as a resource: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(res.Contents[0].Text), &out); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	doc := out["document"].(map[string]any)
	if doc["id"] != "doc-006" {
		t.Errorf("wrong document: %v", doc["id"])
	}
	if _, err := e.cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "vchasno://document/nope"}); err == nil {
		t.Error("an unknown document should not resolve")
	}
}

func TestPrompts(t *testing.T) {
	e := setup(t)
	var names []string
	for p, err := range e.cs.Prompts(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, p.Name)
	}
	want := []string{"onboard_company", "outgoing_overview", "incoming_inbox", "send_document_flow", "chase_unsigned",
		"counterparty_dossier", "monthly_report", "sync_integration", "troubleshoot_access"}
	for _, w := range want {
		found := false
		for _, n := range names {
			if n == w {
				found = true
			}
		}
		if !found {
			t.Errorf("prompt %q is missing", w)
		}
	}
}

func TestPromptsRenderAndValidate(t *testing.T) {
	e := setup(t)
	res, err := e.cs.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: "send_document_flow",
		Arguments: map[string]string{"file_path": "/tmp/akt.pdf", "edrpou": "87654321", "amount": "500.00"}})
	if err != nil {
		t.Fatal(err)
	}
	text := res.Messages[0].Content.(*mcp.TextContent).Text
	for _, want := range []string{"/tmp/akt.pdf", "87654321", "upload_document", "send_document"} {
		if !strings.Contains(text, want) {
			t.Errorf("the rendered prompt lacks %q", want)
		}
	}
	if _, err := e.cs.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: "send_document_flow"}); err == nil {
		t.Error("a prompt with missing required arguments should fail")
	}
	monthly, err := e.cs.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: "monthly_report", Arguments: map[string]string{"month": "2026-08"}})
	if err != nil {
		t.Fatal(err)
	}
	mtext := monthly.Messages[0].Content.(*mcp.TextContent).Text
	if !strings.Contains(mtext, "2026-08-01") || !strings.Contains(mtext, "2026-08-31") {
		t.Errorf("the month was not expanded into a date range: %s", mtext)
	}
}

func TestServerInstructionsMentionKeyRules(t *testing.T) {
	e := setup(t)
	instr := e.cs.InitializeResult().Instructions
	for _, want := range []string{"kopiyka", "7000", "confirm=true", "list_documents", "vchasno://"} {
		if !strings.Contains(instr, want) {
			t.Errorf("the server instructions do not mention %q", want)
		}
	}
}
