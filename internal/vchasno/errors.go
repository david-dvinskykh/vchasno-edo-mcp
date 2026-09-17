package vchasno

import (
	"fmt"
	"net/http"
	"strings"
)

// Error is a failed Vchasno API call. The service answers errors as
// {"code": "...", "reason": "...", "details": ...} with the HTTP status
// carrying most of the meaning, so both are kept.
type Error struct {
	Status   int    `json:"status"`
	Code     string `json:"code,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Details  any    `json:"details,omitempty"`
	Method   string `json:"method,omitempty"`
	Path     string `json:"path,omitempty"`
	Snippet  string `json:"body,omitempty"` // raw body when it was not the documented JSON shape
	RetryDue bool   `json:"-"`              // the caller may retry after a pause
}

func (e *Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "vchasno %s %s: HTTP %d", e.Method, e.Path, e.Status)
	if e.Code != "" {
		fmt.Fprintf(&b, " (%s)", e.Code)
	}
	if e.Reason != "" {
		fmt.Fprintf(&b, ": %s", e.Reason)
	} else if e.Snippet != "" {
		fmt.Fprintf(&b, ": %s", e.Snippet)
	}
	return b.String()
}

// Hint turns the documented error codes into an instruction the model can act on.
func (e *Error) Hint() string {
	switch e.Code {
	case "access_denied":
		return "The company has no active 'Інтеграція' (or 'AI Інтеграція') tariff, so the API is closed. " +
			"Check get_billing; a company may activate a one-time 30-day trial with activate_integration_trial."
	case "login_required":
		return "The token was not sent or is no longer valid. Re-issue the user token in Vchasno settings and log in again."
	case "rate_upload_overlimit":
		return "The tariff's upload quota is exhausted. See get_billing → limits.documents_sent / integration_documents."
	case "too_many_requests":
		return "Vchasno allows 10 requests per second per company. Retry after a short pause or lower MCP_MAX_RPS."
	}
	switch e.Status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "The token lacks the permission for this operation, or the company has no API tariff. " +
			"Employee permissions are listed by list_roles and changed by update_role."
	case http.StatusNotFound:
		return "No such object, or your company has no access to it. Check the id with list_documents / get_document."
	case http.StatusTooManyRequests:
		return "Rate limit: 10 requests per second per company. Retry after a pause."
	case http.StatusBadRequest:
		return "The request payload was rejected. Re-read the tool description: most fields have a documented set of values."
	}
	return ""
}

// IsNotFound reports whether err is a 404 from the API.
func IsNotFound(err error) bool {
	ae, ok := err.(*Error)
	return ok && ae.Status == http.StatusNotFound
}
