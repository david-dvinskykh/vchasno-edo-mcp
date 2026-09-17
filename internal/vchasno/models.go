package vchasno

// Types mirror the JSON of the Vchasno.EDO API. Fields the service may omit
// are pointers or `omitempty` so that a tool answer never invents a value the
// API did not send.

// Document is one document, as returned by /documents and /incoming-documents.
// The two endpoints share most of the shape; the fields only one of them fills
// (edrpou_owner, company_name for incoming) are simply absent in the other.
type Document struct {
	ID                 string          `json:"id"`
	Vendor             string          `json:"vendor,omitempty"`
	VendorID           *string         `json:"vendor_id,omitempty"`
	Status             int             `json:"status"`
	StatusText         string          `json:"status_text,omitempty"`
	StatusMeaning      string          `json:"status_meaning,omitempty"` // added by this server, not by the API
	SignaturesToFinish int             `json:"signatures_to_finish,omitempty"`
	FirstSignBy        string          `json:"first_sign_by,omitempty"`
	Extension          string          `json:"extension,omitempty"`
	Signatures         []Signature     `json:"signatures,omitempty"`
	Title              string          `json:"title,omitempty"`
	Type               *string         `json:"type,omitempty"`
	Amount             *int64          `json:"amount,omitempty"`
	AmountUAH          *string         `json:"amount_uah,omitempty"` // added by this server: amount is in kopiykas
	Date               *string         `json:"date,omitempty"`
	DateCreated        string          `json:"date_created,omitempty"`
	DateFinished       *string         `json:"date_finished,omitempty"`
	DateDelivered      *string         `json:"date_delivered,omitempty"`
	Number             *string         `json:"number,omitempty"`
	PreviewURL         *string         `json:"preview_url,omitempty"`
	Processed          *bool           `json:"processed,omitempty"`
	URL                string          `json:"url,omitempty"`
	IsMultilateral     *bool           `json:"is_multilateral,omitempty"`
	Category           *int            `json:"category,omitempty"`
	CategoryTitle      string          `json:"category_title,omitempty"` // added by this server
	CategoryDetails    any             `json:"category_details,omitempty"`
	IsDelivered        *bool           `json:"is_delivered,omitempty"`
	IsArchived         *bool           `json:"is_archived,omitempty"`
	IsInternal         *bool           `json:"is_internal,omitempty"`
	IsPublicShared     *bool           `json:"is_public_shared,omitempty"`
	Template           any             `json:"template,omitempty"`
	SDStatus           string          `json:"sd_status,omitempty"`
	EdrpouOwner        string          `json:"edrpou_owner,omitempty"`
	CompanyName        string          `json:"company_name,omitempty"`
	Parent             *string         `json:"parent,omitempty"`
	Children           []string        `json:"children,omitempty"`
	Recipients         []Recipient     `json:"recipients,omitempty"`
	Fields             []DocumentField `json:"fields,omitempty"`
	Versions           []Version       `json:"versions,omitempty"`
	Tags               []Tag           `json:"tags,omitempty"`
	DeleteRequests     []DeleteRequest `json:"delete_requests,omitempty"`
	AccessSettings     any             `json:"access_settings,omitempty"`
	ReviewState        string          `json:"review_state,omitempty"`
}

// Signature is a short signature record inside a document listing.
type Signature struct {
	ID          string `json:"id,omitempty"`
	Email       string `json:"email,omitempty"`
	DateCreated string `json:"date_created,omitempty"`
}

// Recipient is a party of a document (with_recipients=1).
type Recipient struct {
	Edrpou         string   `json:"edrpou,omitempty"`
	Emails         []string `json:"emails,omitempty"`
	Name           string   `json:"name,omitempty"`
	IsEmailsHidden *bool    `json:"is_emails_hidden,omitempty"`
	Role           string   `json:"role,omitempty"`
}

// Version is an uploaded version of a document (with_versions=1).
type Version struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	RoleID      string `json:"role_id,omitempty"`
	DateCreated string `json:"date_created,omitempty"`
	IsSent      *bool  `json:"is_sent,omitempty"`
	Extension   string `json:"extension,omitempty"`
}

// DocumentList is the paginated answer of /documents and /incoming-documents.
type DocumentList struct {
	Documents  []Document `json:"documents"`
	NextCursor *string    `json:"next_cursor"`
}

// StatusItem is one row of /documents/statuses.
type StatusItem struct {
	DocumentID string `json:"document_id"`
	StatusID   int    `json:"status_id"`
	StatusText string `json:"status_text"`
	Meaning    string `json:"meaning,omitempty"` // added by this server
}

// StatusList wraps /documents/statuses.
type StatusList struct {
	DataList []StatusItem `json:"data_list"`
}

// UpdatedIDs is the answer of the bulk mark/lock endpoints.
type UpdatedIDs struct {
	UpdatedIDs []string `json:"updated_ids"`
}

// DownloadLink is one row of /download-documents.
type DownloadLink struct {
	ID          string     `json:"id"`
	Extension   string     `json:"extension,omitempty"`
	ArchiveURL  string     `json:"archive_url,omitempty"`
	OriginalURL string     `json:"original_url,omitempty"`
	Status      FlexString `json:"status,omitempty"`
	XMLToPDFURL *string    `json:"xml_to_pdf_url,omitempty"`
}

// DownloadBatch is the answer of /download-documents. The service sends
// `status` as text and `ready` / `pending` as 1 / 0, which is not what its
// documentation says, so both are decoded leniently.
type DownloadBatch struct {
	Status    FlexString     `json:"status"`
	Ready     FlexBool       `json:"ready"`
	Pending   FlexBool       `json:"pending"`
	Total     int            `json:"total"`
	Documents []DownloadLink `json:"documents"`
}

// Tag is a company label.
type Tag struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DateCreated string `json:"date_created,omitempty"`
}

// TagList wraps /tags.
type TagList struct {
	Tags []Tag `json:"tags"`
}

// TagRole binds a label to an employee role.
type TagRole struct {
	RoleID      string `json:"role_id"`
	TagID       string `json:"tag_id"`
	AssignerID  string `json:"assigner_id,omitempty"`
	DateCreated string `json:"date_created,omitempty"`
}

// TagRoleList wraps /tags/{id}/roles.
type TagRoleList struct {
	Roles []TagRole `json:"roles"`
}

// Comment is one comment or rejection reason of the company feed.
type Comment struct {
	ID          string  `json:"id"`
	Text        string  `json:"text"`
	DocumentID  string  `json:"document_id,omitempty"`
	DateCreated string  `json:"date_created,omitempty"`
	Email       string  `json:"email,omitempty"`
	Edrpou      string  `json:"edrpou,omitempty"`
	IsInternal  *bool   `json:"is_internal,omitempty"`
	Type        string  `json:"type,omitempty"`
	Author      *Author `json:"author,omitempty"`
}

// Author is the person behind a per-document comment.
type Author struct {
	FirstName  string `json:"first_name,omitempty"`
	SecondName string `json:"second_name,omitempty"`
	LastName   string `json:"last_name,omitempty"`
	Email      string `json:"email,omitempty"`
	Edrpou     string `json:"edrpou,omitempty"`
	IsLegal    *bool  `json:"is_legal,omitempty"`
}

// CommentList is the paginated company comment feed.
type CommentList struct {
	Comments   []Comment `json:"comments"`
	NextCursor *string   `json:"next_cursor"`
}

// ReviewAction is one entry of the internal approval history.
type ReviewAction struct {
	UserEmail   string `json:"user_email,omitempty"`
	GroupName   string `json:"group_name,omitempty"`
	IsLast      *bool  `json:"is_last,omitempty"`
	Action      string `json:"action,omitempty"`
	DateCreated string `json:"date_created,omitempty"`
}

// ReviewRequest is one approval assignment.
type ReviewRequest struct {
	UserFromEmail string `json:"user_from_email,omitempty"`
	UserToEmail   string `json:"user_to_email,omitempty"`
	GroupToName   string `json:"group_to_name,omitempty"`
	Status        string `json:"status,omitempty"`
	DateCreated   string `json:"date_created,omitempty"`
}

// ReviewStatus is the overall approval state of a document.
type ReviewStatus struct {
	Status      string `json:"status,omitempty"`
	IsRequired  *bool  `json:"is_required,omitempty"`
	DateCreated string `json:"date_created,omitempty"`
	DateUpdated string `json:"date_updated,omitempty"`
}

// Field is a company-level extra document parameter.
type Field struct {
	ID         string `json:"id"`
	Order      any    `json:"order,omitempty"`
	CompanyID  string `json:"company_id,omitempty"`
	Name       string `json:"name"`
	Type       string `json:"type,omitempty"`
	IsRequired *bool  `json:"is_required,omitempty"`
	CreatedBy  string `json:"created_by,omitempty"`
}

// DocumentField is an extra parameter with its value on one document.
type DocumentField struct {
	FieldID     string `json:"field_id"`
	Name        string `json:"name,omitempty"`
	Type        string `json:"type,omitempty"`
	IsRequired  *bool  `json:"is_required,omitempty"`
	Value       string `json:"value,omitempty"`
	DateUpdated string `json:"date_updated,omitempty"`
	DateCreated string `json:"date_created,omitempty"`
}

// FullSignature is one signature with its certificate details.
type FullSignature struct {
	ID             string `json:"id"`
	Edrpou         string `json:"edrpou,omitempty"`
	CompanyName    string `json:"company_name,omitempty"`
	IsInternal     *bool  `json:"is_internal,omitempty"`
	RoleID         string `json:"role_id,omitempty"`
	SignerName     string `json:"signer_name,omitempty"`
	SignerPosition string `json:"signer_position,omitempty"`
	SerialNumber   string `json:"serial_number,omitempty"`
	Timestamp      string `json:"timestamp,omitempty"`
	HasStamp       *bool  `json:"has_stamp,omitempty"`
	Stamp          *Stamp `json:"stamp,omitempty"`
	IsECDSA        *bool  `json:"is_ecdsa,omitempty"`
}

// Stamp is the company seal attached to a signature.
type Stamp struct {
	ACSK           string `json:"acsk,omitempty"`
	CompanyName    string `json:"company_name,omitempty"`
	Edrpou         string `json:"edrpou,omitempty"`
	PowerType      string `json:"power_type,omitempty"`
	SerialNumber   string `json:"serial_number,omitempty"`
	SignerName     string `json:"signer_name,omitempty"`
	SignerPosition string `json:"signer_position,omitempty"`
	Timestamp      string `json:"timestamp,omitempty"`
}

// Flow is one step of a multilateral signing route.
type Flow struct {
	Edrpou            string   `json:"edrpou"`
	Order             *int     `json:"order,omitempty"`
	PendingSignatures *int     `json:"pending_signatures,omitempty"`
	Emails            []string `json:"emails,omitempty"`
}

// DeleteRequest is a request to delete a document agreed with the other party.
type DeleteRequest struct {
	ID              string  `json:"id"`
	DocumentID      string  `json:"document_id,omitempty"`
	Message         string  `json:"message,omitempty"`
	InitiatorRoleID string  `json:"initiator_role_id,omitempty"`
	RejectMessage   *string `json:"reject_message,omitempty"`
	ReceiverEdrpou  string  `json:"receiver_edrpou,omitempty"`
	Status          string  `json:"status,omitempty"`
	DateCreated     string  `json:"date_created,omitempty"`
	DateAccepted    *string `json:"date_accepted,omitempty"`
	DateRejected    *string `json:"date_rejected,omitempty"`
	Cursor          string  `json:"cursor,omitempty"`
}

// Extraction is one structured-data recognition run.
type Extraction struct {
	DocumentID    string  `json:"document_id"`
	VersionID     *string `json:"version_id,omitempty"`
	Status        string  `json:"status,omitempty"`
	DateUpdated   *string `json:"date_updated,omitempty"`
	ErrorMessage  *string `json:"error_message,omitempty"`
	SkippedReason *string `json:"skipped_reason,omitempty"`
}

// ExtractionList is the paginated list of recognition runs.
type ExtractionList struct {
	Data       []Extraction `json:"data"`
	NextCursor *string      `json:"next_cursor"`
}

// Directory is an archive folder.
type Directory struct {
	ID          any    `json:"id"`
	ParentID    any    `json:"parent_id"`
	Name        string `json:"name"`
	DateCreated string `json:"date_created,omitempty"`
}

// DirectoryList is the paginated archive folder listing.
type DirectoryList struct {
	Directories []Directory `json:"directories"`
	NextCursor  *string     `json:"next_cursor"`
}

// ImportedSigned is the answer of the signed-document archive import.
type ImportedSigned struct {
	DocumentID          string `json:"document_id"`
	SignatureCount      int    `json:"signature_count"`
	CounterpartyCount   int    `json:"counterparty_count"`
	HasPDFVisualization bool   `json:"has_pdf_visualization"`
	ApplyVchasnoStamps  bool   `json:"apply_vchasno_stamps"`
}

// Category is a document type of the company.
type Category struct {
	CategoryID    int    `json:"category_id"`
	CategoryTitle string `json:"category_title"`
	IsPublic      *bool  `json:"is_public,omitempty"`
	DateCreated   string `json:"date_created,omitempty"`
	DateUpdated   string `json:"date_updated,omitempty"`
}

// Group is a team of employees.
type Group struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	CreatedBy   string `json:"created_by,omitempty"`
	DateCreated string `json:"date_created,omitempty"`
	DateUpdated string `json:"date_updated,omitempty"`
}

// GroupMember binds an employee role to a team.
type GroupMember struct {
	ID          string `json:"id"`
	RoleID      string `json:"role_id"`
	GroupID     string `json:"group_id"`
	CreatedBy   string `json:"created_by,omitempty"`
	DateCreated string `json:"date_created,omitempty"`
}

// Entity is a role or a group referenced by a scenario or a signing route.
type Entity struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// Scenario is a company template of the approval/signing/tagging route.
type Scenario struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	ReviewSettings  any    `json:"review_settings,omitempty"`
	SignersSettings any    `json:"signers_settings,omitempty"`
	ViewersSettings any    `json:"viewers_settings,omitempty"`
	FieldsSettings  any    `json:"fields_settings,omitempty"`
	TagsSettings    any    `json:"tags_settings,omitempty"`
	CreatedBy       string `json:"created_by,omitempty"`
	DateCreated     string `json:"date_created,omitempty"`
	DateUpdated     string `json:"date_updated,omitempty"`
}

// DocumentTemplate is a file template (PDF form or DOCX placeholders).
type DocumentTemplate struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Extension   string          `json:"extension,omitempty"`
	SharingType string          `json:"sharing_type,omitempty"`
	Fields      []TemplateField `json:"fields,omitempty"`
}

// TemplateField is one fillable field of a document template.
type TemplateField struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description *string  `json:"description,omitempty"`
	Required    *bool    `json:"required,omitempty"`
	Type        string   `json:"type,omitempty"`
	Options     []string `json:"options,omitempty"`
}

// TemplateList is the paginated template listing.
type TemplateList struct {
	Templates []DocumentTemplate `json:"templates"`
	Cursor    *int               `json:"cursor"`
}

// Role is an employee account inside the company.
type Role struct {
	ID          string `json:"id"`
	Status      string `json:"status,omitempty"`
	DateCreated string `json:"date_created,omitempty"`
	Email       string `json:"email,omitempty"`
	Position    string `json:"position,omitempty"`
}

// RoleList wraps /roles.
type RoleList struct {
	Roles []Role `json:"roles"`
}

// SharedLink is a public link to a document.
type SharedLink struct {
	ID              string  `json:"id"`
	DocumentID      string  `json:"document_id"`
	IsSingleUseLink *bool   `json:"is_single_use_link,omitempty"`
	IsActive        *bool   `json:"is_active,omitempty"`
	Type            string  `json:"type,omitempty"`
	AccessPeriod    *int    `json:"access_period,omitempty"`
	RecipientEdrpou *string `json:"recipient_edrpou,omitempty"`
	RecipientEmail  *string `json:"recipient_email,omitempty"`
	DateCreated     string  `json:"date_created,omitempty"`
	DateUpdated     string  `json:"date_updated,omitempty"`
	DateExpired     string  `json:"date_expired,omitempty"`
	Link            *string `json:"link,omitempty"`
}

// SignSession is a view/sign session for the personal cabinet integration.
type SignSession struct {
	ID             string  `json:"id"`
	CreatedBy      string  `json:"created_by,omitempty"`
	DocumentID     string  `json:"document_id,omitempty"`
	DocumentStatus *string `json:"document_status,omitempty"`
	Edrpou         string  `json:"edrpou,omitempty"`
	Email          string  `json:"email,omitempty"`
	IsLegal        *bool   `json:"is_legal,omitempty"`
	OnCancelURL    string  `json:"on_cancel_url,omitempty"`
	OnFinishURL    string  `json:"on_finish_url,omitempty"`
	RoleID         string  `json:"role_id,omitempty"`
	Status         string  `json:"status,omitempty"`
	Type           string  `json:"type,omitempty"`
	URL            string  `json:"url,omitempty"`
	Vendor         string  `json:"vendor,omitempty"`
}

// CloudSession is a Vchasno.KEP cloud-key signing session.
type CloudSession struct {
	AuthSessionID  string `json:"authSessionId,omitempty"`
	IsMobileLogged *bool  `json:"isMobileLogged,omitempty"`
	Status         string `json:"status,omitempty"`
	Token          string `json:"token,omitempty"`
	AccessToken    string `json:"accessToken,omitempty"`
	RefreshToken   string `json:"refreshToken,omitempty"`
	ExpiresIn      int    `json:"expiresIn,omitempty"`
}

// ReportRequest is the id of a queued action-history report.
type ReportRequest struct {
	ReportID string `json:"report_id"`
}

// ReportStatus is the state of a queued report.
type ReportStatus struct {
	Status   string `json:"status"`
	Filename string `json:"filename,omitempty"`
}

// CompanyCheck is the registration state of a counterparty.
type CompanyCheck struct {
	Edrpou       string `json:"edrpou"`
	Name         string `json:"name,omitempty"`
	IsRegistered bool   `json:"is_registered"`
}

// CompanyCheckBatch is the answer of the file-based counterparty check.
type CompanyCheckBatch struct {
	Companies         []CompanyCheck `json:"companies"`
	Percentage        string         `json:"percentage,omitempty"`
	InvalidRowNumbers []any          `json:"invalid_row_numbers,omitempty"`
	RowsInvalid       any            `json:"rows_invalid,omitempty"`
	RowsTotal         any            `json:"rows_total,omitempty"`
}

// Billing is the company's active tariffs and limits.
type Billing struct {
	Rates  []Rate `json:"rates"`
	Limits any    `json:"limits"`
}

// Rate is one active tariff.
type Rate struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	StartDate string `json:"start_date,omitempty"`
	EndDate   any    `json:"end_date,omitempty"`
	Units     any    `json:"units,omitempty"`
	UnitsLeft any    `json:"units_left,omitempty"`
}
