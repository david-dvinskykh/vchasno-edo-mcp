package mockvchasno

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// route dispatches one request. It returns false when nothing matched, so
// ServeHTTP can answer 404 the way the real service does.
//
//nolint:gocyclo // a routing table is long by nature; splitting it hides the map
func (s *Server) route(w http.ResponseWriter, r *http.Request, path string) bool {
	q := r.URL.Query()
	m := r.Method
	seg := strings.Split(strings.TrimPrefix(path, "/api/v2/"), "/")

	switch {
	case path == "/api/v2/company/billing" && m == http.MethodGet:
		writeJSON(w, 200, map[string]any{
			"rates": []map[string]any{{"id": "rate-1", "name": "Інтеграція", "type": "integration", "start_date": "2026-01-15T10:00:00", "end_date": nil}},
			"limits": map[string]any{
				"documents_sent":        map[string]any{"used": 120, "limit": 500, "is_unlimited": false},
				"documents_view":        map[string]any{"active": map[string]any{"used": 45, "limit": 100, "is_unlimited": false}, "archive": map[string]any{"used": 10, "limit": 0, "is_unlimited": false}},
				"employees":             map[string]any{"used": 3, "limit": 10, "is_unlimited": false},
				"integration_documents": map[string]any{"main": map[string]any{"used": 30, "limit": 1000, "is_unlimited": false}, "bonus": map[string]any{"used": 0, "limit": 100, "is_unlimited": false}},
			}})
		return true

	case path == "/api/v2/billing/companies/rates/trials" && m == http.MethodPost:
		s.opt.NoTariff = false
		writeJSON(w, 201, map[string]any{"rate": "integration_trial", "days": 30})
		return true

	case path == "/api/v2/roles" && m == http.MethodGet:
		writeJSON(w, 200, map[string]any{"roles": s.roles})
		return true

	case len(seg) == 2 && seg[0] == "roles" && m == http.MethodPatch:
		patch := body(r)
		for _, role := range s.roles {
			if role["id"] == seg[1] {
				for k, v := range patch {
					role[k] = v
				}
				writeJSON(w, 200, nil)
				return true
			}
		}
		apiError(w, 404, "", "no such role")
		return true

	case len(seg) == 2 && seg[0] == "roles" && m == http.MethodDelete:
		kept := s.roles[:0]
		found := false
		for _, role := range s.roles {
			if role["id"] == seg[1] {
				found = true
				continue
			}
			kept = append(kept, role)
		}
		s.roles = kept
		if !found {
			apiError(w, 404, "", "no such role")
			return true
		}
		writeJSON(w, 200, nil)
		return true

	case path == "/api/v2/invite/coworkers" && m == http.MethodPost:
		writeJSON(w, 200, nil)
		return true

	case path == "/api/v2/coworker" && m == http.MethodPost:
		b := body(r)
		id := s.next("role")
		s.roles = append(s.roles, map[string]any{"id": id, "status": "active", "email": b["email"], "position": ""})
		writeJSON(w, 201, map[string]any{"id": id, "email": b["email"]})
		return true

	case path == "/api/v2/tokens" && m == http.MethodPost:
		b := body(r)
		out := map[string]any{}
		for _, e := range strs(b["emails"]) {
			out[e] = s.next("token")
		}
		writeJSON(w, 200, map[string]any{"tokens": out})
		return true

	case path == "/api/v2/tokens" && m == http.MethodDelete:
		writeJSON(w, 200, nil)
		return true

	// ── documents ───────────────────────────────────────────────

	case path == "/api/v2/documents" && m == http.MethodGet:
		s.listDocuments(w, q, s.documents, true)
		return true

	case path == "/api/v2/incoming-documents" && m == http.MethodGet:
		s.listDocuments(w, q, s.incoming, false)
		return true

	case path == "/api/v2/documents" && m == http.MethodPost:
		s.upload(w, r, q)
		return true

	case path == "/api/v2/documents/statuses" && m == http.MethodPost:
		ids := strs(body(r)["document_ids"])
		rows := []map[string]any{}
		for _, id := range ids {
			if d := s.find(id); d != nil {
				rows = append(rows, map[string]any{"document_id": id, "status_id": d.Status, "status_text": statusText(d.Status)})
			}
		}
		writeJSON(w, 200, map[string]any{"data_list": rows})
		return true

	case path == "/api/v2/documents/mark-as-processed" && m == http.MethodPost:
		ids := strs(body(r)["document_ids"])
		updated := []string{}
		for _, id := range ids {
			if d := s.find(id); d != nil {
				d.Processed = true
				updated = append(updated, id)
			}
		}
		writeJSON(w, 200, map[string]any{"updated_ids": updated})
		return true

	case path == "/api/v2/documents/archive" && m == http.MethodPost:
		for _, id := range strs(body(r)["document_ids"]) {
			s.archived[id] = true
		}
		writeJSON(w, 200, nil)
		return true

	case path == "/api/v2/documents/archive" && m == http.MethodDelete:
		for _, id := range strs(body(r)["document_ids"]) {
			delete(s.archived, id)
		}
		writeJSON(w, 200, nil)
		return true

	case path == "/api/v2/documents/delete-requests" && m == http.MethodGet:
		out := []map[string]any{}
		for _, dr := range s.deleteReqs {
			if st := q.Get("status"); st != "" && dr.Status != st {
				continue
			}
			out = append(out, dr.json())
		}
		sort.Slice(out, func(i, j int) bool { return fmt.Sprint(out[i]["id"]) < fmt.Sprint(out[j]["id"]) })
		writeJSON(w, 200, out)
		return true

	case path == "/api/v2/documents/delete-requests/lock-delete":
		ids := strs(body(r)["document_ids"])
		writeJSON(w, 200, map[string]any{"updated_ids": ids})
		return true

	case path == "/api/v2/documents/structured-data/extractions" && m == http.MethodPost:
		out := []map[string]any{}
		for _, id := range strs(body(r)["document_ids"]) {
			s.extracts[id] = &extraction{DocumentID: id, Status: "pending", Updated: time.Now().Format(time.RFC3339)}
			out = append(out, s.extracts[id].json())
		}
		writeJSON(w, 200, map[string]any{"data": out})
		return true

	case path == "/api/v2/documents/structured-data/extractions" && m == http.MethodGet:
		out := []map[string]any{}
		for _, e := range s.extracts {
			if st := q.Get("status"); st != "" && e.Status != st {
				continue
			}
			out = append(out, e.json())
		}
		sort.Slice(out, func(i, j int) bool { return fmt.Sprint(out[i]["document_id"]) < fmt.Sprint(out[j]["document_id"]) })
		writeJSON(w, 200, map[string]any{"data": out, "next_cursor": nil})
		return true

	case path == "/api/v2/documents/comments" && m == http.MethodGet:
		rows := []map[string]any{}
		for docID, list := range s.comments {
			for _, c := range list {
				rows = append(rows, c.json(docID))
			}
		}
		sort.Slice(rows, func(i, j int) bool { return fmt.Sprint(rows[i]["id"]) < fmt.Sprint(rows[j]["id"]) })
		writeJSON(w, 200, map[string]any{"comments": rows, "next_cursor": nil})
		return true

	case path == "/api/v2/download-documents" && m == http.MethodGet:
		rows := []map[string]any{}
		for _, id := range q["ids"] {
			rows = append(rows, map[string]any{"id": id, "extension": ".pdf",
				"archive_url":  "https://edo.vchasno.ua/download/" + id + ".zip",
				"original_url": "https://edo.vchasno.ua/download/" + id + ".pdf", "status": 7008})
		}
		writeJSON(w, 200, map[string]any{"status": 200, "ready": true, "pending": false, "total": len(rows), "documents": rows})
		return true

	// ── one document ────────────────────────────────────────────

	case len(seg) >= 2 && seg[0] == "documents":
		return s.documentRoute(w, r, seg, q)

	// ── tags ────────────────────────────────────────────────────

	case path == "/api/v2/tags" && m == http.MethodGet:
		rows := []map[string]any{}
		for _, t := range s.tags {
			rows = append(rows, map[string]any{"id": t.ID, "name": t.Name, "date_created": t.Created})
		}
		sort.Slice(rows, func(i, j int) bool { return fmt.Sprint(rows[i]["id"]) < fmt.Sprint(rows[j]["id"]) })
		writeJSON(w, 200, map[string]any{"tags": rows})
		return true

	case path == "/api/v2/tags/documents" && m == http.MethodPost:
		b := body(r)
		created := []map[string]any{}
		for _, name := range strs(b["names"]) {
			t := &tag{ID: s.next("tag"), Name: name, Created: time.Now().Format(time.RFC3339)}
			s.tags[t.ID] = t
			for _, docID := range strs(b["documents_ids"]) {
				s.docTags[docID] = append(s.docTags[docID], t.ID)
			}
			created = append(created, map[string]any{"id": t.ID, "name": t.Name})
		}
		writeJSON(w, 201, created)
		return true

	case path == "/api/v2/tags/documents/connections":
		b := body(r)
		for _, docID := range strs(b["documents_ids"]) {
			for _, tagID := range strs(b["tags_ids"]) {
				if m == http.MethodPost {
					s.docTags[docID] = append(s.docTags[docID], tagID)
				} else {
					s.docTags[docID] = without(s.docTags[docID], tagID)
				}
			}
		}
		writeJSON(w, 200, nil)
		return true

	case path == "/api/v2/tags/roles" && m == http.MethodPost:
		b := body(r)
		created := []map[string]any{}
		for _, name := range strs(b["names"]) {
			t := &tag{ID: s.next("tag"), Name: name, Created: time.Now().Format(time.RFC3339)}
			s.tags[t.ID] = t
			for _, roleID := range strs(b["roles_ids"]) {
				s.roleTags[roleID] = append(s.roleTags[roleID], t.ID)
			}
			created = append(created, map[string]any{"id": t.ID, "name": t.Name})
		}
		writeJSON(w, 201, created)
		return true

	case path == "/api/v2/tags/roles/connections":
		b := body(r)
		for _, roleID := range strs(b["roles_ids"]) {
			for _, tagID := range strs(b["tags_ids"]) {
				if m == http.MethodPost {
					s.roleTags[roleID] = append(s.roleTags[roleID], tagID)
				} else {
					s.roleTags[roleID] = without(s.roleTags[roleID], tagID)
				}
			}
		}
		writeJSON(w, 200, nil)
		return true

	case len(seg) == 3 && seg[0] == "tags" && seg[2] == "roles" && m == http.MethodGet:
		rows := []map[string]any{}
		for roleID, ids := range s.roleTags {
			for _, id := range ids {
				if id == seg[1] {
					rows = append(rows, map[string]any{"role_id": roleID, "tag_id": id, "assigner_id": "role-admin", "date_created": time.Now().Format(time.RFC3339)})
				}
			}
		}
		writeJSON(w, 200, map[string]any{"roles": rows})
		return true

	// ── fields ──────────────────────────────────────────────────

	case path == "/api/v2/fields" && m == http.MethodGet:
		rows := []map[string]any{}
		for _, f := range s.fields {
			rows = append(rows, map[string]any{"id": f.ID, "name": f.Name, "type": f.Type, "is_required": f.Required})
		}
		sort.Slice(rows, func(i, j int) bool { return fmt.Sprint(rows[i]["id"]) < fmt.Sprint(rows[j]["id"]) })
		writeJSON(w, 200, rows)
		return true

	case path == "/api/v2/fields" && m == http.MethodPost:
		b := body(r)
		f := &field{ID: s.next("field"), Name: fmt.Sprint(b["name"]), Type: fmt.Sprint(b["field_type"])}
		if req, isBool := b["is_required"].(bool); isBool {
			f.Required = req
		}
		s.fields[f.ID] = f
		writeJSON(w, 201, map[string]any{"id": f.ID, "name": f.Name, "type": f.Type, "is_required": f.Required, "company_id": "company-1", "created_by": "role-admin", "order": "1"})
		return true

	case len(seg) == 2 && seg[0] == "fields" && m == http.MethodPatch:
		f := s.fields[seg[1]]
		if f == nil {
			apiError(w, 404, "", "no such field")
			return true
		}
		b := body(r)
		f.Name = fmt.Sprint(b["name"])
		if opts, present := b["enum_options"]; present {
			f.Options = strs(opts)
		}
		writeJSON(w, 200, nil)
		return true

	// ── categories ──────────────────────────────────────────────

	case path == "/api/v2/document-categories" && m == http.MethodGet:
		rows := []map[string]any{}
		for _, c := range s.categories {
			rows = append(rows, map[string]any{"category_id": c.ID, "category_title": c.Title, "is_public": c.IsPublic,
				"date_created": "2024-07-25T06:08:34+00:00", "date_updated": "2024-07-25T06:08:34+00:00"})
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i]["category_id"].(int) < rows[j]["category_id"].(int) })
		writeJSON(w, 200, rows)
		return true

	case path == "/api/v2/document-categories" && m == http.MethodPost:
		b := body(r)
		s.counter++
		id := 1000 + s.counter
		s.categories[id] = &category{ID: id, Title: fmt.Sprint(b["title"]), IsPublic: false}
		writeJSON(w, 201, map[string]any{"category_id": id, "category_title": b["title"]})
		return true

	case len(seg) == 2 && seg[0] == "document-categories" && (m == http.MethodPatch || m == http.MethodDelete):
		id, _ := strconv.Atoi(seg[1])
		c := s.categories[id]
		if c == nil {
			apiError(w, 404, "", "no such category")
			return true
		}
		if m == http.MethodPatch {
			c.Title = fmt.Sprint(body(r)["title"])
		} else {
			delete(s.categories, id)
		}
		writeJSON(w, 200, nil)
		return true

	// ── groups ──────────────────────────────────────────────────

	case path == "/api/v2/groups" && m == http.MethodGet:
		rows := []map[string]any{}
		for _, g := range s.groups {
			rows = append(rows, g.json())
		}
		sort.Slice(rows, func(i, j int) bool { return fmt.Sprint(rows[i]["id"]) < fmt.Sprint(rows[j]["id"]) })
		writeJSON(w, 200, rows)
		return true

	case path == "/api/v2/groups" && m == http.MethodPost:
		g := &group{ID: s.next("group"), Name: fmt.Sprint(body(r)["name"]), Created: time.Now().Format(time.RFC3339)}
		s.groups[g.ID] = g
		writeJSON(w, 200, g.json())
		return true

	case len(seg) == 2 && seg[0] == "groups":
		g := s.groups[seg[1]]
		if g == nil {
			apiError(w, 404, "", "no such group")
			return true
		}
		switch m {
		case http.MethodGet:
			writeJSON(w, 200, g.json())
		case http.MethodPatch:
			g.Name = fmt.Sprint(body(r)["name"])
			writeJSON(w, 200, g.json())
		case http.MethodDelete:
			delete(s.groups, g.ID)
			writeJSON(w, 204, nil)
		default:
			return false
		}
		return true

	case len(seg) == 3 && seg[0] == "groups" && seg[2] == "members":
		switch m {
		case http.MethodGet:
			writeJSON(w, 200, s.members[seg[1]])
		case http.MethodPost:
			for _, roleID := range strs(body(r)["role_ids"]) {
				s.members[seg[1]] = append(s.members[seg[1]], map[string]any{"id": s.next("member"), "role_id": roleID,
					"group_id": seg[1], "created_by": "role-admin", "date_created": time.Now().Format(time.RFC3339)})
			}
			writeJSON(w, 200, s.members[seg[1]])
		default:
			return false
		}
		return true

	case len(seg) == 4 && seg[0] == "groups" && seg[2] == "members" && seg[3] == "remove" && m == http.MethodPost:
		drop := map[string]bool{}
		for _, id := range strs(body(r)["group_members"]) {
			drop[id] = true
		}
		kept := s.members[seg[1]][:0]
		for _, mem := range s.members[seg[1]] {
			if !drop[fmt.Sprint(mem["id"])] {
				kept = append(kept, mem)
			}
		}
		s.members[seg[1]] = kept
		writeJSON(w, 204, nil)
		return true

	// ── scenarios and templates ─────────────────────────────────

	case path == "/api/v2/templates" && m == http.MethodGet:
		writeJSON(w, 200, []map[string]any{{"id": "scenario-1", "name": "Сценарій 1",
			"review_settings":  map[string]any{"is_required": false, "is_ordered": false, "reviewer_entities": []map[string]any{{"type": "role", "id": "role-buh"}}},
			"signers_settings": map[string]any{"is_ordered": false, "signer_entities": []map[string]any{{"type": "role", "id": "role-admin"}}},
			"tags_settings":    map[string]any{"tags": []string{"Термінові"}},
			"created_by":       "role-admin", "date_created": "2026-02-14T09:39:32+00:00", "date_updated": "2026-02-14T09:39:32+00:00"}})
		return true

	case len(seg) == 2 && seg[0] == "templates" && m == http.MethodGet:
		writeJSON(w, 200, map[string]any{"id": seg[1], "name": "Сценарій 1"})
		return true

	case path == "/api/v2/document-templates" && m == http.MethodGet:
		writeJSON(w, 200, map[string]any{"templates": []map[string]any{
			{"id": "tpl-1", "title": "Договір надання послуг", "extension": ".docx", "sharing_type": "private"}}, "cursor": nil})
		return true

	case len(seg) == 2 && seg[0] == "document-templates" && m == http.MethodGet:
		writeJSON(w, 200, map[string]any{"id": seg[1], "title": "Договір надання послуг", "extension": ".docx", "sharing_type": "private",
			"fields": []map[string]any{{"id": "tf-1", "name": "ПІБ контрагента", "description": "Повне ім'я", "required": true, "type": "text", "options": nil}}})
		return true

	case len(seg) == 3 && seg[0] == "document-templates" && seg[2] == "document" && m == http.MethodPost:
		id := s.next("doc")
		b := body(r)
		title := "Договір надання послуг"
		if t, present := b["title"].(string); present && t != "" {
			title = t
		}
		s.documents[id] = &doc{ID: id, Status: 7000, Title: title, Extension: ".docx", Owner: "12345678",
			Created: time.Now(), Changed: time.Now(), Access: "extended", SignaturesTo: 2, FirstSignBy: "owner"}
		writeJSON(w, 201, map[string]any{"id": id})
		return true

	// ── archive ─────────────────────────────────────────────────

	case path == "/api/v2/archive/directories" && m == http.MethodGet:
		writeJSON(w, 200, map[string]any{"directories": []map[string]any{
			{"id": 11, "parent_id": nil, "name": "Папка 1", "date_created": "2026-03-18T13:38:31+00:00"}}, "next_cursor": nil})
		return true

	case path == "/api/v2/archive/scans" && m == http.MethodPost:
		_ = r.ParseMultipartForm(32 << 20)
		id := s.next("scan")
		s.documents[id] = &doc{ID: id, Status: 7000, Title: "Скан", Extension: ".pdf", Owner: "12345678",
			Created: time.Now(), Changed: time.Now(), Access: "extended"}
		s.archived[id] = true
		writeJSON(w, 201, map[string]any{"documents": []map[string]any{{"id": id}}})
		return true

	case path == "/api/v2/archive/import-signed" && m == http.MethodPost:
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			apiError(w, 400, "", "bad multipart body")
			return true
		}
		id := s.next("doc")
		title := r.FormValue("title")
		if title == "" {
			title = "Імпортований підписаний документ"
		}
		s.documents[id] = &doc{ID: id, Status: 7008, Title: title, Extension: ".pdf", Owner: "12345678",
			Created: time.Now(), Changed: time.Now(), Access: "extended"}
		s.archived[id] = true
		sigCount := 1
		if r.MultipartForm != nil {
			if files := r.MultipartForm.File["signatures"]; len(files) > 0 {
				sigCount = len(files)
			}
		}
		writeJSON(w, 201, map[string]any{"document_id": id, "signature_count": sigCount, "counterparty_count": 1,
			"has_pdf_visualization": r.MultipartForm != nil && len(r.MultipartForm.File["pdf_visualization"]) > 0,
			"apply_vchasno_stamps":  r.FormValue("apply_vchasno_stamps") != "false"})
		return true

	case len(seg) == 4 && seg[0] == "archive" && seg[1] == "import-signed" && seg[3] == "visualization" && m == http.MethodPost:
		_ = r.ParseMultipartForm(32 << 20)
		writeJSON(w, 200, map[string]any{"has_pdf_visualization": true, "apply_vchasno_stamps": r.FormValue("apply_vchasno_stamps") != "false"})
		return true

	// ── public links ────────────────────────────────────────────

	case len(seg) == 2 && seg[0] == "shared-documents":
		return s.sharedRoute(w, r, seg[1], m)

	// ── sessions and cloud signing ──────────────────────────────

	case path == "/api/v2/sign-sessions" && m == http.MethodPost:
		b := body(r)
		id := s.next("session")
		writeJSON(w, 201, map[string]any{"id": id, "document_id": b["document_id"], "email": b["email"], "edrpou": b["edrpou"],
			"type": b["type"], "status": "active", "url": "https://edo.vchasno.ua/sign/" + id, "created_by": "role-admin",
			"role_id": "role-admin", "vendor": "API", "is_legal": true,
			"on_cancel_url": b["on_cancel_url"], "on_finish_url": b["on_finish_url"]})
		return true

	case path == "/api/v2/cloud-signer/sessions/create" && m == http.MethodPost:
		writeJSON(w, 200, map[string]any{"authSessionId": s.next("auth"), "isMobileLogged": true})
		return true

	case path == "/api/v2/cloud-signer/sessions/check" && m == http.MethodPost:
		writeJSON(w, 200, map[string]any{"status": "ready", "token": "cloud-session-token"})
		return true

	case path == "/api/v2/cloud-signer/sessions/refresh/check" && m == http.MethodPost:
		writeJSON(w, 200, map[string]any{"status": "ready", "accessToken": "cloud-access", "refreshToken": "cloud-refresh", "expiresIn": 3600})
		return true

	case path == "/api/v2/cloud-signer/sessions/refresh" && m == http.MethodPost:
		writeJSON(w, 200, map[string]any{"status": "ready", "accessToken": "cloud-access-2", "refreshToken": "cloud-refresh-2", "expiresIn": 3600})
		return true

	case path == "/api/v2/cloud-signer/sessions/sign-document" && m == http.MethodPost:
		b := body(r)
		if d := s.find(fmt.Sprint(b["document_id"])); d != nil {
			s.signatures[d.ID] = append(s.signatures[d.ID], map[string]any{"id": s.next("sig"), "edrpou": "12345678",
				"company_name": "ТОВ Наша", "signer_name": "Директор", "timestamp": time.Now().Format(time.RFC3339), "is_internal": false})
			d.Status = 7004
			d.Changed = time.Now()
		}
		writeJSON(w, 201, map[string]any{"ok": true})
		return true

	// ── reports ─────────────────────────────────────────────────

	case (path == "/api/v2/document-actions/request-report" || path == "/api/v2/user-actions/request-report") && m == http.MethodPost:
		id := s.next("report")
		s.reports[id] = &report{ID: id, Status: "pending", Filename: "actions_" + id + ".xlsx", Ready: time.Now()}
		writeJSON(w, 201, map[string]any{"report_id": id})
		return true

	case len(seg) == 3 && seg[0] == "actions" && seg[1] == "report-status":
		rep := s.reports[seg[2]]
		if rep == nil {
			writeJSON(w, 200, map[string]any{"status": "not_found"})
			return true
		}
		rep.Status = "ready"
		writeJSON(w, 200, map[string]any{"status": rep.Status, "filename": rep.Filename})
		return true

	case len(seg) == 3 && seg[0] == "actions" && seg[1] == "download-report":
		rep := s.reports[seg[2]]
		if rep == nil {
			apiError(w, 404, "", "no such report")
			return true
		}
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		w.Header().Set("Content-Disposition", `attachment; filename="`+rep.Filename+`"`)
		_, _ = w.Write([]byte("PK\x03\x04 fake xlsx"))
		return true

	// ── counterparty checks ─────────────────────────────────────

	case path == "/api/v2/check/company" && m == http.MethodPost:
		edrpou := fmt.Sprint(body(r)["edrpou"])
		writeJSON(w, 200, map[string]any{"edrpou": edrpou, "name": "ТОВ Контрагент", "is_registered": edrpou != "00000000"})
		return true

	case path == "/api/v2/check/company/upload" && m == http.MethodPost:
		_ = r.ParseMultipartForm(8 << 20)
		writeJSON(w, 200, map[string]any{"companies": []map[string]any{{"edrpou": "87654321", "name": "ТОВ Контрагент", "is_registered": true}},
			"percentage": "100", "invalid_row_numbers": []any{}, "rows_invalid": "0", "rows_total": "1"})
		return true
	}
	return false
}

func (dr *deleteReq) json() map[string]any {
	return map[string]any{"id": dr.ID, "document_id": dr.DocumentID, "message": dr.Message, "initiator_role_id": "role-admin",
		"reject_message": dr.RejectMessage, "receiver_edrpou": dr.Receiver, "status": dr.Status,
		"date_created": dr.Created, "date_accepted": nil, "date_rejected": nil, "cursor": dr.ID}
}

func (c comment) json(docID string) map[string]any {
	return map[string]any{"id": c.ID, "text": c.Text, "document_id": docID, "date_created": c.Created,
		"email": c.Email, "edrpou": "12345678", "is_internal": c.Internal, "type": c.Type}
}

func (e *extraction) json() map[string]any {
	return map[string]any{"document_id": e.DocumentID, "version_id": nil, "status": e.Status,
		"date_updated": e.Updated, "error_message": nil, "skipped_reason": nil}
}

func (g *group) json() map[string]any {
	return map[string]any{"id": g.ID, "name": g.Name, "created_by": "role-admin", "date_created": g.Created, "date_updated": g.Created}
}

func without(list []string, v string) []string {
	out := list[:0]
	for _, s := range list {
		if s != v {
			out = append(out, s)
		}
	}
	return out
}

func (s *Server) find(id string) *doc {
	if d, ok := s.documents[id]; ok {
		return d
	}
	return s.incoming[id]
}

func (s *Server) sharedRoute(w http.ResponseWriter, r *http.Request, id, m string) bool {
	switch m {
	case http.MethodGet:
		linkID, ok := s.linkByDoc[id]
		if !ok {
			apiError(w, 404, "", "no public link for this document")
			return true
		}
		writeJSON(w, 200, s.links[linkID].json())
		return true
	case http.MethodPost:
		b := body(r)
		period, _ := asInt(b["access_period"])
		l := &sharedLink{ID: s.next("link"), DocumentID: id, Type: fmt.Sprint(b["type"]), Active: true,
			AccessPeriod: period, Created: time.Now().Format(time.RFC3339),
			Expired: time.Now().AddDate(0, 0, period).Format(time.RFC3339)}
		if su, isBool := b["is_single_use_link"].(bool); isBool {
			l.SingleUse = su
		}
		s.links[l.ID] = l
		s.linkByDoc[id] = l.ID
		writeJSON(w, 201, l.json())
		return true
	case http.MethodPut:
		l := s.links[id]
		if l == nil {
			apiError(w, 404, "", "no such public link")
			return true
		}
		b := body(r)
		l.Type = fmt.Sprint(b["type"])
		if period, okp := asInt(b["access_period"]); okp {
			l.AccessPeriod = period
		}
		writeJSON(w, 200, l.json())
		return true
	case http.MethodDelete:
		l := s.links[id]
		if l == nil {
			apiError(w, 404, "", "no such public link")
			return true
		}
		l.Active = false
		writeJSON(w, 200, nil)
		return true
	}
	return false
}

func (l *sharedLink) json() map[string]any {
	return map[string]any{"id": l.ID, "document_id": l.DocumentID, "is_single_use_link": l.SingleUse, "is_active": l.Active,
		"type": l.Type, "access_period": l.AccessPeriod, "recipient_edrpou": l.Edrpou, "recipient_email": l.Email,
		"date_created": l.Created, "date_updated": l.Created, "date_expired": l.Expired,
		"link": "https://edo.vchasno.ua/public/" + l.ID}
}

// listDocuments implements the filters and the cursor pagination of the two
// register endpoints. Pages hold three documents, which is small enough for a
// test to exercise multi-page walking.
func (s *Server) listDocuments(w http.ResponseWriter, q map[string][]string, src map[string]*doc, outgoing bool) {
	const pageSize = 3
	all := sortedDocs(src)
	filtered := make([]*doc, 0, len(all))
	for _, d := range all {
		if !s.matches(d, q, outgoing) {
			continue
		}
		filtered = append(filtered, d)
	}
	start := 0
	if c := first(q["cursor"]); c != "" {
		if n, err := strconv.Atoi(c); err == nil {
			start = n
		}
	}
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + pageSize
	if end > len(filtered) {
		end = len(filtered)
	}
	rows := make([]map[string]any, 0, end-start)
	for _, d := range filtered[start:end] {
		rows = append(rows, d.json(q, s))
		if has(q, "has_changed") {
			d.HasChanged = false // reading clears the flag, as the real service does
		}
	}
	var next any
	if end < len(filtered) {
		next = strconv.Itoa(end)
	}
	writeJSON(w, 200, map[string]any{"documents": rows, "next_cursor": next})
}

func (s *Server) matches(d *doc, q map[string][]string, outgoing bool) bool {
	if v := first(q["status"]); v != "" {
		if n, err := strconv.Atoi(v); err == nil && d.Status != n {
			return false
		}
	}
	if ids := q["ids"]; len(ids) > 0 {
		found := false
		for _, id := range ids {
			if id == d.ID {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	if cats := q["category"]; len(cats) > 0 {
		found := false
		for _, c := range cats {
			if n, err := strconv.Atoi(c); err == nil && n == d.Category {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	if v := first(q["recipient_edrpou"]); v != "" && d.Recipient != v {
		return false
	}
	if v := first(q["edrpou_owner"]); v != "" && d.Owner != v {
		return false
	}
	if v := first(q["number"]); v != "" && (d.Number == nil || *d.Number != v) {
		return false
	}
	if v := first(q["processed"]); v != "" {
		want := v == "1" || v == "true"
		if d.Processed != want {
			return false
		}
	}
	if v := first(q["is_archived"]); v != "" {
		want := v == "1" || v == "true"
		if s.archived[d.ID] != want {
			return false
		}
	}
	if has(q, "has_changed") && !d.HasChanged {
		return false
	}
	if v := first(q["amount_gte"]); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && (d.Amount == nil || *d.Amount < n) {
			return false
		}
	}
	if v := first(q["amount_lte"]); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && (d.Amount == nil || *d.Amount > n) {
			return false
		}
	}
	if v := first(q["amount_eq"]); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && (d.Amount == nil || *d.Amount != n) {
			return false
		}
	}
	dateKey := "date_from"
	if !outgoing {
		dateKey = "date_created_from"
	}
	if v := first(q[dateKey]); v != "" {
		if t, err := parseDate(v); err == nil && d.Created.Before(t) {
			return false
		}
	}
	dateKey = "date_to"
	if !outgoing {
		dateKey = "date_created_to"
	}
	if v := first(q[dateKey]); v != "" {
		if t, err := parseDate(v); err == nil && d.Created.After(t.Add(24*time.Hour)) {
			return false
		}
	}
	if v := first(q["changed_from"]); v != "" {
		if t, err := parseDate(v); err == nil && d.Changed.Before(t) {
			return false
		}
	}
	if v := first(q["changed_to"]); v != "" {
		if t, err := parseDate(v); err == nil && !d.Changed.Before(t) {
			return false
		}
	}
	return true
}

func parseDate(v string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("bad date %q", v)
}

func first(v []string) string {
	if len(v) == 0 {
		return ""
	}
	return v[0]
}
