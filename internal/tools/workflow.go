package tools

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/vchasno"
)

// Moving a document through its life: fixing its attributes and parties,
// choosing who signs and who approves, signing, sending, rejecting,
// commenting, and the two ways of signing without a local key file
// (Vchasno.KEP cloud keys and personal-cabinet sessions).

type updateInfoIn struct {
	ID       string `json:"id" jsonschema:"Document id"`
	Title    string `json:"title,omitempty" jsonschema:"New title"`
	Date     string `json:"date,omitempty" jsonschema:"New document date (YYYY-MM-DD or YYYY-MM-DDTHH:MM)"`
	Number   string `json:"number,omitempty" jsonschema:"New external number"`
	Category string `json:"category,omitempty" jsonschema:"New document type: numeric id or title"`
	Amount   string `json:"amount,omitempty" jsonschema:"New amount in hryvnias, e.g. 12500.00"`
}

type setRecipientIn struct {
	ID     string `json:"id" jsonschema:"Document id"`
	Edrpou string `json:"edrpou" jsonschema:"ЄДРПОУ/ІПН of the counterparty"`
	Email  string `json:"email" jsonschema:"Email of the counterparty"`
}

type recipientIn struct {
	Edrpou        string   `json:"edrpou" jsonschema:"ЄДРПОУ/ІПН of this party"`
	Emails        []string `json:"emails" jsonschema:"At least one email of this party"`
	Role          string   `json:"role,omitempty" jsonschema:"signer (default) or viewer"`
	IsEmailHidden bool     `json:"is_email_hidden,omitempty" jsonschema:"Hide this email from the other parties"`
}

type setRecipientsIn struct {
	ID         string        `json:"id" jsonschema:"Document id of a multilateral document"`
	Ordered    bool          `json:"ordered,omitempty" jsonschema:"true = the parties sign strictly in the listed order; false = in parallel"`
	Recipients []recipientIn `json:"recipients" jsonschema:"The complete party list after the change, 1…100 entries. It must include your own company and at least one other ЄДРПОУ, and at least one party with role=signer"`
}

type accessLevelIn struct {
	ID    string `json:"id" jsonschema:"Document id"`
	Level string `json:"level" jsonschema:"extended = every colleague with the general permission sees it; private = only the people explicitly granted access"`
}

type viewersIn struct {
	ID       string   `json:"id" jsonschema:"Document id"`
	Strategy string   `json:"strategy" jsonschema:"add = grant access, remove = take it away, replace = make this the whole list"`
	People   []string `json:"people,omitempty" jsonschema:"Employees by email or role id"`
	Teams    []string `json:"teams,omitempty" jsonschema:"Teams by name or id"`
}

type signersIn struct {
	ID         string   `json:"id" jsonschema:"Document id"`
	People     []string `json:"people,omitempty" jsonschema:"Employees who must sign, by email or role id"`
	Teams      []string `json:"teams,omitempty" jsonschema:"Teams whose members may sign, by name or id"`
	IsParallel bool     `json:"parallel,omitempty" jsonschema:"true = anyone may sign at any time; false = strictly in the order given"`
}

type routeStepIn struct {
	Edrpou     string   `json:"edrpou" jsonschema:"ЄДРПОУ of the signing company"`
	Emails     []string `json:"emails" jsonschema:"Emails of that company's signers"`
	Order      int      `json:"order,omitempty" jsonschema:"Position in the queue for sequential signing, starting at 0"`
	Signatures int      `json:"signatures,omitempty" jsonschema:"How many people of that company must sign (default 1)"`
}

type setRouteIn struct {
	ID    string        `json:"id" jsonschema:"Document id of a multilateral document"`
	Steps []routeStepIn `json:"steps" jsonschema:"The signing route. For sequential signing the document must have been uploaded with is_parallel=false"`
}

type signIn struct {
	ID        string `json:"id" jsonschema:"Document id"`
	Signature string `json:"signature_base64" jsonschema:"A ready detached signature container (.p7s) encoded as base64. This is a signature, never a key file (.dat, .zs2, .jks, .pfx …) — this server never sees private keys"`
	Stamp     string `json:"stamp_base64,omitempty" jsonschema:"An optional detached company stamp (.p7s) as base64"`
	Send      bool   `json:"send,omitempty" jsonschema:"Also call send_document afterwards, which forwards the document to the next party"`
}

type sendIn struct {
	ID string `json:"id" jsonschema:"Document id. In status 7001 this sends it to the counterparty for the first signature; after signing it forwards the document onward"`
}

type rejectIn struct {
	ID      string `json:"id" jsonschema:"Document id"`
	Text    string `json:"reason" jsonschema:"Why the document is rejected. The counterparty sees this text"`
	Confirm bool   `json:"confirm,omitempty" jsonschema:"Must be true: rejection is visible to the counterparty and cannot be taken back"`
}

type commentIn struct {
	ID         string `json:"id" jsonschema:"Document id"`
	Text       string `json:"text" jsonschema:"The comment"`
	IsInternal bool   `json:"internal,omitempty" jsonschema:"true = visible only to your colleagues; false (default) = the counterparty sees it too"`
}

type documentIDIn struct {
	ID string `json:"id" jsonschema:"Document id"`
}

type commentsFeedIn struct {
	DateFrom string `json:"date_from,omitempty" jsonschema:"Comments on or after this date"`
	DateTo   string `json:"date_to,omitempty" jsonschema:"Comments on or before this date"`
	Cursor   string `json:"cursor,omitempty" jsonschema:"next_cursor from a previous answer"`
	Pages    int    `json:"pages,omitempty" jsonschema:"How many cursor pages to walk (default 1)"`
}

type reviewerIn struct {
	ID         string `json:"id" jsonschema:"Document id"`
	Email      string `json:"email,omitempty" jsonschema:"Employee to add or remove, by email. Use either email or team, not both"`
	Team       string `json:"team,omitempty" jsonschema:"Team to add or remove, by name"`
	IsParallel bool   `json:"parallel,omitempty" jsonschema:"When adding: true = approvers work in parallel, false = in turn"`
}

type signSessionIn struct {
	DocumentID  string `json:"document_id" jsonschema:"Document the session is opened for"`
	Email       string `json:"email" jsonschema:"Email of the person who will view or sign"`
	Edrpou      string `json:"edrpou" jsonschema:"ЄДРПОУ/ІПН of that person's company"`
	Type        string `json:"type,omitempty" jsonschema:"view_session (default) or sign_session"`
	OnFinishURL string `json:"on_finish_url,omitempty" jsonschema:"Where to send the person after a successful signature"`
	OnCancelURL string `json:"on_cancel_url,omitempty" jsonschema:"Where to send the person after a rejection"`
}

type cloudCreateIn struct {
	ClientID        string `json:"client_id" jsonschema:"Id of the Vchasno.KEP cloud key or stamp"`
	Duration        int    `json:"duration_seconds,omitempty" jsonschema:"Session lifetime, 60…2592000 seconds. Ignored when use_refresh_token is true"`
	UseRefreshToken bool   `json:"use_refresh_token,omitempty" jsonschema:"true = issue an access token plus a refresh token instead of a single long-lived session token"`
}

type cloudCheckIn struct {
	AuthSessionID string `json:"auth_session_id" jsonschema:"Session id returned by cloud_sign_create_session"`
}

type cloudRefreshIn struct {
	AuthSessionID string `json:"auth_session_id" jsonschema:"Session id"`
	RefreshToken  string `json:"refresh_token" jsonschema:"The refresh token from the previous check or refresh"`
}

type cloudSignIn struct {
	DocumentID   string `json:"document_id" jsonschema:"Document to sign"`
	ClientID     string `json:"client_id" jsonschema:"Id of the Vchasno.KEP cloud key or stamp"`
	Password     string `json:"password" jsonschema:"Password of the cloud key. It is forwarded to Vchasno and never stored by this server"`
	SessionToken string `json:"session_token,omitempty" jsonschema:"Token from cloud_sign_check_session (sessions created without use_refresh_token)"`
	AccessToken  string `json:"access_token,omitempty" jsonschema:"Access token from cloud_sign_check_refresh_session or cloud_sign_refresh_token (sessions created with use_refresh_token)"`
	Confirm      bool   `json:"confirm,omitempty" jsonschema:"Must be true: signing is a legally meaningful act"`
}

func (d *Deps) registerWorkflow(srv *mcp.Server) {
	addRead(srv, "list_signatures", "Підписи документа",
		"Every signature on a document with its certificate details: who signed, their position, the certificate serial number, the issuing КНЕДП, the timestamp, whether a company stamp was applied and whether the signature is a European ECDSA one (those are exported with download_document format=asic).",
		func(ctx context.Context, _ *mcp.CallToolRequest, in documentIDIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			sigs, err := d.api(ctx).ListSignatures(ctx, in.ID)
			if err != nil {
				return fail(err)
			}
			return ok(map[string]any{"document_id": in.ID, "count": len(sigs), "signatures": sigs})
		})

	addRead(srv, "get_multilateral_route", "Маршрут багатостороннього документа",
		"The signing route of a multilateral document: which company signs in which position, how many signatures are still expected from each, and the emails involved. pending_signatures=0 means that party is done.",
		func(ctx context.Context, _ *mcp.CallToolRequest, in documentIDIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			flows, err := d.api(ctx).ListFlows(ctx, in.ID)
			if err != nil {
				return fail(err)
			}
			return ok(map[string]any{"document_id": in.ID, "steps": flows})
		})

	addRead(srv, "list_comments", "Стрічка коментарів",
		"The company-wide comment feed over a period: comments, rejection reasons and the messages attached to deletion requests and revocation acts, each with its document id, author and type.",
		func(ctx context.Context, _ *mcp.CallToolRequest, in commentsFeedIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			pages := d.pages(in.Pages)
			cursor := in.Cursor
			var all []vchasno.Comment
			for i := 0; i < pages; i++ {
				list, err := d.api(ctx).ListComments(ctx, in.DateFrom, in.DateTo, cursor)
				if err != nil {
					return fail(err)
				}
				all = append(all, list.Comments...)
				if list.NextCursor == nil || *list.NextCursor == "" {
					cursor = ""
					break
				}
				cursor = *list.NextCursor
			}
			out := map[string]any{"count": len(all), "comments": all}
			if cursor != "" {
				out["next_cursor"] = cursor
			}
			return ok(out)
		})

	addRead(srv, "get_document_comments", "Коментарі документа",
		"All comments on one document, with the full name and company of each author and the kind of each entry (comment, rejection, delete_request, delete_request_rejection, document_revoke_initiate, document_revoke_rejection).",
		func(ctx context.Context, _ *mcp.CallToolRequest, in documentIDIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			comments, err := d.api(ctx).DocumentComments(ctx, in.ID)
			if err != nil {
				return fail(err)
			}
			return ok(map[string]any{"document_id": in.ID, "count": len(comments), "comments": comments})
		})

	addRead(srv, "get_review_state", "Стан погодження",
		"Internal approval of a document in one answer: the overall state (pending / approved / rejected), whether signing is blocked until approval is done, who was assigned and what each of them did.",
		func(ctx context.Context, _ *mcp.CallToolRequest, in documentIDIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			api := d.api(ctx)
			out := map[string]any{"document_id": in.ID}
			var partial []string
			if st, err := api.ReviewState(ctx, in.ID); err == nil {
				out["status"] = st
			} else {
				partial = append(partial, "status: "+err.Error())
			}
			if reqs, err := api.ReviewRequests(ctx, in.ID); err == nil {
				out["assigned"] = reqs
			} else {
				partial = append(partial, "assigned: "+err.Error())
			}
			if hist, err := api.ReviewHistory(ctx, in.ID); err == nil {
				out["history"] = hist
			} else {
				partial = append(partial, "history: "+err.Error())
			}
			if len(partial) > 0 {
				out["could_not_read"] = partial
			}
			return ok(out)
		})

	if d.Cfg.ReadOnly {
		return
	}

	mcp.AddTool(srv, &mcp.Tool{Name: "update_document_info", Annotations: writes("Реквізити документа"),
		Description: "Change a document's title, date, number, type or amount. Only the owner company may do it, and only while the document is below status 7003 — after that the attributes are frozen. Pass the amount in hryvnias; it is converted to the kopiykas the API wants."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in updateInfoIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			var info vchasno.DocumentInfo
			if in.Title != "" {
				info.Title = &in.Title
			}
			if in.Date != "" {
				info.Date = &in.Date
			}
			if in.Number != "" {
				info.Number = &in.Number
			}
			if in.Category != "" {
				if n, err := atoiSafe(in.Category); err == nil {
					info.Category = &n
				} else if id, found := d.Sess.ResolveCategoryID(ctx, in.Category); found {
					info.Category = &id
				} else {
					return failf("unknown document type %q; call list_document_categories", in.Category)
				}
			}
			amount, err := amountPtr(in.Amount)
			if err != nil {
				return fail(err)
			}
			info.Amount = amount
			if info == (vchasno.DocumentInfo{}) {
				return failf("nothing to change: pass at least one of title, date, number, category, amount")
			}
			doc, err := d.api(ctx).UpdateDocumentInfo(ctx, in.ID, info)
			if err != nil {
				return fail(err)
			}
			doc.Enrich(d.Sess.Categories(ctx))
			return ok(map[string]any{"ok": true, "document": doc})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "set_document_recipient", Annotations: writes("Контрагент документа"),
		Description: "Set or replace the counterparty of a bilateral document. Only the owner company may do it, and only while the counterparty has not signed yet. A document in status 7000 moves to 7001 (ready to send) once it has a counterparty email."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in setRecipientIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if in.Edrpou == "" || in.Email == "" {
				return failf("both edrpou and email are required")
			}
			if err := d.api(ctx).SetRecipient(ctx, in.ID, in.Edrpou, in.Email); err != nil {
				return fail(err)
			}
			return done("recipient set", map[string]any{"document_id": in.ID, "edrpou": in.Edrpou, "email": in.Email,
				"next_step": "sign it (add_signature / cloud_sign_document) and call send_document"})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "set_document_recipients", Annotations: writes("Учасники багатостороннього документа"),
		Description: "Replace the whole party list of a multilateral document. Send the complete configuration, not a delta: your own company must be in it, at least two different ЄДРПОУ must appear, and at least one party must have role=signer. Parties that already signed cannot be dropped."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in setRecipientsIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if len(in.Recipients) == 0 {
				return failf("recipients must list every party of the document")
			}
			parties := make([]vchasno.MultilateralRecipient, 0, len(in.Recipients))
			for _, r := range in.Recipients {
				role := r.Role
				if role == "" {
					role = "signer"
				}
				if role != "signer" && role != "viewer" {
					return failf("role must be signer or viewer, got %q", r.Role)
				}
				if len(r.Emails) == 0 {
					return failf("party %s needs at least one email", r.Edrpou)
				}
				parties = append(parties, vchasno.MultilateralRecipient{Edrpou: r.Edrpou, Emails: r.Emails, IsEmailHidden: r.IsEmailHidden, Role: role})
			}
			if err := d.api(ctx).SetRecipients(ctx, in.ID, in.Ordered, parties); err != nil {
				return fail(err)
			}
			return done("recipients replaced", map[string]any{"document_id": in.ID, "parties": len(parties), "ordered": in.Ordered})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "set_document_access", Annotations: writes("Рівень доступу"),
		Description: "Switch a document between company-wide (extended) and restricted (private) visibility inside your own company. Who exactly may see a private document is managed by set_document_viewers."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in accessLevelIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if in.Level != "private" && in.Level != "extended" {
				return failf("level must be private or extended")
			}
			if err := d.api(ctx).SetAccessLevel(ctx, in.ID, in.Level); err != nil {
				return fail(err)
			}
			return done("access level set", map[string]any{"document_id": in.ID, "level": in.Level})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "set_document_viewers", Annotations: writes("Доступ співробітників"),
		Description: "Grant, revoke or replace read access to a document for colleagues and teams. People may be given by email and teams by name; both are resolved to the ids the API wants."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in viewersIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			switch in.Strategy {
			case "add", "remove", "replace":
			default:
				return failf("strategy must be add, remove or replace")
			}
			roles, err := d.Sess.ResolveRoleIDs(ctx, in.People)
			if err != nil {
				return fail(err)
			}
			var groups []string
			for _, t := range in.Teams {
				id, gerr := d.Sess.ResolveGroupID(ctx, t)
				if gerr != nil {
					return fail(gerr)
				}
				groups = append(groups, id)
			}
			if len(roles) == 0 && len(groups) == 0 && in.Strategy != "replace" {
				return failf("pass at least one person or team")
			}
			if err := d.api(ctx).SetViewers(ctx, in.ID, in.Strategy, groups, roles); err != nil {
				return fail(err)
			}
			return done("viewers updated", map[string]any{"document_id": in.ID, "strategy": in.Strategy, "roles": roles, "teams": groups})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "set_document_signers", Annotations: writes("Підписанти документа"),
		Description: "Choose which colleagues and teams must sign a document, and whether they sign in parallel or strictly in the listed order. Use this on a document already in Vchasno; at upload time the same thing is done with signer_emails."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in signersIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			var entities []vchasno.Entity
			roles, err := d.Sess.ResolveRoleIDs(ctx, in.People)
			if err != nil {
				return fail(err)
			}
			for _, r := range roles {
				entities = append(entities, vchasno.Entity{Type: "role", ID: r})
			}
			for _, t := range in.Teams {
				id, gerr := d.Sess.ResolveGroupID(ctx, t)
				if gerr != nil {
					return fail(gerr)
				}
				entities = append(entities, vchasno.Entity{Type: "group", ID: id})
			}
			if len(entities) == 0 {
				return failf("pass at least one person or team")
			}
			if err := d.api(ctx).SetSigners(ctx, in.ID, entities, in.IsParallel); err != nil {
				return fail(err)
			}
			return done("signers set", map[string]any{"document_id": in.ID, "signers": entities, "parallel": in.IsParallel})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "set_multilateral_route", Annotations: writes("Маршрут підписання"),
		Description: "Define the signing route of a multilateral document: which company signs at which position and how many of its people must sign. The document moves to status 7010 (sent to the signers) immediately after this call, so set it up completely in one go."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in setRouteIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if len(in.Steps) == 0 {
				return failf("steps must describe the signing route")
			}
			steps := make([]vchasno.FlowStep, 0, len(in.Steps))
			for _, s := range in.Steps {
				n := s.Signatures
				if n == 0 {
					n = 1
				}
				if len(s.Emails) == 0 {
					return failf("party %s needs at least one email", s.Edrpou)
				}
				steps = append(steps, vchasno.FlowStep{Edrpou: s.Edrpou, Emails: s.Emails, Order: s.Order, SignNum: n})
			}
			if err := d.api(ctx).SetFlow(ctx, in.ID, steps); err != nil {
				return fail(err)
			}
			return done("route set", map[string]any{"document_id": in.ID, "steps": steps,
				"note": "the document has moved to status 7010 and is now with its signers"})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "add_signature", Annotations: writes("Додати підпис"),
		Description: "Attach a ready detached signature (.p7s, base64) — and optionally a company stamp — to a document. Accepted in statuses 7001, 7002, 7003, 7004, 7007 and 7010. This server never holds private keys: produce the .p7s with your own signing software, or use cloud_sign_document for a Vchasno.KEP cloud key. Set send=true to forward the document in the same step."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in signIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if strings.TrimSpace(in.Signature) == "" {
				return failf("signature_base64 is required")
			}
			if err := d.api(ctx).AddSignature(ctx, in.ID, in.Signature, in.Stamp); err != nil {
				return fail(err)
			}
			out := map[string]any{"document_id": in.ID, "stamp_added": in.Stamp != ""}
			if in.Send {
				if err := d.api(ctx).SendDocument(ctx, in.ID); err != nil {
					out["send_error"] = err.Error()
					out["hint"] = "the signature was accepted but sending failed; call send_document separately"
					return ok(out)
				}
				out["sent"] = true
			}
			return done("signature added", out)
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "send_document", Annotations: writes("Надіслати документ"),
		Description: "Send a document onward. In status 7001 this delivers it to the counterparty for the first signature; after your side has signed it forwards the document to the next party. Sending is visible to the counterparty."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in sendIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if err := d.api(ctx).SendDocument(ctx, in.ID); err != nil {
				return fail(err)
			}
			return done("document sent", map[string]any{"document_id": in.ID})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "reject_document", Annotations: destructive("Відхилити документ"),
		Description: "Reject a document with a reason the counterparty will read. The document moves to status 7006 and the reason is kept in the comment feed. Requires confirm=true."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in rejectIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if strings.TrimSpace(in.Text) == "" {
				return failf("a rejection reason is required")
			}
			if err := confirmed(in.Confirm, "rejecting a document"); err != nil {
				return fail(err)
			}
			if err := d.api(ctx).RejectDocument(ctx, in.ID, in.Text); err != nil {
				return fail(err)
			}
			return done("document rejected", map[string]any{"document_id": in.ID, "reason": in.Text})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "add_comment", Annotations: writes("Додати коментар"),
		Description: "Post a comment on a document. internal=true keeps it inside your company; the default is visible to the counterparty as well."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in commentIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if strings.TrimSpace(in.Text) == "" {
				return failf("text is required")
			}
			if err := d.api(ctx).AddComment(ctx, in.ID, in.Text, in.IsInternal); err != nil {
				return fail(err)
			}
			return done("comment added", map[string]any{"document_id": in.ID, "internal": in.IsInternal})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "add_reviewer", Annotations: writes("Додати погоджувача"),
		Description: "Add a colleague (by email) or a team (by name) to the internal approval of a document. Exactly one of email and team."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in reviewerIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if (in.Email == "") == (in.Team == "") {
				return failf("pass exactly one of email or team")
			}
			if err := d.api(ctx).AddReviewer(ctx, in.ID, in.Email, in.Team, in.IsParallel); err != nil {
				return fail(err)
			}
			return done("reviewer added", map[string]any{"document_id": in.ID, "email": in.Email, "team": in.Team})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "remove_reviewer", Annotations: writes("Прибрати погоджувача"),
		Description: "Take a colleague or a team out of the internal approval of a document. Exactly one of email and team."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in reviewerIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if (in.Email == "") == (in.Team == "") {
				return failf("pass exactly one of email or team")
			}
			if err := d.api(ctx).RemoveReviewer(ctx, in.ID, in.Email, in.Team); err != nil {
				return fail(err)
			}
			return done("reviewer removed", map[string]any{"document_id": in.ID, "email": in.Email, "team": in.Team})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "create_sign_session", Annotations: writes("Сесія перегляду/підписання"),
		Description: "Open a personal-cabinet session so one named person can view or sign a document through a plain link, without a Vchasno account of their own. For a counterparty signing second, the document must already be signed and sent (status 7004); a document whose first signature belongs to the counterparty is sent automatically."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in signSessionIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			t := in.Type
			if t == "" {
				t = "view_session"
			}
			if t != "view_session" && t != "sign_session" {
				return failf("type must be view_session or sign_session")
			}
			body := map[string]any{"document_id": in.DocumentID, "email": in.Email, "edrpou": in.Edrpou, "type": t}
			if in.OnFinishURL != "" {
				body["on_finish_url"] = in.OnFinishURL
			}
			if in.OnCancelURL != "" {
				body["on_cancel_url"] = in.OnCancelURL
			}
			sess, err := d.api(ctx).CreateSignSession(ctx, body)
			if err != nil {
				return fail(err)
			}
			return ok(map[string]any{"ok": true, "session": sess, "open_this_url": sess.URL})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "cloud_sign_create_session", Annotations: writes("Хмарний ключ: сесія"),
		Description: "Open a Vchasno.KEP cloud-key signing session. The key owner gets a push in the Vchasno.KEP app (or a link to enter the key password); poll cloud_sign_check_session until it answers status=ready with a token. The IP this server calls from must be on the employee's allowed-IP list for cloud signing."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in cloudCreateIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if in.Duration != 0 && (in.Duration < 60 || in.Duration > 2592000) {
				return failf("duration_seconds must be between 60 and 2592000")
			}
			sess, err := d.api(ctx).CreateCloudSession(ctx, in.ClientID, in.Duration, in.UseRefreshToken)
			if err != nil {
				return fail(err)
			}
			next := "poll cloud_sign_check_session with this auth_session_id"
			if in.UseRefreshToken {
				next = "poll cloud_sign_check_refresh_session with this auth_session_id"
			}
			return ok(map[string]any{"ok": true, "session": sess, "next_step": next})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "cloud_sign_check_session", Annotations: writes("Хмарний ключ: статус"),
		Description: "Poll a cloud-key session. status=init means the owner has not confirmed yet; status=ready returns the session token — once, so keep it. status=provided means it was already handed out, expired means the session is over."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in cloudCheckIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			sess, err := d.api(ctx).CheckCloudSession(ctx, in.AuthSessionID)
			if err != nil {
				return fail(err)
			}
			return ok(sess)
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "cloud_sign_check_refresh_session", Annotations: writes("Хмарний ключ: статус (refresh)"),
		Description: "Poll a cloud-key session that was created with use_refresh_token=true. When ready it returns an access token, a refresh token and the access token's lifetime — once."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in cloudCheckIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			sess, err := d.api(ctx).CheckCloudRefreshSession(ctx, in.AuthSessionID)
			if err != nil {
				return fail(err)
			}
			return ok(sess)
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "cloud_sign_refresh_token", Annotations: writes("Хмарний ключ: оновити токен"),
		Description: "Exchange a refresh token for a new access token. Both tokens are rotated, so store what comes back and discard the old pair."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in cloudRefreshIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			sess, err := d.api(ctx).RefreshCloudToken(ctx, in.AuthSessionID, in.RefreshToken)
			if err != nil {
				return fail(err)
			}
			return ok(sess)
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "cloud_sign_document", Annotations: destructive("Підписати хмарним ключем"),
		Description: "Sign a document with a Vchasno.KEP cloud key. Needs an open session: pass session_token for a plain session or access_token for one created with use_refresh_token. Signing is a legally meaningful act, so confirm=true is required."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in cloudSignIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if err := confirmed(in.Confirm, "signing a document with a cloud key"); err != nil {
				return fail(err)
			}
			if (in.SessionToken == "") == (in.AccessToken == "") {
				return failf("pass exactly one of session_token or access_token, matching how the session was created")
			}
			body := map[string]any{"client_id": in.ClientID, "password": in.Password, "document_id": in.DocumentID}
			if in.SessionToken != "" {
				body["auth_session_token"] = in.SessionToken
			} else {
				body["access_token"] = in.AccessToken
			}
			res, err := d.api(ctx).CloudSignDocument(ctx, body)
			if err != nil {
				return fail(err)
			}
			return ok(map[string]any{"ok": true, "document_id": in.DocumentID, "result": res,
				"next_step": "call send_document to forward the document to the next party"})
		})
}
