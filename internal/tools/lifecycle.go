package tools

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/vchasno"
)

// What happens to a document once it exists: marking it handled, agreeing on
// its deletion, moving it to the archive, importing documents signed
// elsewhere, recognising structured data, publishing links and linking
// documents to one another.

type idsIn struct {
	IDs    []string `json:"ids" jsonschema:"Document ids"`
	IDsCSV string   `json:"ids_csv,omitempty" jsonschema:"Alternative to ids: the same list comma-separated"`
}

type deleteDocumentIn struct {
	ID      string `json:"id" jsonschema:"Document id"`
	Confirm bool   `json:"confirm,omitempty" jsonschema:"Must be true: deletion cannot be undone"`
}

type deleteRequestIn struct {
	ID      string `json:"id" jsonschema:"Document id"`
	Message string `json:"message,omitempty" jsonschema:"Why the document should be deleted. The other party reads this"`
	Confirm bool   `json:"confirm,omitempty" jsonschema:"Must be true for accept: accepting a deletion request deletes the document"`
}

type listDeleteRequestsIn struct {
	Status       string   `json:"status,omitempty" jsonschema:"new, accepted, rejected or canceled"`
	IDs          []string `json:"ids,omitempty" jsonschema:"Return only these delete-request ids"`
	WithOutgoing string   `json:"with_outgoing,omitempty" jsonschema:"true (default) = both the requests you received and the ones you raised; false = incoming only"`
	Cursor       string   `json:"cursor,omitempty" jsonschema:"Pagination cursor: the smallest 'cursor' value of the previous batch of 100"`
}

type archiveIn struct {
	IDs         []string `json:"ids" jsonschema:"Document ids to move"`
	IDsCSV      string   `json:"ids_csv,omitempty" jsonschema:"Alternative to ids: the same list comma-separated"`
	DirectoryID string   `json:"directory_id,omitempty" jsonschema:"Archive folder to put them in (list_archive_folders). Omit for the archive root"`
}

type listFoldersIn struct {
	ParentID string `json:"parent_id,omitempty" jsonschema:"Folder whose children to list. Omit for the root of the archive"`
	Search   string `json:"search,omitempty" jsonschema:"Search folders by name. Without parent_id it searches the whole company, with it only the children of that folder"`
	Cursor   string `json:"cursor,omitempty" jsonschema:"next_cursor from a previous answer"`
	Limit    int    `json:"limit,omitempty" jsonschema:"1…500, default 100"`
}

type uploadScanIn struct {
	fileInput
	ParentID string `json:"directory_id,omitempty" jsonschema:"Archive folder to upload into. Omit for the archive root"`
}

type importSignedIn struct {
	Original      *fileInput  `json:"original,omitempty" jsonschema:"External format: the original document. Send it together with one or more detached .p7s signatures"`
	Signatures    []fileInput `json:"signatures,omitempty" jsonschema:"External format: detached .p7s signature files, up to 20, each under 10 MB. Every signature must carry a qualified timestamp"`
	Container     *fileInput  `json:"container,omitempty" jsonschema:"Internal format: one .p7s or ASiC-E container holding the document and all its signatures. Use this instead of original+signatures"`
	Visualization *fileInput  `json:"pdf_visualization,omitempty" jsonschema:"Optional PDF rendering of the document, for formats Vchasno cannot display itself (XML). Up to 50 MB"`

	Title              string `json:"title,omitempty" jsonschema:"Document title (max 512 characters)"`
	Number             string `json:"number,omitempty" jsonschema:"Document number (max 512 characters)"`
	DateDocument       string `json:"date_document,omitempty" jsonschema:"Date the document was drawn up: YYYY-MM-DD, YYYY-MM-DDTHH:MM:SS, DD.MM.YYYY or DD/MM/YYYY"`
	Category           string `json:"category,omitempty" jsonschema:"Document type: numeric id or title"`
	Amount             string `json:"amount,omitempty" jsonschema:"Document amount in hryvnias"`
	CounterpartyEdrpou string `json:"counterparty_edrpou,omitempty" jsonschema:"ЄДРПОУ of the counterparty. Without it Vchasno takes the first signature whose ЄДРПОУ differs from yours"`
	ApplyVchasnoStamps string `json:"apply_vchasno_stamps,omitempty" jsonschema:"true (default) = draw Vchasno stamps over the original when rendering it; false for documents signed in another system. Ignored when a PDF visualization is supplied"`
}

type visualizationIn struct {
	DocumentID         string     `json:"document_id" jsonschema:"Document imported into the archive"`
	Visualization      *fileInput `json:"pdf_visualization,omitempty" jsonschema:"PDF rendering to attach or replace. Up to 50 MB"`
	ApplyVchasnoStamps string     `json:"apply_vchasno_stamps,omitempty" jsonschema:"true or false. May be sent on its own, without a file"`
}

type extractionStartIn struct {
	IDs    []string `json:"ids" jsonschema:"Document ids, 1…100 per call. Supported formats: pdf, doc(x), xml with a visualization, png, jpg"`
	IDsCSV string   `json:"ids_csv,omitempty" jsonschema:"Alternative to ids: the same list comma-separated"`
}

type extractionListIn struct {
	IDs    []string `json:"ids,omitempty" jsonschema:"Filter by document ids, up to 100"`
	Status string   `json:"status,omitempty" jsonschema:"pending, awaiting_validation, confirmed, downloaded or error"`
	Cursor string   `json:"cursor,omitempty" jsonschema:"next_cursor from a previous answer"`
	Limit  int      `json:"limit,omitempty" jsonschema:"1…100, default 100"`
}

type extractionDownloadIn struct {
	ID         string `json:"id" jsonschema:"Document id"`
	Format     string `json:"format,omitempty" jsonschema:"json (default), xml or xlsx"`
	InlineText bool   `json:"inline_text,omitempty" jsonschema:"Also put the recognised data in the answer when it is JSON or XML under 64 KiB (default: link only)"`
}

type publicLinkIn struct {
	DocumentID      string `json:"document_id" jsonschema:"Document to publish. Creating a link requires status 7000 (uploaded)"`
	Type            string `json:"type,omitempty" jsonschema:"view (default) = the recipient may read it; sign = the recipient may sign it"`
	AccessPeriod    int    `json:"access_days,omitempty" jsonschema:"Lifetime of the link in days: only 1, 3, 5, 7, 14 or 30"`
	SingleUse       bool   `json:"single_use,omitempty" jsonschema:"true = the link works exactly once"`
	RecipientEdrpou string `json:"recipient_edrpou,omitempty" jsonschema:"ЄДРПОУ/ІПН of the intended recipient"`
	RecipientEmail  string `json:"recipient_email,omitempty" jsonschema:"Email of the intended recipient. Must be paired with email_text"`
	EmailText       string `json:"email_text,omitempty" jsonschema:"Body of the notification email. Required together with recipient_email"`
}

type updatePublicLinkIn struct {
	SharedID        string `json:"shared_id" jsonschema:"Id of the public-link setting (the 'id' of get_public_link, not the document id)"`
	Type            string `json:"type" jsonschema:"view or sign"`
	AccessPeriod    int    `json:"access_days" jsonschema:"1, 3, 5, 7, 14 or 30"`
	SingleUse       bool   `json:"single_use,omitempty" jsonschema:"true = the link works exactly once"`
	RecipientEdrpou string `json:"recipient_edrpou,omitempty" jsonschema:"ЄДРПОУ/ІПН of the intended recipient"`
	RecipientEmail  string `json:"recipient_email,omitempty" jsonschema:"Email of the intended recipient. Changing it makes email_text required"`
	EmailText       string `json:"email_text,omitempty" jsonschema:"Body of the notification email. Only allowed together with recipient_email"`
}

type revokeLinkIn struct {
	SharedID string `json:"shared_id" jsonschema:"Id of the public-link setting"`
	Confirm  bool   `json:"confirm,omitempty" jsonschema:"Must be true: everyone holding the link loses access immediately"`
}

type childIn struct {
	ParentID string `json:"parent_id" jsonschema:"Main document"`
	ChildID  string `json:"child_id" jsonschema:"Document to attach to it"`
}

func (d *Deps) registerLifecycle(srv *mcp.Server) {
	addRead(srv, "list_delete_requests", "Запити на видалення",
		"Deletion requests of the company: what a counterparty asked you to delete and what you asked them to delete, with the reason, the initiator and the state (new, accepted, rejected, canceled).",
		func(ctx context.Context, _ *mcp.CallToolRequest, in listDeleteRequestsIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			withOutgoing, err := boolPtr(in.WithOutgoing)
			if err != nil {
				return failf("with_outgoing: %v", err)
			}
			reqs, err := d.api(ctx).ListDeleteRequests(ctx, in.Status, in.IDs, withOutgoing, in.Cursor)
			if err != nil {
				return fail(err)
			}
			return ok(map[string]any{"count": len(reqs), "delete_requests": reqs})
		})

	addRead(srv, "list_archive_folders", "Папки архіву",
		"Folders of the document archive, from the root down. Use the returned ids with archive_documents and upload_scan.",
		func(ctx context.Context, _ *mcp.CallToolRequest, in listFoldersIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			list, err := d.api(ctx).ListDirectories(ctx, in.ParentID, in.Search, in.Cursor, in.Limit)
			if err != nil {
				return fail(err)
			}
			return ok(map[string]any{"count": len(list.Directories), "next_cursor": list.NextCursor, "folders": list.Directories})
		})

	addRead(srv, "list_structured_data", "Розпізнавання документів",
		"Structured-data recognition runs with their state: pending (in progress), awaiting_validation (recognised, waiting for a human check), confirmed (checked — only then can the data be exported), downloaded, error.",
		func(ctx context.Context, _ *mcp.CallToolRequest, in extractionListIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			list, err := d.api(ctx).ListExtractions(ctx, in.IDs, in.Status, in.Cursor, in.Limit)
			if err != nil {
				return fail(err)
			}
			return ok(map[string]any{"count": len(list.Data), "next_cursor": list.NextCursor, "extractions": list.Data})
		})

	addRead(srv, "download_structured_data", "Вивантажити структуровані дані",
		"Export the data recognised from a document — parties with their bank details, line items with quantities, prices and VAT, and the totals — as json, xml or xlsx. The answer carries a download URL; pass inline_text=true to also get small JSON or XML in the answer itself. Only documents whose recognition is confirmed can be exported; for anything earlier the answer is the current recognition status instead of a file.",
		func(ctx context.Context, _ *mcp.CallToolRequest, in extractionDownloadIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			format := in.Format
			if format == "" {
				format = "json"
			}
			if format != "json" && format != "xml" && format != "xlsx" {
				return failf("format must be json, xml or xlsx")
			}
			resp, err := d.api(ctx).DownloadStructuredData(ctx, in.ID, format)
			if err != nil {
				return fail(err)
			}
			if status, isStatus := decodeStatusPayload(resp.Body); isStatus {
				return ok(map[string]any{"document_id": in.ID, "ready": false, "status": status,
					"hint": "the data is not exportable yet; it becomes exportable once the recognition is confirmed"})
			}
			saved, err := d.saveDownload(resp, in.ID+"-structured."+format, in.InlineText)
			if err != nil {
				return fail(err)
			}
			return ok(map[string]any{"document_id": in.ID, "ready": true, "format": format, "file": saved})
		})

	addRead(srv, "get_public_link", "Публічне посилання",
		"The public-link settings of a document: whether it is active, whether it is single-use, whether it allows viewing or signing, when it expires and the link itself.",
		func(ctx context.Context, _ *mcp.CallToolRequest, in documentIDIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			link, err := d.api(ctx).GetSharedLink(ctx, in.ID)
			if err != nil {
				if vchasno.IsNotFound(err) {
					return ok(map[string]any{"document_id": in.ID, "has_public_link": false})
				}
				return fail(err)
			}
			return ok(map[string]any{"document_id": in.ID, "has_public_link": link.ID != "", "link": link})
		})

	if d.Cfg.ReadOnly {
		return
	}

	mcp.AddTool(srv, &mcp.Tool{Name: "mark_documents_processed", Annotations: writes("Позначити обробленими"),
		Description: "Flag up to 500 documents as processed by your company — the standard way an integration records what it has already imported. The flag is per company and Vchasno clears it again when the document changes (a new signature, a changed recipient email)."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idsIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			ids := mergeIDs(in.IDs, in.IDsCSV)
			if len(ids) == 0 || len(ids) > 500 {
				return failf("pass between 1 and 500 document ids, got %d", len(ids))
			}
			res, err := d.api(ctx).MarkProcessed(ctx, ids)
			if err != nil {
				return fail(err)
			}
			return ok(map[string]any{"ok": true, "requested": len(ids), "updated": len(res.UpdatedIDs), "updated_ids": res.UpdatedIDs})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "delete_document", Annotations: destructive("Видалити документ"),
		Description: "Delete a document outright. Only the owner company may do it, and only with the delete permission. An external document already signed by everyone (7008) cannot be deleted this way — raise a deletion request instead (create_delete_request). Irreversible: requires confirm=true."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in deleteDocumentIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if err := confirmed(in.Confirm, "deleting a document"); err != nil {
				return fail(err)
			}
			if err := d.api(ctx).DeleteDocument(ctx, in.ID); err != nil {
				return fail(err)
			}
			return done("document deleted", map[string]any{"document_id": in.ID})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "create_delete_request", Annotations: writes("Запит на видалення"),
		Description: "Ask the other party to agree to deleting a document — the only way to remove a document both sides have signed. Available to administrators with the delete permission on either side."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in deleteRequestIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			reqs, err := d.api(ctx).CreateDeleteRequest(ctx, in.ID, in.Message)
			if err != nil {
				return fail(err)
			}
			return ok(map[string]any{"ok": true, "document_id": in.ID, "delete_requests": reqs})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "cancel_delete_request", Annotations: writes("Відкликати запит на видалення"),
		Description: "Withdraw a deletion request your own role raised earlier."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in documentIDIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if err := d.api(ctx).CancelDeleteRequest(ctx, in.ID); err != nil {
				return fail(err)
			}
			return done("delete request cancelled", map[string]any{"document_id": in.ID})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "accept_delete_request", Annotations: destructive("Прийняти запит на видалення"),
		Description: "Agree to a counterparty's deletion request. The document is deleted as a result, so this requires confirm=true."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in deleteRequestIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if err := confirmed(in.Confirm, "accepting a deletion request (the document is deleted)"); err != nil {
				return fail(err)
			}
			if err := d.api(ctx).AcceptDeleteRequest(ctx, in.ID); err != nil {
				return fail(err)
			}
			return done("delete request accepted", map[string]any{"document_id": in.ID})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "reject_delete_request", Annotations: writes("Відхилити запит на видалення"),
		Description: "Refuse a counterparty's deletion request, with a reason they will read."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in deleteRequestIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if strings.TrimSpace(in.Message) == "" {
				return failf("message is required: it becomes the reason the other party sees")
			}
			if err := d.api(ctx).RejectDeleteRequest(ctx, in.ID, in.Message); err != nil {
				return fail(err)
			}
			return done("delete request rejected", map[string]any{"document_id": in.ID, "reason": in.Message})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "lock_document_deletion", Annotations: writes("Заборонити пряме видалення"),
		Description: "Forbid the owner of these incoming documents from deleting them outright: they will have to raise a deletion request your company can refuse. Pass action=unlock to lift the restriction."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
			IDs    []string `json:"ids" jsonschema:"Document ids"`
			IDsCSV string   `json:"ids_csv,omitempty" jsonschema:"Alternative to ids: the same list comma-separated"`
			Action string   `json:"action,omitempty" jsonschema:"lock (default) or unlock"`
		}) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			ids := mergeIDs(in.IDs, in.IDsCSV)
			if len(ids) == 0 {
				return failf("pass at least one document id")
			}
			action := in.Action
			if action == "" {
				action = "lock"
			}
			var (
				res *vchasno.UpdatedIDs
				err error
			)
			switch action {
			case "lock":
				res, err = d.api(ctx).LockDelete(ctx, ids)
			case "unlock":
				res, err = d.api(ctx).UnlockDelete(ctx, ids)
			default:
				return failf("action must be lock or unlock")
			}
			if err != nil {
				return fail(err)
			}
			return ok(map[string]any{"ok": true, "action": action, "updated_ids": res.UpdatedIDs})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "archive_documents", Annotations: writes("В архів"),
		Description: "Move documents into the archive, optionally into a specific folder. Archived documents disappear from the main register but stay readable; is_archived=true in the listing tools finds them."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in archiveIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			ids := mergeIDs(in.IDs, in.IDsCSV)
			if len(ids) == 0 {
				return failf("pass at least one document id")
			}
			if err := d.api(ctx).ArchiveDocuments(ctx, ids, in.DirectoryID); err != nil {
				return fail(err)
			}
			return done("documents archived", map[string]any{"count": len(ids), "directory_id": in.DirectoryID})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "unarchive_documents", Annotations: writes("З архіву"),
		Description: "Move documents back from the archive into the main register."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idsIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			ids := mergeIDs(in.IDs, in.IDsCSV)
			if len(ids) == 0 {
				return failf("pass at least one document id")
			}
			if err := d.api(ctx).UnarchiveDocuments(ctx, ids); err != nil {
				return fail(err)
			}
			return done("documents unarchived", map[string]any{"count": len(ids)})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "upload_scan", Annotations: writes("Скан в архів"),
		Description: "Upload a scan straight into the archive, without any signing route. Useful for paper documents kept alongside the electronic ones."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in uploadScanIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			name, content, err := d.readFile(in.fileInput)
			if err != nil {
				return fail(err)
			}
			list, err := d.api(ctx).UploadScans(ctx, in.ParentID, []vchasno.FilePart{{Filename: name, Content: content}})
			if err != nil {
				return fail(err)
			}
			return ok(map[string]any{"ok": true, "file": name, "size_bytes": len(content), "documents": list.Documents})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "import_signed_document", Annotations: writes("Імпорт підписаного документа"),
		Description: "Import into the archive a document that was signed somewhere else. Two shapes are accepted: the external one — the original plus up to 20 detached .p7s signatures — and the internal one — a single .p7s or ASiC-E container holding both. Every signature must carry a qualified timestamp, and the caller needs the can_archive_documents permission. Vchasno works out the counterparty from the signatures unless counterparty_edrpou says otherwise."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in importSignedIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			var files []vchasno.FilePart
			switch {
			case in.Container != nil:
				name, content, err := d.readFile(*in.Container)
				if err != nil {
					return fail(err)
				}
				files = append(files, vchasno.FilePart{Field: "signed_file", Filename: name, Content: content})
			case in.Original != nil:
				if len(in.Signatures) == 0 {
					return failf("the external format needs at least one detached .p7s in signatures")
				}
				if len(in.Signatures) > 20 {
					return failf("at most 20 signature files, got %d", len(in.Signatures))
				}
				name, content, err := d.readFile(*in.Original)
				if err != nil {
					return fail(err)
				}
				files = append(files, vchasno.FilePart{Field: "file", Filename: name, Content: content})
				for _, sig := range in.Signatures {
					sname, scontent, serr := d.readFile(sig)
					if serr != nil {
						return fail(serr)
					}
					files = append(files, vchasno.FilePart{Field: "signatures", Filename: sname, Content: scontent})
				}
			default:
				return failf("pass either container (internal format) or original + signatures (external format)")
			}
			if in.Visualization != nil {
				vname, vcontent, verr := d.readFile(*in.Visualization)
				if verr != nil {
					return fail(verr)
				}
				files = append(files, vchasno.FilePart{Field: "pdf_visualization", Filename: vname, Content: vcontent})
			}
			fields, err := d.importFields(ctx, in)
			if err != nil {
				return fail(err)
			}
			res, err := d.api(ctx).ImportSigned(ctx, files, fields)
			if err != nil {
				return fail(err)
			}
			return ok(map[string]any{"ok": true, "imported": res})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "upload_archive_visualization", Annotations: writes("PDF-візуалізація"),
		Description: "Attach or replace the PDF rendering of a signed document imported into the archive, and/or switch the Vchasno stamps flag. At least one of the two must be given."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in visualizationIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			var files []vchasno.FilePart
			fields := map[string][]string{}
			if in.Visualization != nil {
				name, content, err := d.readFile(*in.Visualization)
				if err != nil {
					return fail(err)
				}
				files = append(files, vchasno.FilePart{Field: "pdf_visualization", Filename: name, Content: content})
			}
			if in.ApplyVchasnoStamps != "" {
				b, err := boolPtr(in.ApplyVchasnoStamps)
				if err != nil {
					return failf("apply_vchasno_stamps: %v", err)
				}
				fields["apply_vchasno_stamps"] = []string{boolText(*b)}
			}
			if len(files) == 0 && len(fields) == 0 {
				return failf("pass pdf_visualization, apply_vchasno_stamps, or both")
			}
			res, err := d.api(ctx).UploadVisualization(ctx, in.DocumentID, files, fields)
			if err != nil {
				return fail(err)
			}
			return ok(map[string]any{"ok": true, "document_id": in.DocumentID, "result": res})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "start_structured_data", Annotations: writes("Розпізнати документи"),
		Description: "Queue structured-data recognition for 1…100 documents (pdf, doc(x), xml with a visualization, png, jpg). Recognition is asynchronous: poll list_structured_data and export with download_structured_data once the status is confirmed."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in extractionStartIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			ids := mergeIDs(in.IDs, in.IDsCSV)
			if len(ids) == 0 || len(ids) > 100 {
				return failf("pass between 1 and 100 document ids, got %d", len(ids))
			}
			runs, err := d.api(ctx).StartExtraction(ctx, ids)
			if err != nil {
				return fail(err)
			}
			return ok(map[string]any{"ok": true, "count": len(runs), "extractions": runs})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "create_public_link", Annotations: writes("Створити публічне посилання"),
		Description: "Publish a link that lets someone without a Vchasno account read or sign a document. The document must be in status 7000 (uploaded), the lifetime is one of 1, 3, 5, 7, 14 or 30 days, and recipient_email and email_text go together or not at all."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in publicLinkIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			body, err := publicLinkBody(in.Type, in.AccessPeriod, in.SingleUse, in.RecipientEdrpou, in.RecipientEmail, in.EmailText)
			if err != nil {
				return fail(err)
			}
			link, err := d.api(ctx).CreateSharedLink(ctx, in.DocumentID, body)
			if err != nil {
				return fail(err)
			}
			return ok(map[string]any{"ok": true, "link": link})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "update_public_link", Annotations: writes("Оновити публічне посилання"),
		Description: "Change an existing public link: its type, its lifetime, whether it is single-use and who it is addressed to. Changing recipient_email makes email_text mandatory."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in updatePublicLinkIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			body, err := publicLinkBody(in.Type, in.AccessPeriod, in.SingleUse, in.RecipientEdrpou, in.RecipientEmail, in.EmailText)
			if err != nil {
				return fail(err)
			}
			link, err := d.api(ctx).UpdateSharedLink(ctx, in.SharedID, body)
			if err != nil {
				return fail(err)
			}
			return ok(map[string]any{"ok": true, "link": link})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "revoke_public_link", Annotations: destructive("Анулювати публічне посилання"),
		Description: "Deactivate a public link. Everyone holding it loses access at once, and the link cannot be brought back — requires confirm=true."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in revokeLinkIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if err := confirmed(in.Confirm, "revoking a public link"); err != nil {
				return fail(err)
			}
			if err := d.api(ctx).RevokeSharedLink(ctx, in.SharedID); err != nil {
				return fail(err)
			}
			return done("public link revoked", map[string]any{"shared_id": in.SharedID})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "attach_child_document", Annotations: writes("Прикріпити дочірній документ"),
		Description: "Link one document to another as its child — an act under a contract, an appendix under an act. The pair shows up in the listing tools with with=[\"connections\"]."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in childIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if err := d.api(ctx).AttachChild(ctx, in.ParentID, in.ChildID); err != nil {
				return fail(err)
			}
			return done("child attached", map[string]any{"parent_id": in.ParentID, "child_id": in.ChildID})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "detach_child_document", Annotations: writes("Відкріпити дочірній документ"),
		Description: "Unlink a child document from its parent. Both documents stay in Vchasno."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in childIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if err := d.api(ctx).DetachChild(ctx, in.ParentID, in.ChildID); err != nil {
				return fail(err)
			}
			return done("child detached", map[string]any{"parent_id": in.ParentID, "child_id": in.ChildID})
		})
}

func (d *Deps) importFields(ctx context.Context, in importSignedIn) (map[string][]string, error) {
	fields := map[string][]string{}
	put := func(k, v string) {
		if v != "" {
			fields[k] = []string{v}
		}
	}
	put("title", in.Title)
	put("number", in.Number)
	put("date_document", in.DateDocument)
	put("counterparty_edrpou", in.CounterpartyEdrpou)
	if in.Amount != "" {
		kop, err := amountPtr(in.Amount)
		if err != nil {
			return nil, err
		}
		put("amount", int64Text(*kop))
	}
	if in.Category != "" {
		if n, err := atoiSafe(in.Category); err == nil {
			put("category", int64Text(int64(n)))
		} else if id, found := d.Sess.ResolveCategoryID(ctx, in.Category); found {
			put("category", int64Text(int64(id)))
		} else {
			return nil, &vchasno.Error{Status: 400, Reason: "unknown document type " + in.Category}
		}
	}
	if in.ApplyVchasnoStamps != "" {
		b, err := boolPtr(in.ApplyVchasnoStamps)
		if err != nil {
			return nil, err
		}
		put("apply_vchasno_stamps", boolText(*b))
	}
	return fields, nil
}

func publicLinkBody(linkType string, days int, singleUse bool, edrpou, email, emailText string) (map[string]any, error) {
	if linkType == "" {
		linkType = "view"
	}
	if linkType != "view" && linkType != "sign" {
		return nil, &vchasno.Error{Status: 400, Reason: "type must be view or sign"}
	}
	allowed := false
	for _, p := range vchasno.AccessPeriods {
		if p == days {
			allowed = true
		}
	}
	if !allowed {
		return nil, &vchasno.Error{Status: 400, Reason: "access_days must be one of 1, 3, 5, 7, 14, 30"}
	}
	if (email == "") != (emailText == "") {
		return nil, &vchasno.Error{Status: 400, Reason: "recipient_email and email_text must be given together or not at all"}
	}
	body := map[string]any{
		"is_single_use_link": singleUse,
		"type":               linkType,
		"access_period":      days,
		"recipient_edrpou":   nilIfEmpty(edrpou),
		"recipient_email":    nilIfEmpty(email),
	}
	if emailText != "" {
		body["email_text"] = emailText
	}
	return body, nil
}

func nilIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func int64Text(v int64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	if v == 0 {
		return "0"
	}
	var buf [24]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
