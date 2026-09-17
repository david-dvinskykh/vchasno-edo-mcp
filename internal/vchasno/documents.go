package vchasno

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// DocumentFilter is the shared filter set of /documents (outgoing) and
// /incoming-documents. Only the fields the chosen endpoint documents are sent;
// Query and IncomingQuery decide which ones those are.
type DocumentFilter struct {
	// Both endpoints.
	DateDocumentFrom       string
	DateDocumentTo         string
	DateFinishedFrom       string
	DateFinishedTo         string
	Status                 string
	Extension              string
	Category               []int
	Cursor                 string
	IDs                    []string
	WithRecipients         *bool
	WithConnections        *bool
	WithDocumentFields     *bool
	WithVersions           *bool
	WithDeleteRequests     *bool
	WithAccessSettings     *bool
	WithTemplate           *bool
	TagID                  string
	NotTagged              *bool
	AmountEq               *int64
	AmountGte              *int64
	AmountLte              *int64
	Processed              *bool
	IsArchived             *bool
	IsPublicShared         *bool
	ReviewState            string
	DateReviewApprovedFrom string
	DateReviewApprovedTo   string
	SDStatus               string

	// Outgoing only.
	DateFrom         string
	DateTo           string
	DateRejectedFrom string
	DateRejectedTo   string
	RecipientEdrpou  string
	Number           string
	Vendor           string
	VendorID         string
	HasChanged       *bool
	IsDelivered      *bool
	IsInternal       *bool
	WithTags         *bool
	ChangedFrom      string
	ChangedTo        string

	// Incoming only.
	DateCreatedFrom string
	DateCreatedTo   string
	DateSentFrom    string
	DateSentTo      string
	EdrpouOwner     string
}

func (f DocumentFilter) shared(q *Values) *Values {
	return q.
		Str("date_document_from", f.DateDocumentFrom).
		Str("date_document_to", f.DateDocumentTo).
		Str("date_finished_from", f.DateFinishedFrom).
		Str("date_finished_to", f.DateFinishedTo).
		Str("status", f.Status).
		Str("extension", f.Extension).
		Ints("category", f.Category).
		Str("cursor", f.Cursor).
		Strs("ids", f.IDs).
		Bool("with_recipients", f.WithRecipients).
		Bool("with_connections", f.WithConnections).
		Bool("with_document_fields", f.WithDocumentFields).
		Bool("with_versions", f.WithVersions).
		Bool("with_delete_requests", f.WithDeleteRequests).
		Bool("with_access_settings", f.WithAccessSettings).
		Bool("with_template", f.WithTemplate).
		Str("tag_id", f.TagID).
		Bool("not_tagged", f.NotTagged).
		Bool("processed", f.Processed).
		Bool("is_archived", f.IsArchived).
		Bool("is_public_shared", f.IsPublicShared).
		Str("review_state", f.ReviewState).
		Str("date_review_approved_from", f.DateReviewApprovedFrom).
		Str("date_review_approved_to", f.DateReviewApprovedTo).
		Str("sd_status", f.SDStatus).
		money("amount_eq", f.AmountEq).
		money("amount_gte", f.AmountGte).
		money("amount_lte", f.AmountLte)
}

func (q *Values) money(key string, v *int64) *Values {
	if v != nil {
		q.v.Set(key, fmt.Sprintf("%d", *v))
	}
	return q
}

// ListDocuments returns outgoing documents (GET /api/v2/documents).
func (c *Client) ListDocuments(ctx context.Context, f DocumentFilter) (*DocumentList, error) {
	q := f.shared(Q()).
		Str("date_from", f.DateFrom).
		Str("date_to", f.DateTo).
		Str("date_rejected_from", f.DateRejectedFrom).
		Str("date_rejected_to", f.DateRejectedTo).
		Str("recipient_edrpou", f.RecipientEdrpou).
		Str("number", f.Number).
		Str("vendor", f.Vendor).
		Str("vendor_id", f.VendorID).
		Bool("has_changed", f.HasChanged).
		Bool("is_delivered", f.IsDelivered).
		Bool("is_internal", f.IsInternal).
		Bool("with_tags", f.WithTags).
		Str("changed_from", f.ChangedFrom).
		Str("changed_to", f.ChangedTo)
	var out DocumentList
	_, err := c.Get(ctx, "/api/v2/documents", q, &out)
	return &out, err
}

// ListIncomingDocuments returns incoming documents (GET /api/v2/incoming-documents).
func (c *Client) ListIncomingDocuments(ctx context.Context, f DocumentFilter) (*DocumentList, error) {
	q := f.shared(Q()).
		Str("date_created_from", f.DateCreatedFrom).
		Str("date_created_to", f.DateCreatedTo).
		Str("date_sent_from", f.DateSentFrom).
		Str("date_sent_to", f.DateSentTo).
		Str("edrpou_owner", f.EdrpouOwner)
	var out DocumentList
	_, err := c.Get(ctx, "/api/v2/incoming-documents", q, &out)
	return &out, err
}

// GetDocument reads one document by id. The same with_* flags as the listing
// endpoints apply.
func (c *Client) GetDocument(ctx context.Context, id string, f DocumentFilter) (*Document, error) {
	q := f.shared(Q())
	q.Bool("with_tags", f.WithTags)
	var doc Document
	resp, err := c.Get(ctx, "/api/v2/documents/"+url.PathEscape(id), q, &doc)
	if err != nil {
		return nil, err
	}
	// Some deployments answer the single-document route with a one-element list.
	if doc.ID == "" && resp != nil {
		var list DocumentList
		if jerr := resp.JSON(&list); jerr == nil && len(list.Documents) > 0 {
			return &list.Documents[0], nil
		}
	}
	return &doc, nil
}

// DocumentStatuses reads the status of up to 500 documents at once.
func (c *Client) DocumentStatuses(ctx context.Context, ids []string) (*StatusList, error) {
	var out StatusList
	_, err := c.Post(ctx, "/api/v2/documents/statuses", nil, map[string]any{"document_ids": ids}, &out)
	return &out, err
}

// UploadOptions are the query parameters of a document upload. Everything the
// file name can carry may also be passed here, which is what this server does:
// explicit parameters beat parsing a file name.
type UploadOptions struct {
	ExpectedOwnerSignatures     *int
	ExpectedRecipientSignatures *int
	FirstSignBy                 string
	SignerRoles                 []string
	SignerEmails                []string
	ParallelSigning             *bool
	IsInternal                  *bool
	IsMultilateral              *bool
	IsParallel                  *bool
	ShareTo                     []string
	ShareToGroups               []string
	ReviewersIDs                []string
	ReviewersEmails             []string
	ParallelReview              *bool
	IsRequiredReview            *bool
	Tags                        []string
	ParentID                    string
	Category                    *int
	Amount                      *int64
	Title                       string
	DocNumber                   string
	DateDocument                string
	RecipientEdrpou             string
	RecipientEmails             []string
	IsVersioned                 *bool
	TemplateID                  string
	VendorID                    string
	ShowRecipients              *bool
	WithAccessSettings          *bool
	AccessSettingsLevel         string
	DocumentPackageID           string
}

func (o UploadOptions) query() *Values {
	q := Q().
		Str("first_sign_by", o.FirstSignBy).
		Strs("signer_roles", o.SignerRoles).
		Strs("signer_emails", o.SignerEmails).
		Bool("parallel_signing", o.ParallelSigning).
		Bool("is_internal", o.IsInternal).
		Bool("is_multilateral", o.IsMultilateral).
		Bool("is_parallel", o.IsParallel).
		Strs("share_to", o.ShareTo).
		Strs("share_to_groups", o.ShareToGroups).
		Strs("reviewers_ids", o.ReviewersIDs).
		Strs("reviewers_emails", o.ReviewersEmails).
		Bool("parallel_review", o.ParallelReview).
		Bool("is_required_review", o.IsRequiredReview).
		Strs("tags", o.Tags).
		Str("parent_id", o.ParentID).
		Str("title", o.Title).
		Str("doc_number", o.DocNumber).
		Str("date_document", o.DateDocument).
		Str("recipient_edrpou", o.RecipientEdrpou).
		Bool("is_versioned", o.IsVersioned).
		Str("template_id", o.TemplateID).
		Str("vendor_id", o.VendorID).
		Bool("show_recipients", o.ShowRecipients).
		Bool("with_access_settings", o.WithAccessSettings).
		Str("access_settings_level", o.AccessSettingsLevel).
		Str("document_package_id", o.DocumentPackageID).
		money("amount", o.Amount)
	if o.ExpectedOwnerSignatures != nil {
		q.v.Set("expected_owner_signatures", fmt.Sprint(*o.ExpectedOwnerSignatures))
	}
	if o.ExpectedRecipientSignatures != nil {
		q.v.Set("expected_recipient_signatures", fmt.Sprint(*o.ExpectedRecipientSignatures))
	}
	if o.Category != nil {
		q.v.Set("category", fmt.Sprint(*o.Category))
	}
	if len(o.RecipientEmails) > 0 {
		q.v.Set("recipient_emails", strings.Join(o.RecipientEmails, ","))
	}
	return q
}

// UploadDocument sends one file (or a .zip of up to 500 files) to Vchasno.
func (c *Client) UploadDocument(ctx context.Context, filename string, content []byte, o UploadOptions) (*DocumentList, error) {
	var out DocumentList
	resp, err := c.PostMultipart(ctx, "/api/v2/documents", o.query(), []FilePart{{Field: "file", Filename: filename, Content: content}}, nil, &out)
	if err != nil {
		return nil, err
	}
	// A single-file upload may answer with a bare document object.
	if len(out.Documents) == 0 && resp != nil {
		var one Document
		if jerr := resp.JSON(&one); jerr == nil && one.ID != "" {
			out.Documents = []Document{one}
		}
	}
	return &out, nil
}

// DocumentInfo is the editable set of document attributes.
type DocumentInfo struct {
	Title    *string `json:"title,omitempty"`
	Date     *string `json:"date,omitempty"`
	Number   *string `json:"number,omitempty"`
	Category *int    `json:"category,omitempty"`
	Amount   *int64  `json:"amount,omitempty"`
}

// UpdateDocumentInfo edits the attributes of a document in status < 7003.
func (c *Client) UpdateDocumentInfo(ctx context.Context, id string, info DocumentInfo) (*Document, error) {
	var out Document
	_, err := c.Patch(ctx, "/api/v2/documents/"+url.PathEscape(id)+"/info", nil, info, &out)
	return &out, err
}

// SetRecipient replaces the counterparty of a bilateral document.
func (c *Client) SetRecipient(ctx context.Context, id, edrpou, email string) error {
	_, err := c.Patch(ctx, "/api/v2/documents/"+url.PathEscape(id)+"/recipient", nil, map[string]string{"edrpou": edrpou, "email": email}, nil)
	return err
}

// MultilateralRecipient is one party of a multilateral signing route.
type MultilateralRecipient struct {
	Edrpou        string   `json:"edrpou"`
	Emails        []string `json:"emails"`
	IsEmailHidden bool     `json:"is_email_hidden"`
	Role          string   `json:"role"`
}

// SetRecipients replaces the whole recipient list of a multilateral document.
func (c *Client) SetRecipients(ctx context.Context, id string, isOrdered bool, recipients []MultilateralRecipient) error {
	_, err := c.Patch(ctx, "/api/v2/documents/"+url.PathEscape(id)+"/recipients", nil,
		map[string]any{"is_ordered": isOrdered, "recipients": recipients}, nil)
	return err
}

// SetAccessLevel switches a document between company-wide and private access.
func (c *Client) SetAccessLevel(ctx context.Context, id, level string) error {
	_, err := c.Patch(ctx, "/api/v2/documents/"+url.PathEscape(id)+"/access-settings", nil, map[string]string{"level": level}, nil)
	return err
}

// SetViewers adds, removes or replaces the employees who may see a document.
func (c *Client) SetViewers(ctx context.Context, id, strategy string, groupIDs, roleIDs []string) error {
	body := map[string]any{"strategy": strategy, "groups_ids": nonNil(groupIDs), "roles_ids": nonNil(roleIDs)}
	_, err := c.Patch(ctx, "/api/v2/documents/"+url.PathEscape(id)+"/viewers-settings", nil, body, nil)
	return err
}

// FlowStep is one step of the multilateral signing route set by SetFlow.
type FlowStep struct {
	Edrpou  string   `json:"edrpou"`
	Emails  []string `json:"emails"`
	Order   int      `json:"order"`
	SignNum int      `json:"sign_num"`
}

// SetFlow assigns the signers of a multilateral document. The document moves
// to status 7010 immediately afterwards.
func (c *Client) SetFlow(ctx context.Context, id string, steps []FlowStep) error {
	_, err := c.Post(ctx, "/api/v2/documents/"+url.PathEscape(id)+"/flow", nil, steps, nil)
	return err
}

// SetSigners assigns roles and groups as signers of a document.
func (c *Client) SetSigners(ctx context.Context, id string, entities []Entity, isParallel bool) error {
	_, err := c.Post(ctx, "/api/v2/documents/"+url.PathEscape(id)+"/signers", nil,
		map[string]any{"signer_entities": entities, "is_parallel": isParallel}, nil)
	return err
}

// ListSignatures reads the signatures of a document with certificate details.
func (c *Client) ListSignatures(ctx context.Context, id string) ([]FullSignature, error) {
	var out []FullSignature
	_, err := c.Get(ctx, "/api/v2/documents/"+url.PathEscape(id)+"/signatures", nil, &out)
	return out, err
}

// ListFlows reads the multilateral signing route of a document.
func (c *Client) ListFlows(ctx context.Context, id string) ([]Flow, error) {
	var out []Flow
	_, err := c.Get(ctx, "/api/v2/documents/"+url.PathEscape(id)+"/flows", nil, &out)
	return out, err
}

// AddSignature attaches a ready detached signature (and optional stamp),
// both base64-encoded .p7s containers.
func (c *Client) AddSignature(ctx context.Context, id, signatureB64, stampB64 string) error {
	body := map[string]string{"signature": signatureB64}
	if stampB64 != "" {
		body["stamp"] = stampB64
	}
	_, err := c.Post(ctx, "/api/v2/documents/"+url.PathEscape(id)+"/signatures", nil, body, nil)
	return err
}

// SendDocument sends a document onwards after signing, or to the counterparty
// for the first signature when it sits in status 7001.
func (c *Client) SendDocument(ctx context.Context, id string) error {
	_, err := c.Post(ctx, "/api/v2/documents/"+url.PathEscape(id)+"/send", nil, nil, nil)
	return err
}

// RejectDocument rejects a document with a reason.
func (c *Client) RejectDocument(ctx context.Context, id, text string) error {
	_, err := c.Post(ctx, "/api/v2/documents/"+url.PathEscape(id)+"/reject", nil, map[string]string{"text": text}, nil)
	return err
}

// DeleteDocument deletes a document outright (owner company, delete permission).
func (c *Client) DeleteDocument(ctx context.Context, id string) error {
	_, err := c.Delete(ctx, "/api/v2/documents/"+url.PathEscape(id), nil, nil, nil)
	return err
}

// MarkProcessed flags up to 500 documents as processed by this company.
func (c *Client) MarkProcessed(ctx context.Context, ids []string) (*UpdatedIDs, error) {
	var out UpdatedIDs
	_, err := c.Post(ctx, "/api/v2/documents/mark-as-processed", nil, map[string]any{"document_ids": ids}, &out)
	return &out, err
}

// ── versions and child documents ────────────────────────────────

// UploadVersion adds a new version of a document.
func (c *Client) UploadVersion(ctx context.Context, id, filename string, content []byte) error {
	_, err := c.PostMultipart(ctx, "/api/v2/documents/"+url.PathEscape(id)+"/version", nil,
		[]FilePart{{Field: "file", Filename: filename, Content: content}}, nil, nil)
	return err
}

// DeleteVersion removes the last uploaded version of a document.
func (c *Client) DeleteVersion(ctx context.Context, id, versionID string) error {
	_, err := c.Delete(ctx, "/api/v2/documents/"+url.PathEscape(id)+"/version/"+url.PathEscape(versionID), nil, nil, nil)
	return err
}

// AttachChild links a child document to a parent one.
func (c *Client) AttachChild(ctx context.Context, parentID, childID string) error {
	_, err := c.Post(ctx, "/api/v2/documents/"+url.PathEscape(parentID)+"/child/"+url.PathEscape(childID), nil, nil, nil)
	return err
}

// DetachChild unlinks a child document from its parent.
func (c *Client) DetachChild(ctx context.Context, parentID, childID string) error {
	_, err := c.Delete(ctx, "/api/v2/documents/"+url.PathEscape(parentID)+"/child/"+url.PathEscape(childID), nil, nil, nil)
	return err
}

// ── downloads ───────────────────────────────────────────────────

// ArchiveOptions tune the ZIP download of a document with its signatures.
type ArchiveOptions struct {
	WithInstruction          *bool
	WithXMLPreview           *bool
	ConvertToSignatureFormat string
	FilenamesMaxLength       int
}

// DownloadOriginal fetches the original file; version may be a version id or "latest".
func (c *Client) DownloadOriginal(ctx context.Context, id, version string) (*Response, error) {
	return c.GetRaw(ctx, "/api/v2/documents/"+url.PathEscape(id)+"/original", Q().Str("version", version))
}

// DownloadArchive fetches a ZIP with the document and its signatures.
func (c *Client) DownloadArchive(ctx context.Context, id string, o ArchiveOptions) (*Response, error) {
	q := Q().Bool("with_instruction", o.WithInstruction).Bool("with_xml_preview", o.WithXMLPreview).
		Str("convert_to_signature_format", o.ConvertToSignatureFormat).Int("filenames_max_length", o.FilenamesMaxLength)
	return c.GetRaw(ctx, "/api/v2/documents/"+url.PathEscape(id)+"/archive", q)
}

// DownloadP7S fetches the internal_appended .p7s container.
func (c *Client) DownloadP7S(ctx context.Context, id string) (*Response, error) {
	return c.GetRaw(ctx, "/api/v2/documents/"+url.PathEscape(id)+"/p7s", nil)
}

// DownloadASIC fetches the ASiC container with European (ECDSA) signatures.
func (c *Client) DownloadASIC(ctx context.Context, id string) (*Response, error) {
	return c.GetRaw(ctx, "/api/v2/documents/"+url.PathEscape(id)+"/asic", nil)
}

// DownloadPDFPrint fetches the printable PDF rendering of a PDF document.
func (c *Client) DownloadPDFPrint(ctx context.Context, id, version string) (*Response, error) {
	return c.GetRaw(ctx, "/api/v2/documents/"+url.PathEscape(id)+"/pdf/print", Q().Str("version", version))
}

// RequestXMLToPDF asks Vchasno to render an XML document as PDF.
func (c *Client) RequestXMLToPDF(ctx context.Context, id string, force bool) error {
	_, err := c.Post(ctx, "/api/v2/documents/"+url.PathEscape(id)+"/xml-to-pdf", nil, map[string]bool{"force": force}, nil)
	return err
}

// DownloadXMLToPDF fetches the rendered PDF of an XML document.
func (c *Client) DownloadXMLToPDF(ctx context.Context, id string) (*Response, error) {
	return c.GetRaw(ctx, "/api/v2/documents/"+url.PathEscape(id)+"/xml-to-pdf", nil)
}

// DownloadLinks returns ready-made download URLs for a list of documents.
func (c *Client) DownloadLinks(ctx context.Context, ids []string) (*DownloadBatch, error) {
	var out DownloadBatch
	_, err := c.Get(ctx, "/api/v2/download-documents", Q().Strs("ids", ids), &out)
	return &out, err
}

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}
