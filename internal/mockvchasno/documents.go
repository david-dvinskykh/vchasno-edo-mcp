package mockvchasno

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The single-document routes and the upload, kept apart from the routing
// table because they carry the status machine, which is the part of the mock
// that has to behave like the real service for the tool tests to mean anything.

func (s *Server) upload(w http.ResponseWriter, r *http.Request, q url.Values) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		apiError(w, 400, "", "bad multipart body")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		apiError(w, 400, "", "the 'file' part is required")
		return
	}
	content, _ := io.ReadAll(io.LimitReader(file, 32<<20))
	_ = file.Close()

	id := s.next("doc")
	now := time.Now()
	d := &doc{ID: id, Status: 7000, Extension: extOf(header.Filename), Owner: "12345678",
		Created: now, Changed: now, Access: "extended", SignaturesTo: 2, FirstSignBy: "owner", Content: content}
	d.Title = q.Get("title")
	if d.Title == "" {
		d.Title = header.Filename
	}
	if v := q.Get("doc_number"); v != "" {
		d.Number = &v
	}
	if v := q.Get("date_document"); v != "" {
		d.Date = &v
	}
	if v := q.Get("vendor_id"); v != "" {
		d.VendorID = &v
	}
	if v := q.Get("amount"); v != "" {
		if n, aerr := strconv.ParseInt(v, 10, 64); aerr == nil {
			d.Amount = &n
		}
	}
	if v := q.Get("category"); v != "" {
		if n, cerr := strconv.Atoi(v); cerr == nil {
			d.Category = n
		}
	}
	if v := q.Get("first_sign_by"); v != "" {
		d.FirstSignBy = v
	}
	d.Internal = q.Get("is_internal") == "1" || q.Get("is_internal") == "true"
	d.Multilateral = q.Get("is_multilateral") == "1" || q.Get("is_multilateral") == "true"
	if v := q.Get("access_settings_level"); v != "" {
		d.Access = v
	}
	if v := q.Get("recipient_edrpou"); v != "" {
		d.Recipient = v
	}
	if v := q.Get("recipient_emails"); v != "" {
		d.RecipientMail = v
		// A counterparty email is what lifts a fresh upload to "ready to send".
		if d.Recipient != "" {
			d.Status = 7001
		}
	}
	if v := q.Get("parent_id"); v != "" {
		d.Parent = &v
		if parent := s.find(v); parent != nil {
			parent.Children = append(parent.Children, id)
		}
	}
	for _, name := range q["tags"] {
		if t := s.tagByIDOrName(name); t != nil {
			s.docTags[id] = append(s.docTags[id], t.ID)
		}
	}
	s.documents[id] = d
	writeJSON(w, 201, map[string]any{"documents": []map[string]any{d.json(q, s)}})
}

func extOf(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return strings.ToLower(name[i:])
	}
	return ""
}

func (s *Server) tagByIDOrName(v string) *tag {
	if t, ok := s.tags[v]; ok {
		return t
	}
	for _, t := range s.tags {
		if strings.EqualFold(t.Name, v) {
			return t
		}
	}
	return nil
}

// documentRoute handles everything under /api/v2/documents/<id>/…
//
//nolint:gocyclo // one switch per sub-resource is the clearest shape here
func (s *Server) documentRoute(w http.ResponseWriter, r *http.Request, seg []string, q url.Values) bool {
	id := seg[1]
	d := s.find(id)
	if d == nil {
		apiError(w, 404, "", "no such document")
		return true
	}
	m := r.Method
	sub := ""
	if len(seg) > 2 {
		sub = seg[2]
	}

	switch {
	case sub == "" && m == http.MethodGet:
		writeJSON(w, 200, d.json(q, s))
		return true

	case sub == "" && m == http.MethodDelete:
		if d.Status == 7008 && !d.Internal {
			apiError(w, 400, "", "a document signed by every party needs an agreed delete request")
			return true
		}
		delete(s.documents, id)
		delete(s.incoming, id)
		writeJSON(w, 200, nil)
		return true

	case sub == "info" && m == http.MethodPatch:
		if d.Status >= 7003 {
			apiError(w, 400, "", "attributes cannot be edited from status 7003 upwards")
			return true
		}
		b := body(r)
		if v, ok := b["title"].(string); ok {
			d.Title = v
		}
		if v, ok := b["number"].(string); ok {
			d.Number = &v
		}
		if v, ok := b["date"].(string); ok {
			d.Date = &v
		}
		if n, ok := asInt(b["category"]); ok {
			d.Category = n
		}
		if n, ok := asInt(b["amount"]); ok {
			v := int64(n)
			d.Amount = &v
		}
		d.Changed = time.Now()
		d.HasChanged = true
		writeJSON(w, 200, d.json(q, s))
		return true

	case sub == "recipient" && m == http.MethodPatch:
		if len(s.signatures[id]) > 0 && d.Status >= 7004 {
			apiError(w, 400, "", "the counterparty already signed")
			return true
		}
		b := body(r)
		d.Recipient = fmt.Sprint(b["edrpou"])
		d.RecipientMail = fmt.Sprint(b["email"])
		if d.Status == 7000 {
			d.Status = 7001
		}
		d.Changed = time.Now()
		d.HasChanged = true
		writeJSON(w, 200, nil)
		return true

	case sub == "recipients" && m == http.MethodPatch:
		b := body(r)
		parties, _ := b["recipients"].([]any)
		if len(parties) == 0 {
			apiError(w, 400, "", "recipients must not be empty")
			return true
		}
		d.Multilateral = true
		d.Changed = time.Now()
		writeJSON(w, 200, nil)
		return true

	case sub == "access-settings" && m == http.MethodPatch:
		level := fmt.Sprint(body(r)["level"])
		if level != "private" && level != "extended" {
			apiError(w, 400, "", "level must be private or extended")
			return true
		}
		d.Access = level
		writeJSON(w, 200, nil)
		return true

	case sub == "viewers-settings" && m == http.MethodPatch:
		if st := fmt.Sprint(body(r)["strategy"]); st != "add" && st != "remove" && st != "replace" {
			apiError(w, 400, "", "bad strategy")
			return true
		}
		writeJSON(w, 200, nil)
		return true

	case sub == "flow" && m == http.MethodPost:
		steps := bodyList(r)
		if len(steps) == 0 {
			apiError(w, 400, "", "the route must not be empty")
			return true
		}
		d.Status = 7010
		d.Multilateral = true
		d.Changed = time.Now()
		writeJSON(w, 200, nil)
		return true

	case sub == "flows" && m == http.MethodGet:
		zero := 0
		writeJSON(w, 200, []map[string]any{{"edrpou": d.Owner, "order": zero, "pending_signatures": 1, "emails": []string{"admin@example.ua"}}})
		return true

	case sub == "signers" && m == http.MethodPost:
		b := body(r)
		if len(strs(b["signer_entities"])) == 0 {
			if _, ok := b["signer_entities"].([]any); !ok {
				apiError(w, 400, "", "signer_entities is required")
				return true
			}
		}
		writeJSON(w, 200, nil)
		return true

	case sub == "signatures" && m == http.MethodGet:
		writeJSON(w, 200, s.signatures[id])
		return true

	case sub == "signatures" && m == http.MethodPost:
		switch d.Status {
		case 7001, 7002, 7003, 7004, 7007, 7010:
		default:
			apiError(w, 400, "", "a signature cannot be added in status "+strconv.Itoa(d.Status))
			return true
		}
		b := body(r)
		if fmt.Sprint(b["signature"]) == "" {
			apiError(w, 400, "", "signature is required")
			return true
		}
		s.signatures[id] = append(s.signatures[id], map[string]any{"id": s.next("sig"), "edrpou": "12345678",
			"company_name": "ТОВ Наша", "is_internal": false, "role_id": "role-admin", "signer_name": "Директор",
			"signer_position": "Директор", "serial_number": "1234ABCD", "timestamp": time.Now().Format(time.RFC3339),
			"has_stamp": b["stamp"] != nil, "is_ecdsa": false})
		if d.Status == 7001 || d.Status == 7002 {
			d.Status = 7003
		}
		d.Changed = time.Now()
		d.HasChanged = true
		writeJSON(w, 200, nil)
		return true

	case sub == "send" && m == http.MethodPost:
		switch d.Status {
		case 7000:
			apiError(w, 400, "", "the document has no counterparty yet")
			return true
		case 7001:
			d.Status = 7002
		case 7003:
			d.Status = 7004
		case 7007:
			d.Status = 7008
			now := time.Now()
			d.Finished = &now
		}
		d.Changed = time.Now()
		d.HasChanged = true
		writeJSON(w, 200, nil)
		return true

	case sub == "reject" && m == http.MethodPost:
		text := fmt.Sprint(body(r)["text"])
		if text == "" {
			apiError(w, 400, "", "text is required")
			return true
		}
		d.Status = 7006
		now := time.Now()
		d.Finished = &now
		d.Changed = now
		d.HasChanged = true
		s.comments[id] = append(s.comments[id], comment{ID: s.next("comment"), Text: text, Type: "rejection",
			Email: "admin@example.ua", Created: now.Format(time.RFC3339)})
		writeJSON(w, 200, nil)
		return true

	case sub == "comments" && m == http.MethodGet:
		rows := []map[string]any{}
		for _, c := range s.comments[id] {
			row := c.json(id)
			row["author"] = map[string]any{"first_name": "Тест", "second_name": "", "last_name": "Користувач",
				"email": c.Email, "edrpou": "12345678", "is_legal": true}
			rows = append(rows, row)
		}
		writeJSON(w, 200, map[string]any{"comments": rows})
		return true

	case sub == "comments" && m == http.MethodPost:
		b := body(r)
		text := fmt.Sprint(b["text"])
		if text == "" {
			apiError(w, 400, "", "text is required")
			return true
		}
		internal, _ := b["is_internal"].(bool)
		s.comments[id] = append(s.comments[id], comment{ID: s.next("comment"), Text: text, Type: "comment",
			Email: "admin@example.ua", Internal: internal, Created: time.Now().Format(time.RFC3339)})
		d.Changed = time.Now()
		d.HasChanged = true
		writeJSON(w, 201, nil)
		return true

	case sub == "reviews" && len(seg) == 3 && m == http.MethodGet:
		writeJSON(w, 200, s.reviews[id])
		return true

	case sub == "reviews" && len(seg) == 4 && seg[3] == "status" && m == http.MethodGet:
		status := "without_any"
		if len(s.reviews[id]) > 0 {
			status = "pending"
		}
		writeJSON(w, 200, map[string]any{"status": status, "is_required": false,
			"date_created": time.Now().Format(time.RFC3339), "date_updated": time.Now().Format(time.RFC3339)})
		return true

	case sub == "reviews" && len(seg) == 4 && seg[3] == "requests":
		switch m {
		case http.MethodGet:
			rows := []map[string]any{}
			for _, rv := range s.reviews[id] {
				rows = append(rows, map[string]any{"user_from_email": "admin@example.ua", "user_to_email": rv["user_email"],
					"group_to_name": rv["group_name"], "status": "active", "date_created": rv["date_created"]})
			}
			writeJSON(w, 200, rows)
			return true
		case http.MethodPost:
			b := body(r)
			s.reviews[id] = append(s.reviews[id], map[string]any{"user_email": b["user_to_email"], "group_name": b["group_to_name"],
				"is_last": false, "action": "", "date_created": time.Now().Format(time.RFC3339)})
			writeJSON(w, 201, nil)
			return true
		case http.MethodDelete:
			b := body(r)
			kept := s.reviews[id][:0]
			for _, rv := range s.reviews[id] {
				if fmt.Sprint(rv["user_email"]) == fmt.Sprint(b["user_to_email"]) && b["user_to_email"] != nil {
					continue
				}
				if fmt.Sprint(rv["group_name"]) == fmt.Sprint(b["group_to_name"]) && b["group_to_name"] != nil {
					continue
				}
				kept = append(kept, rv)
			}
			s.reviews[id] = kept
			writeJSON(w, 204, nil)
			return true
		}

	case sub == "fields" && m == http.MethodGet:
		writeJSON(w, 200, s.docFields[id])
		return true

	case sub == "fields" && m == http.MethodPost:
		b := body(r)
		required, _ := b["is_required"].(bool)
		s.docFields[id] = append(s.docFields[id], map[string]any{"field_id": b["field_id"], "name": s.fieldName(fmt.Sprint(b["field_id"])),
			"type": "text", "is_required": required, "value": b["value"],
			"date_created": time.Now().Format(time.RFC3339), "date_updated": time.Now().Format(time.RFC3339)})
		writeJSON(w, 201, nil)
		return true

	case sub == "child" && len(seg) == 4:
		child := seg[3]
		switch m {
		case http.MethodPost:
			d.Children = append(d.Children, child)
			if c := s.find(child); c != nil {
				c.Parent = &id
			}
			writeJSON(w, 201, nil)
			return true
		case http.MethodDelete:
			d.Children = without(d.Children, child)
			writeJSON(w, 204, nil)
			return true
		}

	case sub == "version" && len(seg) == 3 && m == http.MethodPost:
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			apiError(w, 400, "", "bad multipart body")
			return true
		}
		_, header, ferr := r.FormFile("file")
		if ferr != nil {
			apiError(w, 400, "", "the 'file' part is required")
			return true
		}
		d.Versions = append(d.Versions, map[string]any{"id": s.next("version"), "name": header.Filename,
			"role_id": "role-admin", "date_created": time.Now().Format(time.RFC3339), "is_sent": false, "extension": extOf(header.Filename)})
		writeJSON(w, 200, nil)
		return true

	case sub == "version" && len(seg) == 4 && m == http.MethodDelete:
		kept := d.Versions[:0]
		for _, v := range d.Versions {
			if fmt.Sprint(v["id"]) != seg[3] {
				kept = append(kept, v)
			}
		}
		d.Versions = kept
		writeJSON(w, 204, nil)
		return true

	case sub == "delete-requests" && len(seg) == 3:
		switch m {
		case http.MethodPost:
			dr := &deleteReq{ID: s.next("dr"), DocumentID: id, Message: fmt.Sprint(body(r)["message"]),
				Status: "new", Receiver: d.Recipient, Created: time.Now().Format(time.RFC3339)}
			s.deleteReqs[dr.ID] = dr
			writeJSON(w, 200, []map[string]any{dr.json()})
			return true
		case http.MethodDelete:
			for key, dr := range s.deleteReqs {
				if dr.DocumentID == id {
					dr.Status = "canceled"
					_ = key
				}
			}
			writeJSON(w, 200, nil)
			return true
		}

	case sub == "delete-requests" && len(seg) == 4 && seg[3] == "acceptions" && m == http.MethodPost:
		for _, dr := range s.deleteReqs {
			if dr.DocumentID == id {
				dr.Status = "accepted"
			}
		}
		delete(s.documents, id)
		delete(s.incoming, id)
		writeJSON(w, 200, nil)
		return true

	case sub == "delete-requests" && len(seg) == 4 && seg[3] == "rejections" && m == http.MethodPost:
		msg := fmt.Sprint(body(r)["reject_message"])
		for _, dr := range s.deleteReqs {
			if dr.DocumentID == id {
				dr.Status = "rejected"
				dr.RejectMessage = &msg
			}
		}
		writeJSON(w, 200, nil)
		return true

	case sub == "original" && m == http.MethodGet:
		content := d.Content
		if len(content) == 0 {
			content = []byte("%PDF-1.4 mock original of " + id)
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename="`+id+d.Extension+`"`)
		_, _ = w.Write(content)
		return true

	case sub == "archive" && m == http.MethodGet:
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+id+`.zip"`)
		_, _ = w.Write([]byte("PK\x03\x04 mock archive of " + id))
		return true

	case sub == "p7s" && m == http.MethodGet:
		w.Header().Set("Content-Type", "application/pkcs7-mime")
		_, _ = w.Write([]byte("mock p7s of " + id))
		return true

	case sub == "asic" && m == http.MethodGet:
		w.Header().Set("Content-Type", "application/vnd.etsi.asic-e+zip")
		_, _ = w.Write([]byte("mock asic of " + id))
		return true

	case sub == "pdf" && len(seg) == 4 && seg[3] == "print" && m == http.MethodGet:
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF-1.4 mock print of " + id))
		return true

	case sub == "xml-to-pdf" && m == http.MethodPost:
		writeJSON(w, 200, nil)
		return true

	case sub == "xml-to-pdf" && m == http.MethodGet:
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF-1.4 mock xml rendering of " + id))
		return true

	case sub == "structured-data" && len(seg) == 4 && seg[3] == "download" && m == http.MethodGet:
		e := s.extracts[id]
		if e == nil || e.Status != "confirmed" {
			status := "pending"
			if e != nil {
				status = e.Status
			}
			writeJSON(w, 200, map[string]any{"document_id": id, "version_id": nil, "status": status,
				"date_updated": time.Now().Format(time.RFC3339), "error_message": nil, "skipped_reason": nil})
			return true
		}
		writeJSON(w, 200, map[string]any{
			"details":             map[string]any{"number": "№ 1", "date_created": "2026-05-31", "document_url": "https://edo.vchasno.ua/app/documents/" + id},
			"parties_information": map[string]any{"customer": map[string]any{"company_name": "ТОВ Замовник", "edrpou": "35442539"}},
			"items":               []map[string]any{{"name": "Консультаційні послуги", "quantity": 1, "price_with_vat": 5000, "total_price_with_vat": 5000}},
			"total_price":         map[string]any{"total_price_with_vat": 5000}})
		return true
	}
	return false
}

func (s *Server) fieldName(id string) string {
	if f, ok := s.fields[id]; ok {
		return f.Name
	}
	return ""
}

// Confirm marks a recognition run as checked, so the export path can be tested.
func (s *Server) Confirm(documentID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.extracts[documentID]; ok {
		e.Status = "confirmed"
		return
	}
	s.extracts[documentID] = &extraction{DocumentID: documentID, Status: "confirmed", Updated: time.Now().Format(time.RFC3339)}
}

// Touch marks a document as changed, so the has_changed and sync paths can be tested.
func (s *Server) Touch(documentID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d := s.find(documentID); d != nil {
		d.HasChanged = true
		d.Changed = time.Now()
	}
}
