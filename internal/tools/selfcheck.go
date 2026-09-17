package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/vchasno"
)

// self_check answers the question every integration asks first: does this
// token work, what may it do, and what is in the way.

type checkResult struct {
	Name   string `json:"check"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
	Hint   string `json:"hint,omitempty"`
}

func (d *Deps) registerSelfCheck(srv *mcp.Server) {
	addRead(srv, "self_check", "Перевірка підключення",
		"Verify the connection end to end: that the token is accepted, that the company has an API tariff, and that the everyday endpoints (documents, incoming documents, employees, labels, document types, extra parameters, teams) actually answer. Run this first in a new session, and whenever anything starts failing — it turns a raw 403 into the specific thing that is missing.",
		func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
			Deep bool `json:"deep,omitempty" jsonschema:"Also probe the optional endpoints: scenarios, templates, archive folders, deletion requests, comment feed"`
		}) (*mcp.CallToolResult, any, error) {
			api := d.api(ctx)
			checks := []checkResult{}
			add := func(name string, err error, detail string) bool {
				if err != nil {
					c := checkResult{Name: name, OK: false, Detail: err.Error()}
					if ae, isAPI := err.(*vchasno.Error); isAPI {
						c.Hint = ae.Hint()
					}
					checks = append(checks, c)
					return false
				}
				checks = append(checks, checkResult{Name: name, OK: true, Detail: detail})
				return true
			}

			started := time.Now()
			billing, err := api.GetBilling(ctx)
			tariffOK := add("tariffs_and_limits", err, describeRates(billing))
			if !tariffOK {
				checks = append(checks, checkResult{Name: "api_access", OK: false,
					Detail: "the API is closed for this company",
					Hint:   "activate the 'Інтеграція' tariff in the Vchasno cabinet, or its one-time 30-day trial with activate_integration_trial"})
				return ok(map[string]any{"host": d.Sess.Creds.BaseURL, "api_open": false, "checks": checks,
					"took": time.Since(started).Round(time.Millisecond).String()})
			}

			roles, rerr := api.ListRoles(ctx)
			add("employees", rerr, fmt.Sprintf("%d active employees", len(rolesOf(roles))))

			outgoing, oerr := api.ListDocuments(ctx, vchasno.DocumentFilter{})
			add("outgoing_documents", oerr, fmt.Sprintf("%d in the first page", len(docsOf(outgoing))))

			incoming, ierr := api.ListIncomingDocuments(ctx, vchasno.DocumentFilter{})
			add("incoming_documents", ierr, fmt.Sprintf("%d in the first page", len(docsOf(incoming))))

			cats, cerr := api.ListCategories(ctx)
			add("document_types", cerr, fmt.Sprintf("%d types available", len(cats)))

			tags, terr := api.ListTags(ctx, 5, 0)
			add("labels", terr, fmt.Sprintf("%d labels", len(tagsOf(tags))))

			fields, ferr := api.ListFields(ctx)
			add("extra_parameters", ferr, fmt.Sprintf("%d parameters", len(fields)))

			groups, gerr := api.ListGroups(ctx)
			add("teams", gerr, fmt.Sprintf("%d teams", len(groups)))

			if in.Deep {
				scenarios, serr := api.ListScenarios(ctx)
				add("scenarios", serr, fmt.Sprintf("%d scenarios", len(scenarios)))
				templates, tmerr := api.ListDocumentTemplates(ctx, nil, nil, 5, 0)
				add("file_templates", tmerr, fmt.Sprintf("%d templates in the first page", len(templatesOf(templates))))
				dirs, derr := api.ListDirectories(ctx, "", "", "", 5)
				add("archive_folders", derr, fmt.Sprintf("%d folders at the root", len(dirsOf(dirs))))
				dreqs, dqerr := api.ListDeleteRequests(ctx, "", nil, nil, "")
				add("delete_requests", dqerr, fmt.Sprintf("%d requests", len(dreqs)))
				comments, cmerr := api.ListComments(ctx, "", "", "")
				add("comment_feed", cmerr, fmt.Sprintf("%d comments in the first page", len(commentsOf(comments))))
			}

			passed, failed := 0, 0
			for _, c := range checks {
				if c.OK {
					passed++
				} else {
					failed++
				}
			}
			return ok(map[string]any{
				"host": d.Sess.Creds.BaseURL, "api_open": true, "read_only_mode": d.Cfg.ReadOnly,
				"token": d.Sess.Creds.Redacted(), "passed": passed, "failed": failed,
				"took": time.Since(started).Round(time.Millisecond).String(), "checks": checks,
			})
		})
}

func describeRates(b *vchasno.Billing) string {
	if b == nil || len(b.Rates) == 0 {
		return "no active tariff reported"
	}
	names := make([]string, 0, len(b.Rates))
	for _, r := range b.Rates {
		names = append(names, fmt.Sprintf("%s (%s)", r.Name, r.Type))
	}
	return fmt.Sprintf("%d active: %v", len(b.Rates), names)
}

func rolesOf(l *vchasno.RoleList) []vchasno.Role {
	if l == nil {
		return nil
	}
	return l.Roles
}

func docsOf(l *vchasno.DocumentList) []vchasno.Document {
	if l == nil {
		return nil
	}
	return l.Documents
}

func tagsOf(l *vchasno.TagList) []vchasno.Tag {
	if l == nil {
		return nil
	}
	return l.Tags
}

func templatesOf(l *vchasno.TemplateList) []vchasno.DocumentTemplate {
	if l == nil {
		return nil
	}
	return l.Templates
}

func dirsOf(l *vchasno.DirectoryList) []vchasno.Directory {
	if l == nil {
		return nil
	}
	return l.Directories
}

func commentsOf(l *vchasno.CommentList) []vchasno.Comment {
	if l == nil {
		return nil
	}
	return l.Comments
}
