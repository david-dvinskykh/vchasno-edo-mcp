package tools

import (
	"context"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The company's own configuration: employees and their permissions, teams,
// labels, extra document parameters, document types, scenarios, file
// templates, tariffs and counterparty checks.

type emptyIn struct{}

type tagsListIn struct {
	Limit  int `json:"limit,omitempty" jsonschema:"How many labels to return, up to 200 (default 100)"`
	Offset int `json:"offset,omitempty" jsonschema:"Skip this many labels (up to 20000)"`
}

type tagDocumentsIn struct {
	Action      string   `json:"action,omitempty" jsonschema:"create (make new labels and put them on the documents), assign (attach existing labels) or unassign (detach). Default: assign, or create when names are given"`
	DocumentIDs []string `json:"document_ids" jsonschema:"Documents to label"`
	Names       []string `json:"names,omitempty" jsonschema:"Label names for action=create. They must not exist in the company yet"`
	Tags        []string `json:"tags,omitempty" jsonschema:"Existing labels for assign/unassign, by name or id"`
}

type tagRolesIn struct {
	Action string   `json:"action,omitempty" jsonschema:"create, assign or unassign. Default: assign, or create when names are given"`
	People []string `json:"people" jsonschema:"Employees by email or role id"`
	Names  []string `json:"names,omitempty" jsonschema:"Label names for action=create"`
	Tags   []string `json:"tags,omitempty" jsonschema:"Existing labels for assign/unassign, by name or id"`
}

type tagIDIn struct {
	Tag string `json:"tag" jsonschema:"Label id or name"`
}

type createFieldIn struct {
	Name       string `json:"name" jsonschema:"Name of the parameter"`
	Type       string `json:"type" jsonschema:"text, number, date or enum"`
	IsRequired bool   `json:"required,omitempty" jsonschema:"true = the parameter must be filled in"`
}

type updateFieldIn struct {
	Field       string   `json:"field" jsonschema:"Parameter id or current name"`
	Name        string   `json:"name" jsonschema:"New name, 1…512 characters, unique inside the company"`
	EnumOptions []string `json:"enum_options,omitempty" jsonschema:"For enum parameters: the complete replacement list of options, each up to 64 characters. Leaving it out keeps the current options"`
}

type documentFieldIn struct {
	DocumentID string `json:"document_id" jsonschema:"Document id"`
	Field      string `json:"field" jsonschema:"Parameter id or name"`
	Value      string `json:"value" jsonschema:"Value to store"`
	IsRequired bool   `json:"required,omitempty" jsonschema:"Mark the parameter as mandatory for this document"`
}

type categoryIn struct {
	Title string `json:"title" jsonschema:"Name of the document type. Must be unique in the company"`
}

type categoryEditIn struct {
	CategoryID int    `json:"category_id" jsonschema:"Id of the company's own document type"`
	Title      string `json:"title,omitempty" jsonschema:"New name, for renaming"`
	Confirm    bool   `json:"confirm,omitempty" jsonschema:"Required for deletion: it cannot be undone"`
}

type groupIn struct {
	Group string `json:"group" jsonschema:"Team id or name"`
}

type createGroupIn struct {
	Name string `json:"name" jsonschema:"Name of the new team"`
}

type renameGroupIn struct {
	Group string `json:"group" jsonschema:"Team id or current name"`
	Name  string `json:"name" jsonschema:"New name"`
}

type groupMembersIn struct {
	Group  string   `json:"group" jsonschema:"Team id or name"`
	Action string   `json:"action" jsonschema:"list, add or remove"`
	People []string `json:"people,omitempty" jsonschema:"For add: employees by email or role id. For remove: member ids from action=list (member id, not role id)"`
}

type templatesIn struct {
	Edrpou      []string `json:"edrpou,omitempty" jsonschema:"Filter by the ЄДРПОУ of the owning company; may be repeated"`
	SharingType []string `json:"sharing_type,omitempty" jsonschema:"Filter by access type: private, public or link"`
	Limit       int      `json:"limit,omitempty" jsonschema:"1…500, default 20"`
	Cursor      int      `json:"cursor,omitempty" jsonschema:"Cursor from the previous answer"`
}

type templateIDIn struct {
	ID string `json:"id" jsonschema:"Template id"`
}

type createFromTemplateIn struct {
	TemplateID   string            `json:"template_id" jsonschema:"Template to fill (get_document_template lists its fields)"`
	Title        string            `json:"title,omitempty" jsonschema:"Title of the new document, up to 255 characters. Defaults to the template's title"`
	Category     string            `json:"category,omitempty" jsonschema:"Document type: numeric id or title"`
	DateDocument string            `json:"date_document,omitempty" jsonschema:"Document date, YYYY-MM-DD"`
	Number       string            `json:"number,omitempty" jsonschema:"Document number, up to 512 characters"`
	Amount       string            `json:"amount,omitempty" jsonschema:"Document amount in hryvnias"`
	Fields       map[string]string `json:"fields,omitempty" jsonschema:"Values to substitute. For a PDF template the keys are the field ids from get_document_template; for a DOCX template they are the placeholder names without the braces, e.g. contract_number for {{contract_number}}"`
}

type rolesIn struct {
	Search string `json:"search,omitempty" jsonschema:"Filter employees by email or position, case-insensitive"`
}

type updateRoleIn struct {
	Person        string          `json:"person" jsonschema:"Employee by email or role id"`
	Position      string          `json:"position,omitempty" jsonschema:"Job title"`
	IsAdmin       string          `json:"is_admin,omitempty" jsonschema:"true = company administrator (which switches every permission on), false = ordinary employee"`
	Permissions   map[string]bool `json:"permissions,omitempty" jsonschema:"Permission flags to set, e.g. can_sign_and_reject_document, can_upload_document, can_delete_document, can_archive_documents, can_view_private_document. See the vchasno://guide/permissions resource for the full list"`
	Notifications map[string]bool `json:"notifications,omitempty" jsonschema:"Notification flags to set, e.g. can_receive_inbox, can_receive_comments, can_receive_rejects, can_receive_reminders"`
	AllowedIPs    []string        `json:"allowed_ips,omitempty" jsonschema:"Restrict sign-in to these IPs; a variable address is written as a prefix with a star, e.g. 192.168.*"`
	AllowedAPIIPs []string        `json:"allowed_api_ips,omitempty" jsonschema:"IPs allowed to call the API and Vchasno.KEP cloud signing"`
	Status        string          `json:"status,omitempty" jsonschema:"Set to 'active' to restore a previously removed employee"`
	Confirm       bool            `json:"confirm,omitempty" jsonschema:"Required when is_admin=true or permissions are changed: this widens what somebody may do"`
}

type personIn struct {
	Person  string `json:"person" jsonschema:"Employee by email or role id"`
	Confirm bool   `json:"confirm,omitempty" jsonschema:"Must be true: removing an account cannot be undone"`
}

type inviteIn struct {
	Emails []string `json:"emails" jsonschema:"Email addresses to invite into the company"`
}

type createCoworkerIn struct {
	Email      string `json:"email" jsonschema:"Email in the company's own domain, not yet registered in Vchasno"`
	FirstName  string `json:"first_name,omitempty" jsonschema:"Given name"`
	SecondName string `json:"second_name,omitempty" jsonschema:"Patronymic"`
	LastName   string `json:"last_name,omitempty" jsonschema:"Family name"`
	Phone      string `json:"phone,omitempty" jsonschema:"Phone in the +380XXXXXXXXX format; the person confirms it themselves"`
}

type tokensIn struct {
	Emails     []string `json:"emails" jsonschema:"Employees whose integration token to issue or reset"`
	ExpireDays string   `json:"expire_days,omitempty" jsonschema:"Token lifetime in days (issuing only)"`
	Confirm    bool     `json:"confirm,omitempty" jsonschema:"Required for reset: every integration using the old token stops working at once"`
}

type checkCompanyIn struct {
	Edrpou string     `json:"edrpou,omitempty" jsonschema:"ЄДРПОУ/ІПН of one counterparty"`
	File   *fileInput `json:"file,omitempty" jsonschema:"An .xlsx or .csv with a single ЄДРПОУ/ІПН column, up to 10 codes, instead of the single edrpou"`
}

type trialIn struct {
	Confirm bool `json:"confirm,omitempty" jsonschema:"Must be true: the Integration trial can be activated only once per company, ever"`
}

func (d *Deps) registerCompany(srv *mcp.Server) {
	addRead(srv, "get_billing", "Тарифи та ліміти",
		"Active tariffs of the company and how much of each limit is used: documents sent, documents viewable in the active register and in the archive, employees, and integration units. Start here whenever a call fails with access_denied or rate_upload_overlimit.",
		func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, any, error) {
			billing, err := d.api(ctx).GetBilling(ctx)
			if err != nil {
				return fail(err)
			}
			return ok(billing)
		})

	addRead(srv, "list_roles", "Співробітники",
		"Active employees of the company with their role ids, emails, positions and invitation dates. Role ids are what the API wants wherever a person is named, though this server also accepts the email everywhere.",
		func(ctx context.Context, _ *mcp.CallToolRequest, in rolesIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			roles, err := d.Sess.Roles(ctx)
			if err != nil {
				return fail(err)
			}
			if q := strings.ToLower(strings.TrimSpace(in.Search)); q != "" {
				filtered := roles[:0:0]
				for _, r := range roles {
					if strings.Contains(strings.ToLower(r.Email), q) || strings.Contains(strings.ToLower(r.Position), q) {
						filtered = append(filtered, r)
					}
				}
				roles = filtered
			}
			return ok(map[string]any{"count": len(roles), "employees": roles})
		})

	addRead(srv, "list_tags", "Ярлики компанії",
		"Labels of the company. A label both marks documents and grants access: an employee carrying a label sees every document with that label. A label with no document and no employee left is deleted automatically.",
		func(ctx context.Context, _ *mcp.CallToolRequest, in tagsListIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			list, err := d.api(ctx).ListTags(ctx, in.Limit, in.Offset)
			if err != nil {
				return fail(err)
			}
			return ok(map[string]any{"count": len(list.Tags), "tags": list.Tags})
		})

	addRead(srv, "list_tag_roles", "Хто має ярлик",
		"Which employees carry a label, and therefore see every document marked with it.",
		func(ctx context.Context, _ *mcp.CallToolRequest, in tagIDIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			id, err := d.Sess.ResolveTagID(ctx, in.Tag)
			if err != nil {
				return fail(err)
			}
			list, err := d.api(ctx).ListTagRoles(ctx, id)
			if err != nil {
				return fail(err)
			}
			return ok(map[string]any{"tag_id": id, "count": len(list.Roles), "roles": list.Roles})
		})

	addRead(srv, "list_fields", "Додаткові параметри",
		"Extra document parameters defined by the company (text, number, date or a list of options) — the custom attributes a document can carry beyond title, number, date and amount.",
		func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			fields, err := d.Sess.Fields(ctx)
			if err != nil {
				return fail(err)
			}
			return ok(map[string]any{"count": len(fields), "fields": fields})
		})

	addRead(srv, "get_document_fields", "Параметри документа",
		"The extra parameters set on one document, with their values and when each was last changed.",
		func(ctx context.Context, _ *mcp.CallToolRequest, in documentIDIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			fields, err := d.api(ctx).GetDocumentFields(ctx, in.ID)
			if err != nil {
				return fail(err)
			}
			return ok(map[string]any{"document_id": in.ID, "count": len(fields), "fields": fields})
		})

	addRead(srv, "list_document_categories", "Типи документів",
		"Every document type available to the company: the public ones shared by all of Vchasno and the private ones this company defined itself (is_public=false, usable on internal documents only). These ids are what the category filter and the upload tools want.",
		func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			cats, err := d.api(ctx).ListCategories(ctx)
			if err != nil {
				return fail(err)
			}
			sort.Slice(cats, func(i, j int) bool { return cats[i].CategoryID < cats[j].CategoryID })
			return ok(map[string]any{"count": len(cats), "categories": cats})
		})

	addRead(srv, "list_groups", "Команди",
		"Teams of the company. A team can be added to a document's signers or approvers in one step instead of naming everyone. Pass a team id or name to read just that one.",
		func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
			Group string `json:"group,omitempty" jsonschema:"Team id or name; omit to list them all"`
		}) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if strings.TrimSpace(in.Group) != "" {
				id, err := d.Sess.ResolveGroupID(ctx, in.Group)
				if err != nil {
					return fail(err)
				}
				g, err := d.api(ctx).GetGroup(ctx, id)
				if err != nil {
					return fail(err)
				}
				return ok(g)
			}
			groups, err := d.Sess.Groups(ctx)
			if err != nil {
				return fail(err)
			}
			return ok(map[string]any{"count": len(groups), "teams": groups})
		})

	addRead(srv, "list_scenarios", "Сценарії",
		"Company scenarios — reusable routes that fill in approvers, signers, viewers, labels and extra parameters when a document is uploaded with scenario_id. Pass an id to get just that one.",
		func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
			ID string `json:"id,omitempty" jsonschema:"Scenario id; omit to list them all"`
		}) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if in.ID != "" {
				s, err := d.api(ctx).GetScenario(ctx, in.ID)
				if err != nil {
					return fail(err)
				}
				return ok(s)
			}
			list, err := d.api(ctx).ListScenarios(ctx)
			if err != nil {
				return fail(err)
			}
			return ok(map[string]any{"count": len(list), "scenarios": list})
		})

	addRead(srv, "list_document_templates", "Шаблони документів",
		"File templates of the company — PDF forms and DOCX files with {{placeholders}} — that create_document_from_template turns into documents.",
		func(ctx context.Context, _ *mcp.CallToolRequest, in templatesIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			list, err := d.api(ctx).ListDocumentTemplates(ctx, in.Edrpou, in.SharingType, in.Limit, in.Cursor)
			if err != nil {
				return fail(err)
			}
			return ok(map[string]any{"count": len(list.Templates), "cursor": list.Cursor, "templates": list.Templates})
		})

	addRead(srv, "get_document_template", "Поля шаблону",
		"One file template with the list of fields to fill: their ids (the keys a PDF template wants), names, hints, types and, for select fields, the allowed values. Read this before create_document_from_template.",
		func(ctx context.Context, _ *mcp.CallToolRequest, in templateIDIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			t, err := d.api(ctx).GetDocumentTemplate(ctx, in.ID)
			if err != nil {
				return fail(err)
			}
			return ok(t)
		})

	addRead(srv, "check_counterparty", "Перевірити контрагента",
		"Check whether a counterparty is registered in Vchasno — worth doing before sending them anything, since an unregistered company has to be invited first. Takes one ЄДРПОУ/ІПН, or an .xlsx/.csv with up to ten of them.",
		func(ctx context.Context, _ *mcp.CallToolRequest, in checkCompanyIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			switch {
			case in.File != nil:
				name, content, err := d.readFile(*in.File)
				if err != nil {
					return fail(err)
				}
				batch, err := d.api(ctx).CheckCompaniesFile(ctx, name, content)
				if err != nil {
					return fail(err)
				}
				return ok(batch)
			case strings.TrimSpace(in.Edrpou) != "":
				res, err := d.api(ctx).CheckCompany(ctx, strings.TrimSpace(in.Edrpou))
				if err != nil {
					return fail(err)
				}
				return ok(res)
			}
			return failf("pass either edrpou or file")
		})

	if d.Cfg.ReadOnly {
		return
	}

	mcp.AddTool(srv, &mcp.Tool{Name: "tag_documents", Annotations: writes("Ярлики документів"),
		Description: "Put labels on documents or take them off. action=create makes brand-new labels from names, action=assign attaches existing ones, action=unassign detaches them. Labels may be given by name — they are resolved to ids. Note that attaching a label also grants access to everyone carrying that label."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in tagDocumentsIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if len(in.DocumentIDs) == 0 {
				return failf("document_ids is required")
			}
			action := in.Action
			if action == "" {
				if len(in.Names) > 0 {
					action = "create"
				} else {
					action = "assign"
				}
			}
			defer d.Sess.Invalidate()
			switch action {
			case "create":
				if len(in.Names) == 0 {
					return failf("action=create needs names")
				}
				tags, err := d.api(ctx).CreateDocumentTags(ctx, in.DocumentIDs, in.Names)
				if err != nil {
					return fail(err)
				}
				return ok(map[string]any{"ok": true, "action": action, "tags": tags})
			case "assign", "unassign":
				ids, err := d.Sess.ResolveTagIDs(ctx, in.Tags)
				if err != nil {
					return fail(err)
				}
				if len(ids) == 0 {
					return failf("action=%s needs tags", action)
				}
				if action == "assign" {
					err = d.api(ctx).AssignTagsToDocuments(ctx, in.DocumentIDs, ids)
				} else {
					err = d.api(ctx).UnassignTagsFromDocuments(ctx, in.DocumentIDs, ids)
				}
				if err != nil {
					return fail(err)
				}
				return done("labels "+action+"ed", map[string]any{"documents": len(in.DocumentIDs), "tags": ids})
			}
			return failf("action must be create, assign or unassign")
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "tag_employees", Annotations: writes("Ярлики співробітників"),
		Description: "Put labels on employees or take them off. An employee carrying a label sees every document with that label, so this both creates and removes access — it needs administrator rights, and the person doing it should mean it."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in tagRolesIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			roles, err := d.Sess.ResolveRoleIDs(ctx, in.People)
			if err != nil {
				return fail(err)
			}
			if len(roles) == 0 {
				return failf("people is required")
			}
			action := in.Action
			if action == "" {
				if len(in.Names) > 0 {
					action = "create"
				} else {
					action = "assign"
				}
			}
			defer d.Sess.Invalidate()
			switch action {
			case "create":
				if len(in.Names) == 0 {
					return failf("action=create needs names")
				}
				tags, cerr := d.api(ctx).CreateRoleTags(ctx, roles, in.Names)
				if cerr != nil {
					return fail(cerr)
				}
				return ok(map[string]any{"ok": true, "action": action, "tags": tags})
			case "assign", "unassign":
				ids, terr := d.Sess.ResolveTagIDs(ctx, in.Tags)
				if terr != nil {
					return fail(terr)
				}
				if len(ids) == 0 {
					return failf("action=%s needs tags", action)
				}
				if action == "assign" {
					terr = d.api(ctx).AssignTagsToRoles(ctx, roles, ids)
				} else {
					terr = d.api(ctx).UnassignTagsFromRoles(ctx, roles, ids)
				}
				if terr != nil {
					return fail(terr)
				}
				return done("labels "+action+"ed", map[string]any{"people": roles, "tags": ids})
			}
			return failf("action must be create, assign or unassign")
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "create_field", Annotations: writes("Створити параметр"),
		Description: "Define a new extra document parameter for the company: text, number, date or enum (a list of options)."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in createFieldIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			switch in.Type {
			case "text", "number", "date", "enum":
			default:
				return failf("type must be text, number, date or enum")
			}
			f, err := d.api(ctx).CreateField(ctx, in.Name, in.Type, in.IsRequired)
			if err != nil {
				return fail(err)
			}
			d.Sess.Invalidate()
			return ok(map[string]any{"ok": true, "field": f})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "update_field", Annotations: writes("Змінити параметр"),
		Description: "Rename an extra parameter and, for an enum, replace its list of options. Passing enum_options replaces the whole list; leaving it out keeps what is there. The type and the required flag cannot be changed this way."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in updateFieldIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			id, err := d.Sess.ResolveFieldID(ctx, in.Field)
			if err != nil {
				return fail(err)
			}
			if err := d.api(ctx).UpdateField(ctx, id, in.Name, in.EnumOptions); err != nil {
				return fail(err)
			}
			d.Sess.Invalidate()
			return done("field updated", map[string]any{"field_id": id, "name": in.Name})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "set_document_field", Annotations: writes("Значення параметра"),
		Description: "Set the value of an extra parameter on a document. The parameter may be named instead of given by id."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in documentFieldIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			id, err := d.Sess.ResolveFieldID(ctx, in.Field)
			if err != nil {
				return fail(err)
			}
			if err := d.api(ctx).SetDocumentField(ctx, in.DocumentID, id, in.Value, in.IsRequired); err != nil {
				return fail(err)
			}
			return done("field set", map[string]any{"document_id": in.DocumentID, "field_id": id, "value": in.Value})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "create_document_category", Annotations: writes("Створити тип документа"),
		Description: "Define a document type of your own. Company types are private: they can be used on internal documents only."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in categoryIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			res, err := d.api(ctx).CreateCategory(ctx, in.Title)
			if err != nil {
				return fail(err)
			}
			d.Sess.Invalidate()
			return ok(map[string]any{"ok": true, "category": res})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "rename_document_category", Annotations: writes("Перейменувати тип документа"),
		Description: "Rename one of your company's own document types. Public types cannot be renamed."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in categoryEditIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if in.Title == "" {
				return failf("title is required")
			}
			if err := d.api(ctx).RenameCategory(ctx, in.CategoryID, in.Title); err != nil {
				return fail(err)
			}
			d.Sess.Invalidate()
			return done("category renamed", map[string]any{"category_id": in.CategoryID, "title": in.Title})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "delete_document_category", Annotations: destructive("Видалити тип документа"),
		Description: "Delete one of your company's own document types. Irreversible — requires confirm=true."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in categoryEditIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if err := confirmed(in.Confirm, "deleting a document type"); err != nil {
				return fail(err)
			}
			if err := d.api(ctx).DeleteCategory(ctx, in.CategoryID); err != nil {
				return fail(err)
			}
			d.Sess.Invalidate()
			return done("category deleted", map[string]any{"category_id": in.CategoryID})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "create_group", Annotations: writes("Створити команду"),
		Description: "Create a team of employees. Teams can be assigned as signers or approvers of a document in one step."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in createGroupIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			g, err := d.api(ctx).CreateGroup(ctx, in.Name)
			if err != nil {
				return fail(err)
			}
			d.Sess.Invalidate()
			return ok(map[string]any{"ok": true, "team": g})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "rename_group", Annotations: writes("Перейменувати команду"),
		Description: "Change a team's name."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in renameGroupIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			id, err := d.Sess.ResolveGroupID(ctx, in.Group)
			if err != nil {
				return fail(err)
			}
			g, err := d.api(ctx).RenameGroup(ctx, id, in.Name)
			if err != nil {
				return fail(err)
			}
			d.Sess.Invalidate()
			return ok(map[string]any{"ok": true, "team": g})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "delete_group", Annotations: destructive("Видалити команду"),
		Description: "Delete a team. Its members keep their own accounts, but every place the team was used as a signer, approver or viewer loses it. Irreversible — requires confirm=true."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
			Group   string `json:"group" jsonschema:"Team id or name"`
			Confirm bool   `json:"confirm,omitempty" jsonschema:"Must be true"`
		}) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if err := confirmed(in.Confirm, "deleting a team"); err != nil {
				return fail(err)
			}
			id, err := d.Sess.ResolveGroupID(ctx, in.Group)
			if err != nil {
				return fail(err)
			}
			if err := d.api(ctx).DeleteGroup(ctx, id); err != nil {
				return fail(err)
			}
			d.Sess.Invalidate()
			return done("team deleted", map[string]any{"group_id": id})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "group_members", Annotations: writes("Учасники команди"),
		Description: "List, add or remove the members of a team. Adding takes employees by email or role id; removing takes the member ids that action=list returns — a member id is not a role id."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in groupMembersIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			id, err := d.Sess.ResolveGroupID(ctx, in.Group)
			if err != nil {
				return fail(err)
			}
			switch in.Action {
			case "", "list":
				members, lerr := d.api(ctx).ListGroupMembers(ctx, id)
				if lerr != nil {
					return fail(lerr)
				}
				return ok(map[string]any{"group_id": id, "count": len(members), "members": members})
			case "add":
				roles, rerr := d.Sess.ResolveRoleIDs(ctx, in.People)
				if rerr != nil {
					return fail(rerr)
				}
				if len(roles) == 0 {
					return failf("action=add needs people")
				}
				members, aerr := d.api(ctx).AddGroupMembers(ctx, id, roles)
				if aerr != nil {
					return fail(aerr)
				}
				return ok(map[string]any{"ok": true, "group_id": id, "members": members})
			case "remove":
				if len(in.People) == 0 {
					return failf("action=remove needs the member ids from action=list")
				}
				if rerr := d.api(ctx).RemoveGroupMembers(ctx, id, in.People); rerr != nil {
					return fail(rerr)
				}
				return done("members removed", map[string]any{"group_id": id, "members": in.People})
			}
			return failf("action must be list, add or remove")
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "create_document_from_template", Annotations: writes("Документ із шаблону"),
		Description: "Create a document from a file template. For a PDF template the keys of fields are the field ids from get_document_template; for a DOCX template they are the placeholder names without braces ({{contract_number}} → contract_number). The result is an ordinary document: give it a counterparty and send it as usual."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in createFromTemplateIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			body := map[string]any{}
			if in.Title != "" {
				body["title"] = in.Title
			}
			if in.Number != "" {
				body["number"] = in.Number
			}
			if in.DateDocument != "" {
				body["date_document"] = in.DateDocument
			}
			if in.Category != "" {
				if n, err := atoiSafe(in.Category); err == nil {
					body["category_id"] = n
				} else if id, found := d.Sess.ResolveCategoryID(ctx, in.Category); found {
					body["category_id"] = id
				} else {
					return failf("unknown document type %q", in.Category)
				}
			}
			if in.Amount != "" {
				kop, err := amountPtr(in.Amount)
				if err != nil {
					return fail(err)
				}
				body["amount"] = float64(*kop) / 100
			}
			if len(in.Fields) > 0 {
				extra := make(map[string]any, len(in.Fields))
				for k, v := range in.Fields {
					extra[k] = v
				}
				body["extra_fields"] = extra
			}
			res, err := d.api(ctx).CreateDocumentFromTemplate(ctx, in.TemplateID, body)
			if err != nil {
				return fail(err)
			}
			return ok(map[string]any{"ok": true, "document": res,
				"next_step": "set_document_recipient, then sign and send_document"})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "invite_coworkers", Annotations: writes("Запросити співробітників"),
		Description: "Send registration invitations by email. Each recipient signs up and joins the company themselves. Requires the invite permission."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in inviteIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if len(in.Emails) == 0 {
				return failf("emails is required")
			}
			if err := d.api(ctx).InviteCoworkers(ctx, in.Emails); err != nil {
				return fail(err)
			}
			d.Sess.Invalidate()
			return done("invitations sent", map[string]any{"emails": in.Emails})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "create_coworker", Annotations: writes("Створити співробітника"),
		Description: "Create a ready employee account: Vchasno mails the person their login and password instead of an invitation. The email must be in the company's verified corporate domain and must not already exist in Vchasno."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in createCoworkerIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			body := map[string]any{"email": in.Email}
			for k, v := range map[string]string{"first_name": in.FirstName, "second_name": in.SecondName, "last_name": in.LastName, "phone": in.Phone} {
				if v != "" {
					body[k] = v
				}
			}
			res, err := d.api(ctx).CreateCoworker(ctx, body)
			if err != nil {
				return fail(err)
			}
			d.Sess.Invalidate()
			return ok(map[string]any{"ok": true, "coworker": res})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "update_role", Annotations: writes("Права співробітника"),
		Description: "Change an employee's position, permissions, notification settings, IP restrictions, or restore a removed account with status=active. Making somebody an administrator switches every permission on at once, so both is_admin and any permission change need confirm=true. The full flag list is in the vchasno://guide/permissions resource."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in updateRoleIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			id, err := d.Sess.ResolveRoleID(ctx, in.Person)
			if err != nil {
				return fail(err)
			}
			patch := map[string]any{}
			if in.Position != "" {
				patch["position"] = in.Position
			}
			if in.Status != "" {
				patch["status"] = in.Status
			}
			if in.AllowedIPs != nil {
				patch["allowed_ips"] = in.AllowedIPs
			}
			if in.AllowedAPIIPs != nil {
				patch["allowed_api_ips"] = in.AllowedAPIIPs
			}
			widens := false
			if in.IsAdmin != "" {
				admin, berr := boolPtr(in.IsAdmin)
				if berr != nil {
					return failf("is_admin: %v", berr)
				}
				if *admin {
					patch["user_role"] = 8001
					widens = true
				} else {
					patch["user_role"] = 8000
				}
			}
			for k, v := range in.Permissions {
				patch[k] = v
				if v {
					widens = true
				}
			}
			for k, v := range in.Notifications {
				patch[k] = v
			}
			if len(patch) == 0 {
				return failf("nothing to change")
			}
			if widens {
				if err := confirmed(in.Confirm, "granting an employee wider permissions"); err != nil {
					return fail(err)
				}
			}
			if err := d.api(ctx).UpdateRole(ctx, id, patch); err != nil {
				return fail(err)
			}
			d.Sess.Invalidate()
			return done("role updated", map[string]any{"role_id": id, "changed": keysOf(patch)})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "delete_role", Annotations: destructive("Видалити співробітника"),
		Description: "Remove an employee account from the company. The person loses access to every document at once. Irreversible from here (Vchasno can restore an account with update_role status=active, but only while it is recoverable) — requires confirm=true."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in personIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if err := confirmed(in.Confirm, "removing an employee account"); err != nil {
				return fail(err)
			}
			id, err := d.Sess.ResolveRoleID(ctx, in.Person)
			if err != nil {
				return fail(err)
			}
			if err := d.api(ctx).DeleteRole(ctx, id); err != nil {
				return fail(err)
			}
			d.Sess.Invalidate()
			return done("role deleted", map[string]any{"role_id": id})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "create_user_tokens", Annotations: writes("Видати токени"),
		Description: "Issue integration tokens for employees, optionally with a lifetime in days. Administrator rights required. The tokens come back in the answer — hand them over through a safe channel and do not store them anywhere else."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in tokensIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if len(in.Emails) == 0 {
				return failf("emails is required")
			}
			res, err := d.api(ctx).CreateUserTokens(ctx, in.Emails, in.ExpireDays)
			if err != nil {
				return fail(err)
			}
			return ok(map[string]any{"ok": true, "result": res,
				"warning": "these tokens grant full API access on behalf of those employees; treat them like passwords"})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "reset_user_tokens", Annotations: destructive("Скинути токени"),
		Description: "Invalidate the integration tokens of the given employees. Every integration using them — possibly including this MCP server — stops working immediately. Requires confirm=true."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in tokensIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if err := confirmed(in.Confirm, "resetting integration tokens"); err != nil {
				return fail(err)
			}
			if err := d.api(ctx).ResetUserTokens(ctx, in.Emails); err != nil {
				return fail(err)
			}
			return done("tokens reset", map[string]any{"emails": in.Emails})
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "activate_integration_trial", Annotations: destructive("Активувати тестовий тариф"),
		Description: "Turn on the 30-day trial of the Integration tariff, which opens the API for this company. It can be activated exactly once per company, ever, so this is not something to try speculatively — requires confirm=true."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in trialIn) (*mcp.CallToolResult, any, error) {
			if err := confirmed(in.Confirm, "activating the one-time Integration trial"); err != nil {
				return fail(err)
			}
			res, err := d.api(ctx).ActivateIntegrationTrial(ctx)
			if err != nil {
				return fail(err)
			}
			d.Sess.APIOpen = true
			d.Sess.Invalidate()
			return ok(map[string]any{"ok": true, "result": res, "note": "the API is open for 30 days from now"})
		})
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
