package vchasno

import (
	"context"
	"net/url"
)

// Endpoints that describe or configure the company itself: employees, teams,
// labels, extra parameters, document types, scenarios, templates, billing.

// ── employees (roles) ───────────────────────────────────────────

// ListRoles returns the active employees of the company with their role ids.
func (c *Client) ListRoles(ctx context.Context) (*RoleList, error) {
	var out RoleList
	_, err := c.Get(ctx, "/api/v2/roles", nil, &out)
	return &out, err
}

// UpdateRole changes permissions, notifications and profile fields of one
// employee. Only the keys present in patch are sent.
func (c *Client) UpdateRole(ctx context.Context, roleID string, patch map[string]any) error {
	_, err := c.Patch(ctx, "/api/v2/roles/"+url.PathEscape(roleID), nil, patch, nil)
	return err
}

// DeleteRole removes an employee account from the company.
func (c *Client) DeleteRole(ctx context.Context, roleID string) error {
	_, err := c.Delete(ctx, "/api/v2/roles/"+url.PathEscape(roleID), nil, nil, nil)
	return err
}

// InviteCoworkers sends registration invitations by email.
func (c *Client) InviteCoworkers(ctx context.Context, emails []string) error {
	_, err := c.Post(ctx, "/api/v2/invite/coworkers", nil, map[string]any{"emails": emails}, nil)
	return err
}

// CreateCoworker creates a ready employee account (corporate domain required).
func (c *Client) CreateCoworker(ctx context.Context, body map[string]any) (map[string]any, error) {
	out := map[string]any{}
	_, err := c.Post(ctx, "/api/v2/coworker", nil, body, &out)
	return out, err
}

// CreateUserTokens issues integration tokens for the given employees.
func (c *Client) CreateUserTokens(ctx context.Context, emails []string, expireDays string) (map[string]any, error) {
	body := map[string]any{"emails": emails}
	if expireDays != "" {
		body["expire_days"] = expireDays
	}
	out := map[string]any{}
	_, err := c.Post(ctx, "/api/v2/tokens", nil, body, &out)
	return out, err
}

// ResetUserTokens invalidates the integration tokens of the given employees.
func (c *Client) ResetUserTokens(ctx context.Context, emails []string) error {
	_, err := c.Delete(ctx, "/api/v2/tokens", nil, map[string]any{"emails": emails}, nil)
	return err
}

// ── teams (groups) ──────────────────────────────────────────────

// ListGroups returns the teams of the company.
func (c *Client) ListGroups(ctx context.Context) ([]Group, error) {
	var out []Group
	_, err := c.Get(ctx, "/api/v2/groups", nil, &out)
	return out, err
}

// GetGroup reads one team.
func (c *Client) GetGroup(ctx context.Context, id string) (*Group, error) {
	var out Group
	_, err := c.Get(ctx, "/api/v2/groups/"+url.PathEscape(id), nil, &out)
	return &out, err
}

// CreateGroup creates a team.
func (c *Client) CreateGroup(ctx context.Context, name string) (*Group, error) {
	var out Group
	_, err := c.Post(ctx, "/api/v2/groups", nil, map[string]string{"name": name}, &out)
	return &out, err
}

// RenameGroup changes a team's name.
func (c *Client) RenameGroup(ctx context.Context, id, name string) (*Group, error) {
	var out Group
	_, err := c.Patch(ctx, "/api/v2/groups/"+url.PathEscape(id), nil, map[string]string{"name": name}, &out)
	return &out, err
}

// DeleteGroup removes a team.
func (c *Client) DeleteGroup(ctx context.Context, id string) error {
	_, err := c.Delete(ctx, "/api/v2/groups/"+url.PathEscape(id), nil, nil, nil)
	return err
}

// ListGroupMembers returns the members of a team.
func (c *Client) ListGroupMembers(ctx context.Context, id string) ([]GroupMember, error) {
	var out []GroupMember
	_, err := c.Get(ctx, "/api/v2/groups/"+url.PathEscape(id)+"/members", nil, &out)
	return out, err
}

// AddGroupMembers adds employee roles to a team.
func (c *Client) AddGroupMembers(ctx context.Context, id string, roleIDs []string) ([]GroupMember, error) {
	var out []GroupMember
	_, err := c.Post(ctx, "/api/v2/groups/"+url.PathEscape(id)+"/members", nil, map[string]any{"role_ids": roleIDs}, &out)
	return out, err
}

// RemoveGroupMembers removes members from a team by member id (not role id).
func (c *Client) RemoveGroupMembers(ctx context.Context, id string, memberIDs []string) error {
	_, err := c.Post(ctx, "/api/v2/groups/"+url.PathEscape(id)+"/members/remove", nil, map[string]any{"group_members": memberIDs}, nil)
	return err
}

// ── labels (tags) ───────────────────────────────────────────────

// ListTags returns the labels of the company.
func (c *Client) ListTags(ctx context.Context, limit, offset int) (*TagList, error) {
	var out TagList
	_, err := c.Get(ctx, "/api/v2/tags", Q().Int("limit", limit).Int("offset", offset), &out)
	return &out, err
}

// CreateDocumentTags creates new labels and assigns them to documents.
func (c *Client) CreateDocumentTags(ctx context.Context, documentIDs, names []string) ([]Tag, error) {
	var out []Tag
	_, err := c.Post(ctx, "/api/v2/tags/documents", nil, map[string]any{"documents_ids": documentIDs, "names": names}, &out)
	return out, err
}

// AssignTagsToDocuments links existing labels to documents.
func (c *Client) AssignTagsToDocuments(ctx context.Context, documentIDs, tagIDs []string) error {
	_, err := c.Post(ctx, "/api/v2/tags/documents/connections", nil, map[string]any{"documents_ids": documentIDs, "tags_ids": tagIDs}, nil)
	return err
}

// UnassignTagsFromDocuments unlinks labels from documents. A label with no
// document and no role left is deleted by Vchasno automatically.
func (c *Client) UnassignTagsFromDocuments(ctx context.Context, documentIDs, tagIDs []string) error {
	_, err := c.Delete(ctx, "/api/v2/tags/documents/connections", nil, map[string]any{"documents_ids": documentIDs, "tags_ids": tagIDs}, nil)
	return err
}

// CreateRoleTags creates new labels and assigns them to employees.
func (c *Client) CreateRoleTags(ctx context.Context, roleIDs, names []string) ([]Tag, error) {
	var out []Tag
	_, err := c.Post(ctx, "/api/v2/tags/roles", nil, map[string]any{"roles_ids": roleIDs, "names": names}, &out)
	return out, err
}

// AssignTagsToRoles links existing labels to employees, granting them access
// to every document carrying the same label.
func (c *Client) AssignTagsToRoles(ctx context.Context, roleIDs, tagIDs []string) error {
	_, err := c.Post(ctx, "/api/v2/tags/roles/connections", nil, map[string]any{"roles_ids": roleIDs, "tags_ids": tagIDs}, nil)
	return err
}

// UnassignTagsFromRoles unlinks labels from employees.
func (c *Client) UnassignTagsFromRoles(ctx context.Context, roleIDs, tagIDs []string) error {
	_, err := c.Delete(ctx, "/api/v2/tags/roles/connections", nil, map[string]any{"roles_ids": roleIDs, "tags_ids": tagIDs}, nil)
	return err
}

// ListTagRoles returns the employees a label is attached to.
func (c *Client) ListTagRoles(ctx context.Context, tagID string) (*TagRoleList, error) {
	var out TagRoleList
	_, err := c.Get(ctx, "/api/v2/tags/"+url.PathEscape(tagID)+"/roles", nil, &out)
	return &out, err
}

// ── extra document parameters (fields) ──────────────────────────

// ListFields returns the extra parameters defined by the company.
func (c *Client) ListFields(ctx context.Context) ([]Field, error) {
	var out []Field
	_, err := c.Get(ctx, "/api/v2/fields", nil, &out)
	return out, err
}

// CreateField defines a new extra parameter.
func (c *Client) CreateField(ctx context.Context, name, fieldType string, isRequired bool) (*Field, error) {
	var out Field
	_, err := c.Post(ctx, "/api/v2/fields", nil, map[string]any{"name": name, "field_type": fieldType, "is_required": isRequired}, &out)
	return &out, err
}

// UpdateField renames an extra parameter and replaces its enum options.
func (c *Client) UpdateField(ctx context.Context, fieldID, name string, enumOptions []string) error {
	body := map[string]any{"name": name}
	if enumOptions != nil {
		body["enum_options"] = enumOptions
	}
	_, err := c.Patch(ctx, "/api/v2/fields/"+url.PathEscape(fieldID), nil, body, nil)
	return err
}

// GetDocumentFields reads the extra parameters set on one document.
func (c *Client) GetDocumentFields(ctx context.Context, documentID string) ([]DocumentField, error) {
	var out []DocumentField
	_, err := c.Get(ctx, "/api/v2/documents/"+url.PathEscape(documentID)+"/fields", nil, &out)
	return out, err
}

// SetDocumentField sets the value of an extra parameter on a document.
func (c *Client) SetDocumentField(ctx context.Context, documentID, fieldID, value string, isRequired bool) error {
	_, err := c.Post(ctx, "/api/v2/documents/"+url.PathEscape(documentID)+"/fields", nil,
		map[string]any{"field_id": fieldID, "value": value, "is_required": isRequired}, nil)
	return err
}

// ── document types (categories) ─────────────────────────────────

// ListCategories returns the public and company-private document types.
func (c *Client) ListCategories(ctx context.Context) ([]Category, error) {
	var out []Category
	_, err := c.Get(ctx, "/api/v2/document-categories", nil, &out)
	return out, err
}

// CreateCategory defines a company-private document type.
func (c *Client) CreateCategory(ctx context.Context, title string) (map[string]any, error) {
	out := map[string]any{}
	_, err := c.Post(ctx, "/api/v2/document-categories", nil, map[string]string{"title": title}, &out)
	return out, err
}

// RenameCategory renames a company-private document type.
func (c *Client) RenameCategory(ctx context.Context, categoryID int, title string) error {
	_, err := c.Patch(ctx, "/api/v2/document-categories/"+itoa(categoryID), nil, map[string]string{"title": title}, nil)
	return err
}

// DeleteCategory removes a company-private document type.
func (c *Client) DeleteCategory(ctx context.Context, categoryID int) error {
	_, err := c.Delete(ctx, "/api/v2/document-categories/"+itoa(categoryID), nil, nil, nil)
	return err
}

// ── scenarios and file templates ────────────────────────────────

// ListScenarios returns the company's document scenarios.
func (c *Client) ListScenarios(ctx context.Context) ([]Scenario, error) {
	var out []Scenario
	_, err := c.Get(ctx, "/api/v2/templates", nil, &out)
	return out, err
}

// GetScenario reads one scenario by id.
func (c *Client) GetScenario(ctx context.Context, id string) (*Scenario, error) {
	var out Scenario
	_, err := c.Get(ctx, "/api/v2/templates/"+url.PathEscape(id), nil, &out)
	return &out, err
}

// ListDocumentTemplates returns the file templates available to the user.
func (c *Client) ListDocumentTemplates(ctx context.Context, edrpou, sharingType []string, limit, cursor int) (*TemplateList, error) {
	q := Q().Strs("edrpou", edrpou).Strs("sharing_type", sharingType).Int("limit", limit).Int("cursor", cursor)
	var out TemplateList
	_, err := c.Get(ctx, "/api/v2/document-templates", q, &out)
	return &out, err
}

// GetDocumentTemplate reads one file template with its fillable fields.
func (c *Client) GetDocumentTemplate(ctx context.Context, id string) (*DocumentTemplate, error) {
	var out DocumentTemplate
	_, err := c.Get(ctx, "/api/v2/document-templates/"+url.PathEscape(id), nil, &out)
	return &out, err
}

// CreateDocumentFromTemplate fills a template and creates a document from it.
func (c *Client) CreateDocumentFromTemplate(ctx context.Context, templateID string, body map[string]any) (map[string]any, error) {
	out := map[string]any{}
	_, err := c.Post(ctx, "/api/v2/document-templates/"+url.PathEscape(templateID)+"/document", nil, body, &out)
	return out, err
}

// ── billing and counterparty checks ─────────────────────────────

// GetBilling reads the active tariffs and the limits of the company.
func (c *Client) GetBilling(ctx context.Context) (*Billing, error) {
	var out Billing
	_, err := c.Get(ctx, "/api/v2/company/billing", nil, &out)
	return &out, err
}

// ActivateIntegrationTrial turns on the one-time 30-day Integration trial.
func (c *Client) ActivateIntegrationTrial(ctx context.Context) (map[string]any, error) {
	out := map[string]any{}
	_, err := c.Post(ctx, "/api/v2/billing/companies/rates/trials", nil, map[string]string{"rate": "integration_trial"}, &out)
	return out, err
}

// CheckCompany reports whether a counterparty is registered in Vchasno.
func (c *Client) CheckCompany(ctx context.Context, edrpou string) (*CompanyCheck, error) {
	var out CompanyCheck
	_, err := c.Post(ctx, "/api/v2/check/company", nil, map[string]string{"edrpou": edrpou}, &out)
	return &out, err
}

// CheckCompaniesFile checks up to ten counterparties listed in an xlsx or csv file.
func (c *Client) CheckCompaniesFile(ctx context.Context, filename string, content []byte) (*CompanyCheckBatch, error) {
	var out CompanyCheckBatch
	_, err := c.PostMultipart(ctx, "/api/v2/check/company/upload", nil,
		[]FilePart{{Field: "file", Filename: filename, Content: content}}, nil, &out)
	return &out, err
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
