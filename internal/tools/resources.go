package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/vchasno"
)

// MCP resources: documents an agent can read instead of spending tool calls
// on discovery. Two kinds live here — what this particular company contains
// (overview, reference data, one document) and what anybody working with
// Vchasno has to know (statuses, types, permissions, errors, integration
// patterns).
//
// URIs:
//   vchasno://company/overview       connection, tariffs, limits, counts (JSON)
//   vchasno://company/reference      employees, teams, labels, parameters, types (JSON)
//   vchasno://guide/workflow         which tool answers which question (Markdown)
//   vchasno://guide/statuses         the status machine and what moves a document (Markdown)
//   vchasno://guide/categories       document types with their ids (Markdown)
//   vchasno://guide/permissions      employee permission and notification flags (Markdown)
//   vchasno://guide/errors           API errors and what to do about them (Markdown)
//   vchasno://guide/integration      sync, idempotency, rate limits, file naming (Markdown)
//   vchasno://document/{id}          one document with its signatures and comments (JSON)

const (
	uriOverview    = "vchasno://company/overview"
	uriReference   = "vchasno://company/reference"
	uriWorkflow    = "vchasno://guide/workflow"
	uriStatuses    = "vchasno://guide/statuses"
	uriCategories  = "vchasno://guide/categories"
	uriPermissions = "vchasno://guide/permissions"
	uriErrors      = "vchasno://guide/errors"
	uriIntegration = "vchasno://guide/integration"
	uriDocumentTpl = "vchasno://document/{id}"
)

type resourceDef struct {
	uri, name, title, desc, mime string
	build                        func(context.Context) (string, error)
}

func (d *Deps) registerResources(srv *mcp.Server) {
	defs := []resourceDef{
		{uriOverview, "company_overview", "Огляд компанії",
			"This connection: host, whether the API is open, active tariffs with their limits and usage, and how many employees, teams, labels, parameters and document types the company has.",
			"application/json", d.buildOverview},
		{uriReference, "company_reference", "Довідники компанії",
			"The company's own reference data in one document: employees with role ids and emails, teams, labels, extra document parameters and the private document types. This is the mapping from the names a person uses to the ids the API wants.",
			"application/json", d.buildReference},
		{uriWorkflow, "guide_workflow", "Який інструмент для якого питання",
			"A cookbook mapping business questions to tool calls: send a document, chase an unsigned one, work the inbox, sync an integration, export recognised data, manage access.",
			"text/markdown", d.buildWorkflow},
		{uriStatuses, "guide_statuses", "Статуси документів",
			"The document status machine: every code, what it means, what moves a document out of it, and which statuses still allow editing, signing or deletion.",
			"text/markdown", d.buildStatuses},
		{uriCategories, "guide_categories", "Типи документів",
			"Document types with their numeric ids — the public catalogue merged with this company's own types.",
			"text/markdown", d.buildCategories},
		{uriPermissions, "guide_permissions", "Права співробітників",
			"Every employee permission and notification flag update_role accepts, with what each one governs.",
			"text/markdown", d.buildPermissions},
		{uriErrors, "guide_errors", "Помилки API",
			"The documented API errors, what causes each one and what to do about it.",
			"text/markdown", d.buildErrors},
		{uriIntegration, "guide_integration", "Інтеграція",
			"How to build a reliable integration on this API: incremental sync windows, the processed flag, the has_changed trap, rate limits, file-name metadata, upload limits and the two signing paths.",
			"text/markdown", d.buildIntegration},
	}
	for _, r := range defs {
		r := r
		srv.AddResource(&mcp.Resource{URI: r.uri, Name: r.name, Title: r.title, Description: r.desc, MIMEType: r.mime},
			func(ctx context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				text, err := r.build(ctx)
				if err != nil {
					return nil, err
				}
				return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: r.uri, MIMEType: r.mime, Text: text}}}, nil
			})
	}

	srv.AddResourceTemplate(&mcp.ResourceTemplate{URITemplate: uriDocumentTpl, Name: "document", Title: "Документ", MIMEType: "application/json",
		Description: "One document by id, with its parties, signatures, comments and approval state — the same content as get_document with full=true, addressable as a resource."},
		func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			id := strings.TrimPrefix(req.Params.URI, "vchasno://document/")
			if id == "" || id == req.Params.URI {
				return nil, mcp.ResourceNotFoundError(req.Params.URI)
			}
			text, err := d.buildDocument(ctx, id)
			if err != nil {
				return nil, mcp.ResourceNotFoundError(req.Params.URI)
			}
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: req.Params.URI, MIMEType: "application/json", Text: text}}}, nil
		})
}

func jsonText(v any) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (d *Deps) buildOverview(ctx context.Context) (string, error) {
	out := map[string]any{
		"host":           d.Sess.Creds.BaseURL,
		"api_open":       d.Sess.APIOpen,
		"read_only_mode": d.Cfg.ReadOnly,
		"limits": map[string]any{
			"requests_per_second_allowed_by_vchasno": 10,
			"client_pacing_rps":                      d.Cfg.MaxRPS,
			"max_upload_bytes":                       d.Cfg.MaxUploadBytes,
			"max_files_per_zip":                      500,
			"max_ids_per_status_call":                500,
			"max_documents_per_recognition_call":     100,
		},
	}
	if !d.Sess.APIOpen {
		out["api_closed_reason"] = d.Sess.APINote
		out["fix"] = "activate the 'Інтеграція' tariff, or its one-time 30-day trial with activate_integration_trial"
		return jsonText(out)
	}
	if billing, err := d.api(ctx).GetBilling(ctx); err == nil {
		out["billing"] = billing
	}
	counts := map[string]any{}
	if roles, err := d.Sess.Roles(ctx); err == nil {
		counts["employees"] = len(roles)
	}
	if groups, err := d.Sess.Groups(ctx); err == nil {
		counts["teams"] = len(groups)
	}
	if tags, err := d.Sess.Tags(ctx); err == nil {
		counts["labels"] = len(tags)
	}
	if fields, err := d.Sess.Fields(ctx); err == nil {
		counts["extra_parameters"] = len(fields)
	}
	counts["document_types"] = len(d.Sess.Categories(ctx))
	out["counts"] = counts
	return jsonText(out)
}

func (d *Deps) buildReference(ctx context.Context) (string, error) {
	out := map[string]any{}
	if roles, err := d.Sess.Roles(ctx); err == nil {
		out["employees"] = roles
	}
	if groups, err := d.Sess.Groups(ctx); err == nil {
		out["teams"] = groups
	}
	if tags, err := d.Sess.Tags(ctx); err == nil {
		out["labels"] = tags
	}
	if fields, err := d.Sess.Fields(ctx); err == nil {
		out["extra_parameters"] = fields
	}
	if cats, err := d.api(ctx).ListCategories(ctx); err == nil {
		private := make([]vchasno.Category, 0)
		for _, c := range cats {
			if c.IsPublic != nil && !*c.IsPublic {
				private = append(private, c)
			}
		}
		out["company_document_types"] = private
		out["all_document_types_count"] = len(cats)
	}
	out["note"] = "Every tool of this server accepts an employee email instead of a role id, a label name instead of a label id, a team name instead of a team id and a document type title instead of a category number."
	return jsonText(out)
}

func (d *Deps) buildDocument(ctx context.Context, id string) (string, error) {
	var f vchasno.DocumentFilter
	applyWith(&f, []string{"recipients", "connections", "tags", "fields", "versions"})
	doc, err := d.api(ctx).GetDocument(ctx, id, f)
	if err != nil {
		return "", err
	}
	doc.Enrich(d.Sess.Categories(ctx))
	res := documentDossier{Document: *doc}
	if sigs, serr := d.api(ctx).ListSignatures(ctx, id); serr == nil {
		res.Signatures = sigs
	}
	if comments, cerr := d.api(ctx).DocumentComments(ctx, id); cerr == nil {
		res.Comments = comments
	}
	if review, rerr := d.api(ctx).ReviewState(ctx, id); rerr == nil && review.Status != "" {
		res.Review = review
	}
	return jsonText(res)
}

func (d *Deps) buildStatuses(_ context.Context) (string, error) {
	var b strings.Builder
	b.WriteString("# Статуси документів у Вчасно.ЕДО\n\n")
	b.WriteString("The status is the single most important field of a document: it says what may still be done to it.\n\n")
	b.WriteString("| Code | Name | Meaning |\n|---|---|---|\n")
	codes := make([]int, 0, len(vchasno.StatusMeaning))
	for c := range vchasno.StatusMeaning {
		codes = append(codes, c)
	}
	sort.Ints(codes)
	for _, c := range codes {
		fmt.Fprintf(&b, "| %d | %s | %s |\n", c, vchasno.StatusName[c], vchasno.StatusMeaning[c])
	}
	b.WriteString(`
## What moves a document

- **7000 → 7001** — the counterparty gets filled in: their email arrives with the upload, or ` + "`set_document_recipient`" + ` adds it later.
- **7001 → 7002/7004** — ` + "`send_document`" + `. If your side signs first, sign with ` + "`add_signature`" + ` (or ` + "`cloud_sign_document`" + `) and then send: the document goes to 7004, waiting for the counterparty. If the counterparty signs first, sending alone puts it in 7002.
- **7003 / 7007** — a partial state: some of the required signatures on that side are in place, or everything is signed but the document has not been sent onward. ` + "`send_document`" + ` clears the second case.
- **→ 7008** — the last expected signature lands. ` + "`date_finished`" + ` is set; a later extra signature updates it again.
- **→ 7006** — somebody calls ` + "`reject_document`" + `. The reason is kept and shows up in the comment feed with type=rejection.
- **7010** — a multilateral document that ` + "`set_multilateral_route`" + ` has just sent to its signers.
- **7011** — revoked; currently EDI documents only.

## What each status allows

- **Editing attributes** (` + "`update_document_info`" + `) — only below 7003, and only for the owner company.
- **Changing the counterparty** (` + "`set_document_recipient`" + `) — only while the counterparty has not signed.
- **Adding a signature** — 7001, 7002, 7003, 7004, 7007, 7010.
- **Creating a public link** — 7000 only.
- **Deleting outright** — the owner company with the delete permission. An external document in 7008 needs a deletion request agreed with the other party instead.

## Two flags that are not statuses

- **is_delivered** — somebody on the counterparty's side has actually opened the document list in Vchasno. It says nothing about signing.
- **processed** — your own company marked the document as handled (` + "`mark_documents_processed`" + `). Vchasno clears it again when the document changes.
`)
	return b.String(), nil
}

func (d *Deps) buildCategories(ctx context.Context) (string, error) {
	var b strings.Builder
	b.WriteString("# Типи документів (category)\n\nThe numeric ids the `category` filter and the upload tools want.\n\n")
	cats := d.Sess.Categories(ctx)
	ids := make([]int, 0, len(cats))
	for id := range cats {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	b.WriteString("| id | Назва |\n|---|---|\n")
	for _, id := range ids {
		fmt.Fprintf(&b, "| %d | %s |\n", id, cats[id])
	}
	b.WriteString("\nTypes a company defines itself (`is_public=false` in `list_document_categories`) may be used on internal documents only. Every tool here also accepts the title instead of the id.\n")
	return b.String(), nil
}

func (d *Deps) buildWorkflow(_ context.Context) (string, error) {
	return `# Питання → інструмент

## Getting oriented
- Does this connection work at all, and what may it do → ` + "`self_check`" + `, resource ` + "`vchasno://company/overview`" + `.
- Who works here, what teams/labels/parameters exist → resource ` + "`vchasno://company/reference`" + `, or ` + "`list_roles`" + ` / ` + "`list_groups`" + ` / ` + "`list_tags`" + ` / ` + "`list_fields`" + `.

## Outgoing documents
- What did we send, and where is it stuck → ` + "`list_documents`" + ` with a date range, then filter by ` + "`status`" + `.
- What is waiting for the counterparty → ` + "`list_documents status=waiting_for_counterparty`" + ` (7004).
- What did they reject → ` + "`list_documents status=rejected`" + `, then ` + "`get_document_comments`" + ` for the reason.
- Everything about one document → ` + "`get_document`" + ` with ` + "`full=true`" + `.

## Sending a document
1. ` + "`check_counterparty`" + ` — is their ЄДРПОУ registered in Vchasno?
2. ` + "`upload_document`" + ` with the file, the counterparty's ЄДРПОУ and email, the title, number, date, type and amount.
3. If the counterparty was missing: ` + "`set_document_recipient`" + `.
4. Sign: ` + "`add_signature`" + ` with a ready .p7s, or the ` + "`cloud_sign_*`" + ` flow with a Vchasno.KEP key.
5. ` + "`send_document`" + `.
6. Watch it: ` + "`get_document_statuses`" + ` on the ids, or ` + "`sync_changed_documents`" + `.

A document that follows a standard route is simpler: give ` + "`upload_document`" + ` a ` + "`scenario_id`" + ` from ` + "`list_scenarios`" + ` and Vchasno fills in approvers, signers, viewers, labels and parameters itself.

## Incoming documents
- The inbox → ` + "`list_incoming_documents processed=false`" + `.
- Handled them → ` + "`mark_documents_processed`" + `.
- Accept → sign with ` + "`add_signature`" + ` and ` + "`send_document`" + `; refuse → ` + "`reject_document`" + ` with a reason.
- Pull the data out of a PDF → ` + "`start_structured_data`" + `, poll ` + "`list_structured_data`" + `, then ` + "`download_structured_data`" + ` once the state is ` + "`confirmed`" + `.

## Internal approval
- Where does it stand → ` + "`get_review_state`" + `.
- Add or remove an approver → ` + "`add_reviewer`" + ` / ` + "`remove_reviewer`" + ` (a person by email, or a whole team by name).
- Block signing until approval is done → upload with ` + "`is_required_review=true`" + `.

## Files
- The original, the signed ZIP, the .p7s, the ASiC, a printable PDF → ` + "`download_document`" + ` with the matching ` + "`format`" + `.
- Just links to hand over → ` + "`get_download_links`" + `.

## Access
- Who may see a document → ` + "`set_document_access`" + ` (private/extended) and ` + "`set_document_viewers`" + ` (people and teams).
- Access by label: ` + "`tag_documents`" + ` marks the documents, ` + "`tag_employees`" + ` gives people the same label, and they see everything carrying it.
- Someone without a Vchasno account → ` + "`create_sign_session`" + ` (a named person) or ` + "`create_public_link`" + ` (a link).

## Deleting
- Your own document, not yet signed by everyone → ` + "`delete_document`" + ` with ` + "`confirm=true`" + `.
- Signed by both sides → ` + "`create_delete_request`" + `, and the other party answers with ` + "`accept_delete_request`" + ` or ` + "`reject_delete_request`" + `.
- Stop a counterparty deleting what they sent you → ` + "`lock_document_deletion`" + `.

## Reports and audit
- Who did what to the documents → ` + "`request_actions_report kind=documents`" + ` (up to 30 days, at most a year back), then ` + "`get_actions_report`" + `.
- What employees did → the same with ` + "`kind=users`" + `.
`, nil
}

func (d *Deps) buildPermissions(_ context.Context) (string, error) {
	return `# Права та сповіщення співробітника

` + "`update_role`" + ` accepts these flags. ` + "`is_admin=true`" + ` (user_role 8001) switches every permission on at once.

## Права доступу
| Flag | Governs |
|---|---|
| can_view_document | Seeing every document with company-wide access |
| can_view_private_document | Seeing documents marked private |
| can_comment_document | Commenting |
| can_upload_document | Uploading documents |
| can_download_document | Downloading files |
| can_print_document | Printing |
| can_delete_document | Deleting documents |
| can_delete_document_extended | Deleting any document of the company |
| can_sign_and_reject_document | Signing and rejecting |
| can_remove_itself_from_approval | Taking oneself out of an approval |
| can_change_document_signers_and_reviewers | Changing a signing or approval process already under way |
| can_edit_company | Company-wide settings |
| can_edit_roles | Editing and removing employees |
| can_invite_coworkers | Inviting employees |
| can_view_coworkers | Seeing the employee list |
| can_edit_document_templates | Configuring scenarios |
| can_edit_templates | Creating and editing file templates |
| can_edit_document_fields | Configuring extra parameters |
| can_edit_required_fields | Configuring mandatory fields for incoming documents |
| can_edit_document_category | Managing the company's own document types |
| can_create_tags | Creating labels |
| can_edit_directories | Managing archive folders |
| can_archive_documents | Moving documents into the archive and importing signed ones |
| can_delete_archived_documents | Deleting archived documents |
| can_download_actions | Saving action-history reports |
| can_edit_company_contact | Managing counterparty contacts |
| can_edit_security | Changing security settings |

## Сповіщення
| Flag | Sent when |
|---|---|
| can_receive_inbox | A document arrives |
| can_receive_inbox_as_default | A document arrives for an unregistered recipient |
| can_receive_comments | Someone comments |
| can_receive_rejects | A document is rejected |
| can_receive_reviews | An approval concerns them |
| can_receive_reminders | Requests are left unhandled |
| can_receive_access_to_doc | They are granted access to a document |
| can_receive_delete_requests | A deletion request arrives |
| can_receive_review_process_finished | An approval finishes on a document they uploaded |
| can_receive_review_process_finished_assigner | An approval they started finishes |
| can_receive_sign_process_finished | A signing finishes on a document they uploaded |
| can_receive_sign_process_finished_assigner | A signing they started finishes |
| can_receive_finished_docs | A document is completed |
| can_receive_new_roles | A new employee joins the company |
| can_receive_token_expiration | An integration token is about to expire |
| can_receive_email_change | An employee changes their email |
| can_receive_admin_role_deletion | An administrator is removed |

## Інше
- ` + "`position`" + ` — job title.
- ` + "`allowed_ips`" + ` — sign-in only from these addresses; a variable address is written as a prefix with a star (` + "`192.168.*`" + `).
- ` + "`allowed_api_ips`" + ` — addresses allowed to call the API and Vchasno.KEP cloud signing. **A cloud-signing session fails until the calling server's address is on this list.**
- ` + "`show_child_documents`" + ` — show child documents in the register.
- ` + "`status: \"active\"`" + ` — restore a removed employee.
`, nil
}

func (d *Deps) buildErrors(_ context.Context) (string, error) {
	return `# Помилки API

| HTTP | code | Meaning | What to do |
|---|---|---|---|
| 400 | rate_upload_overlimit | The tariff's upload quota is exhausted | ` + "`get_billing`" + ` shows what is left; the tariff has to be extended |
| 403 | access_denied | The company has no active 'Інтеграція' (or 'AI Інтеграція') tariff, so the whole API is closed | Activate it in the cabinet, or the one-time 30-day trial with ` + "`activate_integration_trial`" + ` |
| 403 | login_required | The token was not sent, or it is no longer valid | Re-issue the employee token in Vchasno settings and log in again |
| 429 | too_many_requests | Over 10 requests per second for this company | Wait and retry; this client already paces itself, but other integrations of the same company share the limit |

## Errors without a code

- **403 on a single method** — the token's employee lacks that permission (deleting, archiving, editing parameters, inviting). ` + "`list_roles`" + ` shows who is who; ` + "`update_role`" + ` changes the flags; the full list is in ` + "`vchasno://guide/permissions`" + `.
- **404** — no such object, or your company has no access to it. A document id from another company answers exactly like a non-existent one.
- **400 on an edit** — usually the status: attributes are frozen from 7003 upwards, the counterparty cannot be changed once they have signed, and a public link only exists for a document in 7000.
- **400 on a public link** — ` + "`access_days`" + ` must be 1, 3, 5, 7, 14 or 30, and ` + "`recipient_email`" + ` and ` + "`email_text`" + ` go together or not at all.
- **400 on a report** — the period may not exceed 30 days and may not start more than a year ago.
- **400 invalid_request on the comment feed** — ` + "`/documents/comments`" + ` refuses an unbounded request: it needs both ends of the period, which its documentation does not mention. ` + "`list_comments`" + ` fills a missing date in with the last 30 days and says so.

Every tool of this server answers a failure with ` + "`error`" + `, the API's own ` + "`code`" + ` and ` + "`status`" + `, and a ` + "`hint`" + ` saying what to do — read the hint before retrying.
`, nil
}

func (d *Deps) buildIntegration(_ context.Context) (string, error) {
	return `# Як будувати інтеграцію на цьому API

## Incremental sync
Use ` + "`sync_changed_documents`" + `. It sits on ` + "`changed_from`/`changed_to`" + `, which are **idempotent**: the same window always answers the same set, so a crashed run can simply be repeated. Keep the start of the previous run and overlap it by about five minutes — changes reach the search index with a small delay.

**Do not build on ` + "`has_changed`" + `.** Reading it clears the flag on everything it returned, so a failure between reading and storing loses those documents for good. It is fine for a quick look, never for a pipeline.

Incoming documents have no ` + "`changed_from`" + ` filter in the API; the window falls back to ` + "`date_created`" + `, which catches new arrivals but not later edits of old ones. Combine it with ` + "`processed=false`" + `.

## The processed flag
` + "`mark_documents_processed`" + ` records what your side has imported, per company, up to 500 ids per call. Vchasno clears the flag again by itself when the document changes: the sender edits the recipient's email, or a new signature lands. That is a feature — a cleared flag means "look at this again".

## Matching to your own system
Pass your own id at upload (` + "`vendor_id`" + `) and filter by it later. Vchasno also reads the metadata from a structured file name:

` + "```" + `
<edrpou_owner>_<edrpou_recipient>_<YYYYMMDD>_<title>_<number>_<email>_<vendor_id>_<amount>.pdf
` + "```" + `

with the amount in kopiykas; ` + "`_seq`" + ` at the end of an internal document's name means its signers sign in turn. Explicit parameters are the more reliable channel — with them the file name may be anything.

## Limits
- 10 requests per second per company, shared by every integration the company runs.
- One upload: at most 500 files, 100 MB in total, 15 MB per file.
- ` + "`get_document_statuses`" + ` — 500 ids; ` + "`start_structured_data`" + ` — 100; ` + "`mark_documents_processed`" + ` — 500.
- Action reports — 30 days per report, starting at most a year back.

## Money and dates
Amounts travel as **integers in kopiykas**. This server takes hryvnias in its inputs (` + "`1234.56`" + `) and adds ` + "`amount_uah`" + ` next to every amount it returns, but a raw API answer is kopiykas. Dates are ISO 8601; filters take ` + "`YYYY-MM-DD`" + ` or ` + "`YYYY-MM-DDTHH:MM`" + `.

## Signing
There are exactly two ways, and neither puts a private key on this server:

1. **A ready detached signature.** Your own software produces a .p7s from the original; ` + "`add_signature`" + ` sends it base64-encoded, with an optional company stamp.
2. **A Vchasno.KEP cloud key.** ` + "`cloud_sign_create_session`" + ` → the owner confirms in the app → ` + "`cloud_sign_check_session`" + ` returns the token **once** → ` + "`cloud_sign_document`" + `. The address this server calls from must be in the employee's ` + "`allowed_api_ips`" + `.

Either way, signing does not send the document: ` + "`send_document`" + ` does.

## Pagination
The listing tools walk cursor pages for you — ` + "`limit`" + ` says how many documents you want, ` + "`pages`" + ` how many pages they may walk to get there. When the answer carries ` + "`next_cursor`" + `, more is waiting behind it.
`, nil
}
