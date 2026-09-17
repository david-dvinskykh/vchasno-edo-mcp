package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/vchasno"
)

// Getting bytes in and out of Vchasno: uploading a document (or a zip of up
// to 500), adding versions, and every download format the service offers.

type uploadIn struct {
	fileInput

	RecipientEdrpou string   `json:"recipient_edrpou,omitempty" jsonschema:"ЄДРПОУ/ІПН of the counterparty. Without it the document stays in status 7000 (uploaded) and cannot be sent"`
	RecipientEmails []string `json:"recipient_emails,omitempty" jsonschema:"Email(s) of the counterparty. With an email the document goes straight to status 7001 (ready to send)"`
	Title           string   `json:"title,omitempty" jsonschema:"Document title shown in Vchasno"`
	Number          string   `json:"number,omitempty" jsonschema:"External document number"`
	DateDocument    string   `json:"date_document,omitempty" jsonschema:"Date the document was drawn up (YYYY-MM-DD)"`
	Category        string   `json:"category,omitempty" jsonschema:"Document type: numeric id or its title (Рахунок, Акт наданих послуг, Договір, Видаткова накладна …)"`
	Amount          string   `json:"amount,omitempty" jsonschema:"Document amount in hryvnias, e.g. 12500.00"`
	VendorID        string   `json:"vendor_id,omitempty" jsonschema:"Your own system's id for this document, so later syncs can match it"`

	ExpectedOwnerSignatures     int    `json:"expected_owner_signatures,omitempty" jsonschema:"How many signatures your company must put on it (default 1)"`
	ExpectedRecipientSignatures int    `json:"expected_recipient_signatures,omitempty" jsonschema:"How many signatures the counterparty must put on it (default 1). 0 for documents the counterparty only receives"`
	FirstSignBy                 string `json:"first_sign_by,omitempty" jsonschema:"Who signs first: owner (default) or recipient"`
	ParallelSigning             string `json:"parallel_signing,omitempty" jsonschema:"true (default) = your signers may sign in any order, false = strictly in the order given"`
	IsInternal                  string `json:"is_internal,omitempty" jsonschema:"true = internal document of your own company; no counterparty needed"`
	IsMultilateral              string `json:"is_multilateral,omitempty" jsonschema:"true = multilateral document; set the route afterwards with set_multilateral_route. Cannot be combined with the owner/recipient signature counts"`
	IsParallel                  string `json:"is_parallel,omitempty" jsonschema:"For multilateral documents: true (default) = parallel signing, false = in turn"`
	IsVersioned                 string `json:"is_versioned,omitempty" jsonschema:"true = the document accepts further versions"`

	SignerEmails    []string `json:"signer_emails,omitempty" jsonschema:"Employees of your company who must sign, by email. Use either this or signer_roles, not both"`
	SignerRoles     []string `json:"signer_roles,omitempty" jsonschema:"The same, by role id (list_roles). Emails are resolved to role ids automatically, so signer_emails is usually enough"`
	ReviewersEmails []string `json:"reviewers_emails,omitempty" jsonschema:"Employees who must approve the document internally before it can be signed"`
	ParallelReview  string   `json:"parallel_review,omitempty" jsonschema:"true (default) = approvers work in parallel, false = in turn"`
	RequiredReview  string   `json:"is_required_review,omitempty" jsonschema:"true = signing and rejection are blocked until the internal approval is finished"`

	ShareWith      []string `json:"share_with,omitempty" jsonschema:"Employees (emails or role ids) who get read access to this document"`
	ShareWithTeams []string `json:"share_with_teams,omitempty" jsonschema:"Teams (names or ids) that get read access"`
	Tags           []string `json:"tags,omitempty" jsonschema:"Labels to put on the document, by name or id"`
	ParentID       string   `json:"parent_id,omitempty" jsonschema:"Attach this document as a child of that one"`
	ScenarioID     string   `json:"scenario_id,omitempty" jsonschema:"Apply a company scenario (list_scenarios) that fills approvers, signers, viewers, labels and extra parameters"`
	AccessLevel    string   `json:"access_level,omitempty" jsonschema:"extended (default, visible company-wide) or private"`
}

type uploadVersionIn struct {
	fileInput
	DocumentID string `json:"document_id" jsonschema:"Document that gets the new version"`
}

type deleteVersionIn struct {
	DocumentID string `json:"document_id" jsonschema:"Document id"`
	VersionID  string `json:"version_id" jsonschema:"Version id to remove (the last uploaded one)"`
	Confirm    bool   `json:"confirm,omitempty" jsonschema:"Must be true: removing a version cannot be undone"`
}

type downloadIn struct {
	ID         string `json:"id" jsonschema:"Document id"`
	Format     string `json:"format,omitempty" jsonschema:"original (default) = the file as uploaded; archive = ZIP with the document, every signature and the verification instruction; p7s = one internal_appended container; asic = ASiC container with European (ECDSA) signatures; pdf = printable rendering of a PDF; xml_pdf = PDF rendering of an XML document"`
	Version    string `json:"version,omitempty" jsonschema:"Version id, or 'latest'. Applies to format=original and format=pdf"`
	InlineText bool   `json:"inline_text,omitempty" jsonschema:"For textual formats only (XML, JSON): also put the content in the answer when it is under 64 KiB. Binary files are never inlined — use the returned url"`

	WithInstruction *bool  `json:"with_instruction,omitempty" jsonschema:"For the archive format: include the PDF instruction on verifying signatures (default true)"`
	WithXMLPreview  *bool  `json:"with_xml_preview,omitempty" jsonschema:"For the archive format: include the printable form of an XML document (default true)"`
	ConvertTo       string `json:"convert_signatures_to,omitempty" jsonschema:"For the archive format: convert signatures on the fly. Only internal_appended is supported"`
	FilenamesMax    int    `json:"filenames_max_length,omitempty" jsonschema:"For the archive format: trim file names inside the ZIP to this many characters (10…255)"`
	Force           bool   `json:"force,omitempty" jsonschema:"For the xml_pdf format: re-render the PDF instead of reusing the stored one (needed after the document changed status)"`
}

type downloadLinksIn struct {
	IDs    []string `json:"ids" jsonschema:"Document ids"`
	IDsCSV string   `json:"ids_csv,omitempty" jsonschema:"Alternative to ids: the same list comma-separated"`
}

func (d *Deps) registerFiles(srv *mcp.Server) {
	addRead(srv, "download_document", "Завантажити документ",
		"Download a document in any format Vchasno offers: the original file, the ZIP with all signatures, the .p7s container, the ASiC container with European signatures, or a printable PDF. The answer is a direct download URL plus the size, content type and SHA-256 — the bytes never travel through the conversation. Fetch the url yourself; it needs no authentication and expires, so use it promptly rather than storing it.",
		func(ctx context.Context, _ *mcp.CallToolRequest, in downloadIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if strings.TrimSpace(in.ID) == "" {
				return failf("id is required")
			}
			api := d.api(ctx)
			format := strings.ToLower(strings.TrimSpace(in.Format))
			if format == "" {
				format = "original"
			}
			var (
				resp *vchasno.Response
				err  error
				name string
			)
			switch format {
			case "original":
				resp, err = api.DownloadOriginal(ctx, in.ID, in.Version)
				name = in.ID
			case "archive", "zip":
				resp, err = api.DownloadArchive(ctx, in.ID, vchasno.ArchiveOptions{
					WithInstruction: in.WithInstruction, WithXMLPreview: in.WithXMLPreview,
					ConvertToSignatureFormat: in.ConvertTo, FilenamesMaxLength: in.FilenamesMax})
				name = in.ID + ".zip"
			case "p7s":
				resp, err = api.DownloadP7S(ctx, in.ID)
				name = in.ID + ".p7s"
			case "asic":
				resp, err = api.DownloadASIC(ctx, in.ID)
				name = in.ID + ".asice"
			case "pdf", "pdf_print", "print":
				resp, err = api.DownloadPDFPrint(ctx, in.ID, in.Version)
				name = in.ID + ".pdf"
			case "xml_pdf", "xml-to-pdf":
				if rerr := api.RequestXMLToPDF(ctx, in.ID, in.Force); rerr != nil {
					return fail(fmt.Errorf("requesting the PDF rendering: %w", rerr))
				}
				resp, err = api.DownloadXMLToPDF(ctx, in.ID)
				name = in.ID + ".pdf"
			default:
				return failf("unknown format %q: use original, archive, p7s, asic, pdf or xml_pdf", in.Format)
			}
			if err != nil {
				return fail(err)
			}
			saved, err := d.saveDownload(resp, name, in.InlineText)
			if err != nil {
				return fail(err)
			}
			return ok(map[string]any{"document_id": in.ID, "format": format, "file": saved})
		})

	addRead(srv, "get_download_links", "Посилання на завантаження",
		"Ready-made download URLs for a list of documents: the original, the signed archive and, for XML documents, the generated PDF. Cheaper than downloading when the files only need to be handed to somebody else. 'pending' in the answer means Vchasno is still preparing them — ask again shortly.",
		func(ctx context.Context, _ *mcp.CallToolRequest, in downloadLinksIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			ids := mergeIDs(in.IDs, in.IDsCSV)
			if len(ids) == 0 {
				return failf("pass at least one document id")
			}
			batch, err := d.api(ctx).DownloadLinks(ctx, ids)
			if err != nil {
				return fail(err)
			}
			return ok(batch)
		})

	if d.Cfg.ReadOnly {
		return
	}

	mcp.AddTool(srv, &mcp.Tool{Name: "upload_document", Annotations: writes("Завантажити документ"),
		Description: "Upload a document into Vchasno — the first step of every outgoing flow. Pass the file by path or as base64, plus whatever metadata you have: counterparty ЄДРПОУ and email, title, number, date, type and amount. With a counterparty email the document lands in status 7001 (ready to send); without one it stays 7000 and set_document_recipient fills the gap. Signers, approvers, viewers and labels can all be set here in the same call, or a company scenario can do it via scenario_id. A .zip may carry up to 500 documents at once."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in uploadIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			name, content, err := d.readFile(in.fileInput)
			if err != nil {
				return fail(err)
			}
			o := vchasno.UploadOptions{
				RecipientEdrpou: in.RecipientEdrpou, RecipientEmails: in.RecipientEmails,
				Title: in.Title, DocNumber: in.Number, DateDocument: in.DateDocument,
				VendorID: in.VendorID, ParentID: in.ParentID, TemplateID: in.ScenarioID,
				AccessSettingsLevel:         in.AccessLevel,
				ExpectedOwnerSignatures:     intPtr(in.ExpectedOwnerSignatures),
				ExpectedRecipientSignatures: intPtr(in.ExpectedRecipientSignatures),
				FirstSignBy:                 in.FirstSignBy,
				ReviewersEmails:             in.ReviewersEmails,
			}
			if o.Amount, err = amountPtr(in.Amount); err != nil {
				return fail(err)
			}
			if in.Category != "" {
				if n, cerr := atoiSafe(in.Category); cerr == nil {
					o.Category = &n
				} else if id, found := d.Sess.ResolveCategoryID(ctx, in.Category); found {
					o.Category = &id
				} else {
					return failf("unknown document type %q%s; call list_document_categories", in.Category, d.categoryHint(ctx, in.Category))
				}
			}
			for _, pair := range []struct {
				raw string
				dst **bool
				arg string
			}{
				{in.ParallelSigning, &o.ParallelSigning, "parallel_signing"},
				{in.IsInternal, &o.IsInternal, "is_internal"},
				{in.IsMultilateral, &o.IsMultilateral, "is_multilateral"},
				{in.IsParallel, &o.IsParallel, "is_parallel"},
				{in.IsVersioned, &o.IsVersioned, "is_versioned"},
				{in.ParallelReview, &o.ParallelReview, "parallel_review"},
				{in.RequiredReview, &o.IsRequiredReview, "is_required_review"},
			} {
				v, perr := boolPtr(pair.raw)
				if perr != nil {
					return failf("%s: %v", pair.arg, perr)
				}
				*pair.dst = v
			}
			// Vchasno wants signers either as role ids or as emails, never both.
			switch {
			case len(in.SignerRoles) > 0:
				roles, rerr := d.Sess.ResolveRoleIDs(ctx, in.SignerRoles)
				if rerr != nil {
					return fail(rerr)
				}
				o.SignerRoles = roles
			case len(in.SignerEmails) > 0:
				o.SignerEmails = in.SignerEmails
			}
			if len(in.ShareWith) > 0 {
				roles, rerr := d.Sess.ResolveRoleIDs(ctx, in.ShareWith)
				if rerr != nil {
					return fail(rerr)
				}
				o.ShareTo = roles
			}
			for _, team := range in.ShareWithTeams {
				id, gerr := d.Sess.ResolveGroupID(ctx, team)
				if gerr != nil {
					return fail(gerr)
				}
				o.ShareToGroups = append(o.ShareToGroups, id)
			}
			if len(in.Tags) > 0 {
				tags, terr := d.Sess.ResolveTagIDs(ctx, in.Tags)
				if terr != nil {
					return fail(terr)
				}
				o.Tags = tags
			}
			t := true
			o.ShowRecipients = &t
			list, err := d.api(ctx).UploadDocument(ctx, name, content, o)
			if err != nil {
				return fail(err)
			}
			d.enrich(ctx, list.Documents)
			next := "The document is uploaded. Next: set_document_recipient if the counterparty is still missing, then add_signature (or a cloud_sign_* flow), then send_document."
			if len(list.Documents) == 1 && list.Documents[0].Status == 7001 {
				next = "Status 7001: the document is ready. Sign it (add_signature or cloud_sign_document) and call send_document."
			}
			return ok(map[string]any{"uploaded": len(list.Documents), "file": name, "size_bytes": len(content), "next_step": next, "documents": list.Documents})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "upload_document_version", Annotations: writes("Завантажити версію"),
		Description: "Add a new version of an existing document (.txt, .doc, .docx, .xls, .xlsx, .pdf). Versions are listed by get_document with with=[\"versions\"] and downloaded by download_document with a version id. Note: the service accepts the upload for any document but only records a version on documents that support versioning, so read the versions back after uploading instead of assuming it stuck."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in uploadVersionIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if strings.TrimSpace(in.DocumentID) == "" {
				return failf("document_id is required")
			}
			name, content, err := d.readFile(in.fileInput)
			if err != nil {
				return fail(err)
			}
			if err := d.api(ctx).UploadVersion(ctx, in.DocumentID, name, content); err != nil {
				return fail(err)
			}
			// The endpoint answers 200/201 even when it records nothing, so
			// report what the document actually holds now rather than the
			// upload's own optimism.
			out := map[string]any{"document_id": in.DocumentID, "file": name, "size_bytes": len(content)}
			var f vchasno.DocumentFilter
			applyWith(&f, []string{"versions"})
			if doc, derr := d.api(ctx).GetDocument(ctx, in.DocumentID, f); derr == nil {
				out["versions_now"] = len(doc.Versions)
				if len(doc.Versions) == 0 {
					out["warning"] = "the upload was accepted but the document reports no versions; this document does not support versioning"
				}
			}
			return done("version uploaded", out)
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "delete_document_version", Annotations: destructive("Видалити версію"),
		Description: "Remove the last uploaded version of a document. Irreversible — requires confirm=true."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in deleteVersionIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if err := confirmed(in.Confirm, "deleting a document version"); err != nil {
				return fail(err)
			}
			if err := d.api(ctx).DeleteVersion(ctx, in.DocumentID, in.VersionID); err != nil {
				return fail(err)
			}
			return done("version deleted", map[string]any{"document_id": in.DocumentID, "version_id": in.VersionID})
		})
}

// decodeStatusPayload tells a structured-data download that is actually a
// progress report apart from one that is the data itself.
func decodeStatusPayload(body []byte) (*vchasno.Extraction, bool) {
	var probe vchasno.Extraction
	if err := json.Unmarshal(body, &probe); err != nil {
		return nil, false
	}
	if probe.DocumentID == "" || probe.Status == "" {
		return nil, false
	}
	return &probe, true
}
