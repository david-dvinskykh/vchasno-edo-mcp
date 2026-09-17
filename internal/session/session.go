// Package session binds one Vchasno company token to a live API client and
// the slowly-changing company reference data (document types, employees,
// labels, extra parameters, teams) that almost every tool needs in order to
// translate names to ids and codes to meanings.
package session

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/config"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/vchasno"
)

// Session is one authenticated company context shared by every MCP request
// that presents the same token.
type Session struct {
	Client *vchasno.Client
	Creds  vchasno.Credentials
	Cfg    config.Config
	Logger *slog.Logger

	// APIOpen is false when the token is valid but the company has no active
	// "Інтеграція" tariff; every data call will then fail with access_denied.
	APIOpen  bool
	APINote  string
	Billing  *vchasno.Billing
	OpenedAt time.Time

	mu         sync.Mutex
	categories map[int]string
	catAt      time.Time
	roles      []vchasno.Role
	rolesAt    time.Time
	tags       []vchasno.Tag
	tagsAt     time.Time
	fields     []vchasno.Field
	fieldsAt   time.Time
	groups     []vchasno.Group
	groupsAt   time.Time
}

// refTTL is how long company reference data is reused before being refetched.
const refTTL = 10 * time.Minute

// Connect validates the token and prepares a session.
//
// A token that Vchasno does not recognise is a hard failure. A recognised
// token on a company without an API tariff is not: the session opens with
// APIOpen=false so that the server still starts and self_check / get_billing
// can explain what is missing.
func Connect(ctx context.Context, cfg config.Config, creds vchasno.Credentials, logger *slog.Logger) (*Session, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if strings.TrimSpace(creds.Token) == "" {
		return nil, &vchasno.Error{Status: 401, Code: "login_required", Reason: "no Vchasno token was provided"}
	}
	client := vchasno.New(creds, vchasno.Options{
		Timeout:       cfg.RequestTimeout,
		MaxRPS:        cfg.MaxRPS,
		RetryAttempts: cfg.RetryAttempts,
		MaxUpload:     cfg.MaxUploadBytes,
		Logger:        logger,
	})
	s := &Session{Client: client, Creds: client.Creds(), Cfg: cfg, Logger: logger, OpenedAt: time.Now()}

	billing, err := client.GetBilling(ctx)
	switch {
	case err == nil:
		s.APIOpen = true
		s.Billing = billing
	default:
		ae, ok := err.(*vchasno.Error)
		if !ok {
			return nil, err
		}
		switch {
		case ae.Code == "access_denied" || (ae.Status == 403 && ae.Code == ""):
			s.APIOpen = false
			s.APINote = ae.Reason
			if s.APINote == "" {
				s.APINote = "the company has no active 'Інтеграція' tariff"
			}
			logger.Warn("token accepted but API is closed for this company", "reason", s.APINote)
		case ae.Status == 401 || ae.Code == "login_required":
			return nil, err
		case ae.Status == 404:
			// Older deployments may not expose /company/billing; fall back to
			// a cheap call that every tariff has.
			if _, rerr := client.ListRoles(ctx); rerr != nil {
				return nil, rerr
			}
			s.APIOpen = true
		default:
			return nil, err
		}
	}
	return s, nil
}

// Close releases the session. Nothing is held open at the moment; the method
// exists so callers do not have to know that.
func (s *Session) Close() {}

// Categories returns document type id → title, merging the company's own types
// over the documented public ones.
func (s *Session) Categories(ctx context.Context) map[int]string {
	s.mu.Lock()
	if s.categories != nil && time.Since(s.catAt) < refTTL {
		out := s.categories
		s.mu.Unlock()
		return out
	}
	s.mu.Unlock()

	merged := make(map[int]string, len(vchasno.PublicCategories)+8)
	for id, title := range vchasno.PublicCategories {
		merged[id] = title
	}
	if s.APIOpen {
		if live, err := s.Client.ListCategories(ctx); err == nil {
			for _, c := range live {
				merged[c.CategoryID] = c.CategoryTitle
			}
		} else {
			s.Logger.Debug("cannot refresh document categories", "err", err)
		}
	}
	s.mu.Lock()
	s.categories, s.catAt = merged, time.Now()
	s.mu.Unlock()
	return merged
}

// Roles returns the company's employees, cached for refTTL.
func (s *Session) Roles(ctx context.Context) ([]vchasno.Role, error) {
	s.mu.Lock()
	if s.roles != nil && time.Since(s.rolesAt) < refTTL {
		out := s.roles
		s.mu.Unlock()
		return out, nil
	}
	s.mu.Unlock()
	list, err := s.Client.ListRoles(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.roles, s.rolesAt = list.Roles, time.Now()
	s.mu.Unlock()
	return list.Roles, nil
}

// Tags returns the company's labels, cached for refTTL.
func (s *Session) Tags(ctx context.Context) ([]vchasno.Tag, error) {
	s.mu.Lock()
	if s.tags != nil && time.Since(s.tagsAt) < refTTL {
		out := s.tags
		s.mu.Unlock()
		return out, nil
	}
	s.mu.Unlock()
	list, err := s.Client.ListTags(ctx, 200, 0)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.tags, s.tagsAt = list.Tags, time.Now()
	s.mu.Unlock()
	return list.Tags, nil
}

// Fields returns the company's extra document parameters, cached for refTTL.
func (s *Session) Fields(ctx context.Context) ([]vchasno.Field, error) {
	s.mu.Lock()
	if s.fields != nil && time.Since(s.fieldsAt) < refTTL {
		out := s.fields
		s.mu.Unlock()
		return out, nil
	}
	s.mu.Unlock()
	fields, err := s.Client.ListFields(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.fields, s.fieldsAt = fields, time.Now()
	s.mu.Unlock()
	return fields, nil
}

// Groups returns the company's teams, cached for refTTL.
func (s *Session) Groups(ctx context.Context) ([]vchasno.Group, error) {
	s.mu.Lock()
	if s.groups != nil && time.Since(s.groupsAt) < refTTL {
		out := s.groups
		s.mu.Unlock()
		return out, nil
	}
	s.mu.Unlock()
	groups, err := s.Client.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.groups, s.groupsAt = groups, time.Now()
	s.mu.Unlock()
	return groups, nil
}

// Invalidate drops the cached reference data after a tool changed it.
func (s *Session) Invalidate() {
	s.mu.Lock()
	s.categories, s.roles, s.tags, s.fields, s.groups = nil, nil, nil, nil, nil
	s.mu.Unlock()
}

// ResolveRoleID maps an employee email to a role id, so that tools can accept
// "someone@company.ua" wherever Vchasno wants a role GUID. Anything that is
// not an email address is taken to be an id already.
func (s *Session) ResolveRoleID(ctx context.Context, emailOrID string) (string, error) {
	v := strings.TrimSpace(emailOrID)
	if v == "" || !strings.Contains(v, "@") {
		return v, nil
	}
	roles, err := s.Roles(ctx)
	if err != nil {
		return "", err
	}
	for _, r := range roles {
		if strings.EqualFold(r.Email, v) {
			return r.ID, nil
		}
	}
	return "", &vchasno.Error{Status: 404, Reason: "no active employee with email " + v + "; call list_roles to see who is in the company"}
}

// ResolveRoleIDs maps a list of emails and/or ids to role ids.
func (s *Session) ResolveRoleIDs(ctx context.Context, values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	for _, v := range values {
		id, err := s.ResolveRoleID(ctx, v)
		if err != nil {
			return nil, err
		}
		if id != "" {
			out = append(out, id)
		}
	}
	return out, nil
}

// ResolveTagID maps a label name to its id. An id is returned unchanged,
// whether or not it is shaped like a GUID: the lookup checks the company's own
// ids before falling back to the shape.
func (s *Session) ResolveTagID(ctx context.Context, nameOrID string) (string, error) {
	v := strings.TrimSpace(nameOrID)
	if v == "" {
		return v, nil
	}
	tags, err := s.Tags(ctx)
	if err != nil {
		if looksLikeGUID(v) {
			return v, nil
		}
		return "", err
	}
	for _, t := range tags {
		if t.ID == v {
			return v, nil
		}
	}
	for _, t := range tags {
		if strings.EqualFold(t.Name, v) {
			return t.ID, nil
		}
	}
	if looksLikeGUID(v) {
		return v, nil
	}
	return "", &vchasno.Error{Status: 404, Reason: "no label named " + v + "; call list_tags to see the company's labels"}
}

// ResolveTagIDs maps a list of label names and/or ids to ids.
func (s *Session) ResolveTagIDs(ctx context.Context, values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	for _, v := range values {
		id, err := s.ResolveTagID(ctx, v)
		if err != nil {
			return nil, err
		}
		if id != "" {
			out = append(out, id)
		}
	}
	return out, nil
}

// ResolveFieldID maps an extra parameter name to its id.
func (s *Session) ResolveFieldID(ctx context.Context, nameOrID string) (string, error) {
	v := strings.TrimSpace(nameOrID)
	if v == "" {
		return v, nil
	}
	fields, err := s.Fields(ctx)
	if err != nil {
		if looksLikeGUID(v) {
			return v, nil
		}
		return "", err
	}
	for _, f := range fields {
		if f.ID == v {
			return v, nil
		}
	}
	for _, f := range fields {
		if strings.EqualFold(f.Name, v) {
			return f.ID, nil
		}
	}
	if looksLikeGUID(v) {
		return v, nil
	}
	return "", &vchasno.Error{Status: 404, Reason: "no extra parameter named " + v + "; call list_fields to see them"}
}

// ResolveGroupID maps a team name to its id.
func (s *Session) ResolveGroupID(ctx context.Context, nameOrID string) (string, error) {
	v := strings.TrimSpace(nameOrID)
	if v == "" {
		return v, nil
	}
	groups, err := s.Groups(ctx)
	if err != nil {
		if looksLikeGUID(v) {
			return v, nil
		}
		return "", err
	}
	for _, g := range groups {
		if g.ID == v {
			return v, nil
		}
	}
	for _, g := range groups {
		if strings.EqualFold(g.Name, v) {
			return g.ID, nil
		}
	}
	if looksLikeGUID(v) {
		return v, nil
	}
	return "", &vchasno.Error{Status: 404, Reason: "no team named " + v + "; call list_groups to see the company's teams"}
}

// ResolveCategoryID maps a document type title to its numeric id.
func (s *Session) ResolveCategoryID(ctx context.Context, title string) (int, bool) {
	t := strings.TrimSpace(strings.ToLower(title))
	if t == "" {
		return 0, false
	}
	cats := s.Categories(ctx)
	for id, name := range cats {
		if strings.ToLower(name) == t {
			return id, true
		}
	}
	// The documented spellings and the live ones do not always agree — the
	// service calls type 15 "Інший" where the API reference writes "Інше" —
	// so a documented name is accepted as long as the live list has that id.
	for id, name := range vchasno.PublicCategories {
		if strings.ToLower(name) == t {
			if _, known := cats[id]; known {
				return id, true
			}
		}
	}
	// No substring fallback on purpose: "Рахуно" is a substring of
	// "Розрахунок коригування", and silently stamping the wrong type onto a
	// real document is worse than refusing. A near miss is answered with
	// suggestions instead — see SuggestCategories.
	return 0, false
}

// SuggestCategories returns the type titles closest to what was asked for, so
// that a failed lookup can tell the caller what it probably meant.
func (s *Session) SuggestCategories(ctx context.Context, title string, limit int) []string {
	t := strings.ToLower(strings.TrimSpace(title))
	if t == "" {
		return nil
	}
	type scored struct {
		text  string
		score int
	}
	var best []scored
	for id, name := range s.Categories(ctx) {
		n := strings.ToLower(name)
		score := commonPrefix(t, n)
		if score < 3 && !strings.Contains(n, t) && !strings.Contains(t, n) {
			continue
		}
		best = append(best, scored{fmt.Sprintf("%d — %s", id, name), score})
	}
	sort.Slice(best, func(i, j int) bool { return best[i].score > best[j].score })
	out := make([]string, 0, limit)
	for i, b := range best {
		if i >= limit {
			break
		}
		out = append(out, b.text)
	}
	return out
}

func commonPrefix(a, b string) int {
	ar, br := []rune(a), []rune(b)
	n := 0
	for n < len(ar) && n < len(br) && ar[n] == br[n] {
		n++
	}
	return n
}

func looksLikeGUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
			if !isHex {
				return false
			}
		}
	}
	return true
}
