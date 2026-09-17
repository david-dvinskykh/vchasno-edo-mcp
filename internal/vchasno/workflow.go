package vchasno

import (
	"context"
	"net/url"
)

// Endpoints around the life of a document after it is uploaded: comments,
// internal approval, deletion agreements, archiving, structured data,
// public links, sign sessions, cloud-key signing and action reports.

// ── comments ────────────────────────────────────────────────────

// ListComments reads the company-wide comment feed.
func (c *Client) ListComments(ctx context.Context, dateFrom, dateTo, cursor string) (*CommentList, error) {
	var out CommentList
	_, err := c.Get(ctx, "/api/v2/documents/comments", Q().Str("date_from", dateFrom).Str("date_to", dateTo).Str("cursor", cursor), &out)
	return &out, err
}

// DocumentComments reads the comments of one document.
func (c *Client) DocumentComments(ctx context.Context, documentID string) ([]Comment, error) {
	var wrapped struct {
		Comments []Comment `json:"comments"`
	}
	resp, err := c.Get(ctx, "/api/v2/documents/"+url.PathEscape(documentID)+"/comments", nil, &wrapped)
	if err != nil {
		return nil, err
	}
	if len(wrapped.Comments) > 0 {
		return wrapped.Comments, nil
	}
	// The endpoint may answer with a bare array.
	var bare []Comment
	if jerr := resp.JSON(&bare); jerr == nil {
		return bare, nil
	}
	return wrapped.Comments, nil
}

// AddComment posts a comment on a document, either internal (to colleagues)
// or visible to the counterparty.
func (c *Client) AddComment(ctx context.Context, documentID, text string, isInternal bool) error {
	_, err := c.Post(ctx, "/api/v2/documents/"+url.PathEscape(documentID)+"/comments", nil,
		map[string]any{"text": text, "is_internal": isInternal}, nil)
	return err
}

// ── internal approval (reviews) ─────────────────────────────────

// ReviewHistory reads what each approver did and when.
func (c *Client) ReviewHistory(ctx context.Context, documentID string) ([]ReviewAction, error) {
	var out []ReviewAction
	_, err := c.Get(ctx, "/api/v2/documents/"+url.PathEscape(documentID)+"/reviews", nil, &out)
	return out, err
}

// ReviewRequests reads who was assigned to approve a document.
func (c *Client) ReviewRequests(ctx context.Context, documentID string) ([]ReviewRequest, error) {
	var out []ReviewRequest
	_, err := c.Get(ctx, "/api/v2/documents/"+url.PathEscape(documentID)+"/reviews/requests", nil, &out)
	return out, err
}

// ReviewState reads the overall approval state of a document.
func (c *Client) ReviewState(ctx context.Context, documentID string) (*ReviewStatus, error) {
	var out ReviewStatus
	_, err := c.Get(ctx, "/api/v2/documents/"+url.PathEscape(documentID)+"/reviews/status", nil, &out)
	return &out, err
}

// AddReviewer adds an employee (by email) or a team (by name) to the approval
// of a document. Exactly one of userEmail and groupName must be set.
func (c *Client) AddReviewer(ctx context.Context, documentID, userEmail, groupName string, isParallel bool) error {
	body := map[string]any{"is_parallel": isParallel}
	if userEmail != "" {
		body["user_to_email"] = userEmail
	}
	if groupName != "" {
		body["group_to_name"] = groupName
	}
	_, err := c.Post(ctx, "/api/v2/documents/"+url.PathEscape(documentID)+"/reviews/requests", nil, body, nil)
	return err
}

// RemoveReviewer takes an employee or a team out of the approval.
func (c *Client) RemoveReviewer(ctx context.Context, documentID, userEmail, groupName string) error {
	body := map[string]any{}
	if userEmail != "" {
		body["user_to_email"] = userEmail
	}
	if groupName != "" {
		body["group_to_name"] = groupName
	}
	_, err := c.Delete(ctx, "/api/v2/documents/"+url.PathEscape(documentID)+"/reviews/requests", nil, body, nil)
	return err
}

// ── delete requests ─────────────────────────────────────────────

// CreateDeleteRequest asks the other party to agree to a deletion.
func (c *Client) CreateDeleteRequest(ctx context.Context, documentID, message string) ([]DeleteRequest, error) {
	var out []DeleteRequest
	_, err := c.Post(ctx, "/api/v2/documents/"+url.PathEscape(documentID)+"/delete-requests", nil, map[string]string{"message": message}, &out)
	return out, err
}

// CancelDeleteRequest withdraws a deletion request this role created.
func (c *Client) CancelDeleteRequest(ctx context.Context, documentID string) error {
	_, err := c.Delete(ctx, "/api/v2/documents/"+url.PathEscape(documentID)+"/delete-requests", nil, nil, nil)
	return err
}

// AcceptDeleteRequest agrees to a deletion request (deletes the document).
func (c *Client) AcceptDeleteRequest(ctx context.Context, documentID string) error {
	_, err := c.Post(ctx, "/api/v2/documents/"+url.PathEscape(documentID)+"/delete-requests/acceptions", nil, nil, nil)
	return err
}

// RejectDeleteRequest refuses a deletion request with a reason.
func (c *Client) RejectDeleteRequest(ctx context.Context, documentID, message string) error {
	_, err := c.Post(ctx, "/api/v2/documents/"+url.PathEscape(documentID)+"/delete-requests/rejections", nil,
		map[string]string{"reject_message": message}, nil)
	return err
}

// ListDeleteRequests reads the company's incoming (and optionally outgoing)
// deletion requests.
func (c *Client) ListDeleteRequests(ctx context.Context, status string, ids []string, withOutgoing *bool, cursor string) ([]DeleteRequest, error) {
	q := Q().Str("status", status).Strs("ids", ids).Bool("with_outgoing", withOutgoing).Str("cursor", cursor)
	var out []DeleteRequest
	_, err := c.Get(ctx, "/api/v2/documents/delete-requests", q, &out)
	return out, err
}

// LockDelete forbids the owner from deleting the given incoming documents
// directly: they will have to raise a deletion request instead.
func (c *Client) LockDelete(ctx context.Context, documentIDs []string) (*UpdatedIDs, error) {
	var out UpdatedIDs
	_, err := c.Post(ctx, "/api/v2/documents/delete-requests/lock-delete", nil, map[string]any{"document_ids": documentIDs}, &out)
	return &out, err
}

// UnlockDelete lifts the direct-deletion lock.
func (c *Client) UnlockDelete(ctx context.Context, documentIDs []string) (*UpdatedIDs, error) {
	var out UpdatedIDs
	_, err := c.Delete(ctx, "/api/v2/documents/delete-requests/lock-delete", nil, map[string]any{"document_ids": documentIDs}, &out)
	return &out, err
}

// ── structured data ─────────────────────────────────────────────

// StartExtraction queues structured-data recognition for up to 100 documents.
func (c *Client) StartExtraction(ctx context.Context, documentIDs []string) ([]Extraction, error) {
	var out struct {
		Data []Extraction `json:"data"`
	}
	_, err := c.Post(ctx, "/api/v2/documents/structured-data/extractions", nil, map[string]any{"document_ids": documentIDs}, &out)
	return out.Data, err
}

// ListExtractions reads recognition runs with filters and cursor pagination.
func (c *Client) ListExtractions(ctx context.Context, documentIDs []string, status, cursor string, limit int) (*ExtractionList, error) {
	q := Q().Strs("document_ids", documentIDs).Str("status", status).Str("cursor", cursor).Int("limit", limit)
	var out ExtractionList
	_, err := c.Get(ctx, "/api/v2/documents/structured-data/extractions", q, &out)
	return &out, err
}

// DownloadStructuredData fetches the recognised data as json, xml or xlsx.
// When recognition is not finished the answer is a small JSON status instead
// of a file, which the caller detects by unmarshalling it.
func (c *Client) DownloadStructuredData(ctx context.Context, documentID, format string) (*Response, error) {
	return c.GetRaw(ctx, "/api/v2/documents/"+url.PathEscape(documentID)+"/structured-data/download", Q().Str("format", format))
}

// ── archive ─────────────────────────────────────────────────────

// ListDirectories reads archive folders, optionally under a parent folder.
func (c *Client) ListDirectories(ctx context.Context, parentID, search, cursor string, limit int) (*DirectoryList, error) {
	q := Q().Str("parent_id", parentID).Str("search", search).Str("cursor", cursor).Int("limit", limit)
	var out DirectoryList
	_, err := c.Get(ctx, "/api/v2/archive/directories", q, &out)
	return &out, err
}

// UploadScans puts one or more scans straight into the archive.
func (c *Client) UploadScans(ctx context.Context, parentID string, files []FilePart) (*DocumentList, error) {
	for i := range files {
		files[i].Field = "files"
	}
	var out DocumentList
	_, err := c.PostMultipart(ctx, "/api/v2/archive/scans", Q().Str("parent_id", parentID), files, nil, &out)
	return &out, err
}

// ArchiveDocuments moves documents into the archive.
func (c *Client) ArchiveDocuments(ctx context.Context, documentIDs []string, directoryID string) error {
	body := map[string]any{"document_ids": documentIDs}
	if directoryID != "" {
		body["directory_id"] = directoryID
	}
	_, err := c.Post(ctx, "/api/v2/documents/archive", nil, body, nil)
	return err
}

// UnarchiveDocuments moves documents back out of the archive.
func (c *Client) UnarchiveDocuments(ctx context.Context, documentIDs []string) error {
	_, err := c.Delete(ctx, "/api/v2/documents/archive", nil, map[string]any{"document_ids": documentIDs}, nil)
	return err
}

// ImportSigned uploads a document signed elsewhere into the archive: either
// an original plus detached .p7s signatures, or one .p7s / ASiC-E container.
func (c *Client) ImportSigned(ctx context.Context, files []FilePart, fields map[string][]string) (*ImportedSigned, error) {
	var out ImportedSigned
	_, err := c.PostMultipart(ctx, "/api/v2/archive/import-signed", nil, files, fields, &out)
	return &out, err
}

// UploadVisualization attaches (or replaces) the PDF rendering of an imported
// signed document, and/or switches the Vchasno stamps flag.
func (c *Client) UploadVisualization(ctx context.Context, documentID string, files []FilePart, fields map[string][]string) (map[string]any, error) {
	out := map[string]any{}
	_, err := c.PostMultipart(ctx, "/api/v2/archive/import-signed/"+url.PathEscape(documentID)+"/visualization", nil, files, fields, &out)
	return out, err
}

// ── public links ────────────────────────────────────────────────

// GetSharedLink reads the public-link settings of a document.
func (c *Client) GetSharedLink(ctx context.Context, documentID string) (*SharedLink, error) {
	var out SharedLink
	_, err := c.Get(ctx, "/api/v2/shared-documents/"+url.PathEscape(documentID), nil, &out)
	return &out, err
}

// CreateSharedLink publishes a view or sign link for a document in status 7000.
func (c *Client) CreateSharedLink(ctx context.Context, documentID string, body map[string]any) (*SharedLink, error) {
	var out SharedLink
	_, err := c.Post(ctx, "/api/v2/shared-documents/"+url.PathEscape(documentID), nil, body, &out)
	return &out, err
}

// UpdateSharedLink changes an existing public link.
func (c *Client) UpdateSharedLink(ctx context.Context, sharedID string, body map[string]any) (*SharedLink, error) {
	var out SharedLink
	_, err := c.Put(ctx, "/api/v2/shared-documents/"+url.PathEscape(sharedID), nil, body, &out)
	return &out, err
}

// RevokeSharedLink deactivates a public link.
func (c *Client) RevokeSharedLink(ctx context.Context, sharedID string) error {
	_, err := c.Delete(ctx, "/api/v2/shared-documents/"+url.PathEscape(sharedID), nil, nil, nil)
	return err
}

// ── personal-cabinet sessions ───────────────────────────────────

// CreateSignSession opens a view or sign session for one person, returning a
// URL that person can open without a Vchasno account of their own.
func (c *Client) CreateSignSession(ctx context.Context, body map[string]any) (*SignSession, error) {
	var out SignSession
	_, err := c.Post(ctx, "/api/v2/sign-sessions", nil, body, &out)
	return &out, err
}

// ── Vchasno.KEP cloud signing ───────────────────────────────────

// CreateCloudSession opens a cloud-key signing session; the key owner then
// confirms it in the Vchasno.KEP app or by entering the key password.
func (c *Client) CreateCloudSession(ctx context.Context, clientID string, duration int, useRefreshToken bool) (*CloudSession, error) {
	body := map[string]any{"client_id": clientID}
	if duration > 0 {
		body["duration"] = duration
	}
	if useRefreshToken {
		body["use_refresh_token"] = true
	}
	var out CloudSession
	_, err := c.Post(ctx, "/api/v2/cloud-signer/sessions/create", nil, body, &out)
	return &out, err
}

// CheckCloudSession polls a cloud session; the token is returned exactly once.
func (c *Client) CheckCloudSession(ctx context.Context, authSessionID string) (*CloudSession, error) {
	var out CloudSession
	_, err := c.Post(ctx, "/api/v2/cloud-signer/sessions/check", nil, map[string]string{"auth_session_id": authSessionID}, &out)
	return &out, err
}

// CheckCloudRefreshSession polls a session created with use_refresh_token.
func (c *Client) CheckCloudRefreshSession(ctx context.Context, authSessionID string) (*CloudSession, error) {
	var out CloudSession
	_, err := c.Post(ctx, "/api/v2/cloud-signer/sessions/refresh/check", nil, map[string]string{"auth_session_id": authSessionID}, &out)
	return &out, err
}

// RefreshCloudToken exchanges a refresh token for a new access token.
func (c *Client) RefreshCloudToken(ctx context.Context, authSessionID, refreshToken string) (*CloudSession, error) {
	var out CloudSession
	_, err := c.Post(ctx, "/api/v2/cloud-signer/sessions/refresh", nil,
		map[string]string{"auth_session_id": authSessionID, "refresh_token": refreshToken}, &out)
	return &out, err
}

// CloudSignDocument signs a document with a Vchasno.KEP cloud key.
func (c *Client) CloudSignDocument(ctx context.Context, body map[string]any) (map[string]any, error) {
	out := map[string]any{}
	_, err := c.Post(ctx, "/api/v2/cloud-signer/sessions/sign-document", nil, body, &out)
	return out, err
}

// ── action history reports ──────────────────────────────────────

// RequestDocumentActionsReport queues an xlsx report of document actions.
// The period may not start more than a year ago and may not exceed 30 days.
func (c *Client) RequestDocumentActionsReport(ctx context.Context, dateFrom, dateTo string) (*ReportRequest, error) {
	var out ReportRequest
	_, err := c.Post(ctx, "/api/v2/document-actions/request-report", nil, map[string]string{"date_from": dateFrom, "date_to": dateTo}, &out)
	return &out, err
}

// RequestUserActionsReport queues an xlsx report of employee actions.
func (c *Client) RequestUserActionsReport(ctx context.Context, dateFrom, dateTo string) (*ReportRequest, error) {
	var out ReportRequest
	_, err := c.Post(ctx, "/api/v2/user-actions/request-report", nil, map[string]string{"date_from": dateFrom, "date_to": dateTo}, &out)
	return &out, err
}

// ReportStatusOf checks whether a queued report is ready.
func (c *Client) ReportStatusOf(ctx context.Context, reportID string) (*ReportStatus, error) {
	var out ReportStatus
	_, err := c.Get(ctx, "/api/v2/actions/report-status/"+url.PathEscape(reportID), nil, &out)
	return &out, err
}

// DownloadReport fetches the xlsx of a ready report.
func (c *Client) DownloadReport(ctx context.Context, reportID string) (*Response, error) {
	return c.GetRaw(ctx, "/api/v2/actions/download-report/"+url.PathEscape(reportID), nil)
}
