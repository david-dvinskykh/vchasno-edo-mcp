// Package mockvchasno is an in-process stand-in for the Vchasno.EDO API.
//
// It exists so that the write paths of this server — uploading, signing,
// sending, rejecting, deleting, archiving, granting permissions — can be
// exercised end to end without touching a real company's documents. It keeps
// everything in memory and models the parts of the service the tools depend
// on: the status machine, cursor pagination, the has_changed flag, the
// processed flag, the documented error codes and the rate limit.
package mockvchasno

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Options tune the behaviour a test wants to provoke.
type Options struct {
	Token     string // the only token the mock accepts
	NoTariff  bool   // answer every data call with 403 access_denied
	FailEvery int    // fail every Nth request with 429, to exercise retries
	Seed      int    // how many documents to generate up front
}

// Server is the mock API.
type Server struct {
	opt Options

	mu         sync.Mutex
	documents  map[string]*doc
	incoming   map[string]*doc
	tags       map[string]*tag
	docTags    map[string][]string
	roleTags   map[string][]string
	comments   map[string][]comment
	signatures map[string][]map[string]any
	reviews    map[string][]map[string]any
	fields     map[string]*field
	docFields  map[string][]map[string]any
	categories map[int]*category
	groups     map[string]*group
	members    map[string][]map[string]any
	links      map[string]*sharedLink
	linkByDoc  map[string]string
	deleteReqs map[string]*deleteReq
	archived   map[string]bool
	extracts   map[string]*extraction
	reports    map[string]*report
	roles      []map[string]any
	counter    int
	requests   int

	// Calls records every path the mock was asked for, so a test can assert
	// that a tool hit the endpoint it claims to.
	Calls []string
}

type doc struct {
	ID            string
	Status        int
	Title         string
	Number        *string
	Date          *string
	Amount        *int64
	Category      int
	Extension     string
	VendorID      *string
	Owner         string
	Recipient     string
	RecipientMail string
	Created       time.Time
	Changed       time.Time
	Finished      *time.Time
	Delivered     bool
	Processed     bool
	Internal      bool
	Multilateral  bool
	HasChanged    bool
	Content       []byte
	Versions      []map[string]any
	Parent        *string
	Children      []string
	Access        string
	SignaturesTo  int
	FirstSignBy   string
}

type tag struct {
	ID, Name, Created string
}

type comment struct {
	ID, Text, Type, Email string
	Internal              bool
	Created               string
}

type field struct {
	ID, Name, Type string
	Required       bool
	Options        []string
}

type category struct {
	ID       int
	Title    string
	IsPublic bool
}

type group struct {
	ID, Name, Created string
}

type sharedLink struct {
	ID, DocumentID, Type string
	SingleUse, Active    bool
	AccessPeriod         int
	Edrpou, Email        *string
	Created, Expired     string
}

type deleteReq struct {
	ID, DocumentID, Message, Status, Receiver string
	Created                                   string
	RejectMessage                             *string
}

type extraction struct {
	DocumentID, Status, Updated string
}

type report struct {
	ID, Status, Filename string
	Ready                time.Time
}

// New builds a mock with some documents already in it.
func New(opt Options) *Server {
	if opt.Token == "" {
		opt.Token = "test-token"
	}
	s := &Server{
		opt:       opt,
		documents: map[string]*doc{}, incoming: map[string]*doc{},
		tags: map[string]*tag{}, docTags: map[string][]string{}, roleTags: map[string][]string{},
		comments: map[string][]comment{}, signatures: map[string][]map[string]any{},
		reviews: map[string][]map[string]any{}, fields: map[string]*field{}, docFields: map[string][]map[string]any{},
		categories: map[int]*category{}, groups: map[string]*group{}, members: map[string][]map[string]any{},
		links: map[string]*sharedLink{}, linkByDoc: map[string]string{}, deleteReqs: map[string]*deleteReq{},
		archived: map[string]bool{}, extracts: map[string]*extraction{}, reports: map[string]*report{},
	}
	s.roles = []map[string]any{
		{"id": "role-admin", "status": "active", "date_created": "2024-01-10T09:00:00+02:00", "email": "admin@example.ua", "position": "Директор"},
		{"id": "role-buh", "status": "active", "date_created": "2024-02-11T09:00:00+02:00", "email": "buh@example.ua", "position": "Бухгалтер"},
		{"id": "role-jurist", "status": "active", "date_created": "2024-03-12T09:00:00+02:00", "email": "jurist@example.ua", "position": "Юрист"},
	}
	s.categories[37] = &category{ID: 37, Title: "Внутрішній акт", IsPublic: false}
	s.categories[1] = &category{ID: 1, Title: "Акт наданих послуг", IsPublic: true}
	s.categories[2] = &category{ID: 2, Title: "Рахунок", IsPublic: true}
	s.tags["tag-1"] = &tag{ID: "tag-1", Name: "Термінові", Created: "2025-01-01T10:00:00+02:00"}
	s.fields["field-1"] = &field{ID: "field-1", Name: "Центр витрат", Type: "text"}
	s.groups["group-1"] = &group{ID: "group-1", Name: "Бухгалтерія", Created: "2025-01-01T10:00:00+02:00"}
	seed := opt.Seed
	if seed == 0 {
		seed = 7
	}
	base := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	statuses := []int{7000, 7001, 7002, 7004, 7006, 7008, 7008}
	for i := 0; i < seed; i++ {
		id := fmt.Sprintf("doc-%03d", i+1)
		num := fmt.Sprintf("A-%03d", i+1)
		date := base.AddDate(0, 0, i).Format("2006-01-02T15:04:05+02:00")
		amount := int64((i + 1) * 100000)
		d := &doc{ID: id, Status: statuses[i%len(statuses)], Title: "Тестовий документ " + num, Number: &num, Date: &date,
			Amount: &amount, Category: 1 + i%2, Extension: ".pdf", Owner: "12345678", Recipient: "87654321",
			RecipientMail: "partner@example.ua", Created: base.AddDate(0, 0, i), Changed: base.AddDate(0, 0, i),
			Access: "extended", SignaturesTo: 2, FirstSignBy: "owner"}
		if d.Status == 7008 {
			f := base.AddDate(0, 0, i+1)
			d.Finished = &f
		}
		s.documents[id] = d
	}
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("in-%03d", i+1)
		num := fmt.Sprintf("B-%03d", i+1)
		date := base.AddDate(0, 0, i).Format("2006-01-02T15:04:05+02:00")
		amount := int64((i + 1) * 250000)
		s.incoming[id] = &doc{ID: id, Status: []int{7002, 7004, 7008}[i], Title: "Вхідний документ " + num, Number: &num,
			Date: &date, Amount: &amount, Category: 2, Extension: ".pdf", Owner: "87654321", Recipient: "12345678",
			Created: base.AddDate(0, 0, i), Changed: base.AddDate(0, 0, i), Access: "extended", SignaturesTo: 2, FirstSignBy: "owner"}
	}
	return s
}

// Requests is how many API calls the mock has served.
func (s *Server) Requests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests
}

// Called reports whether a path containing sub was requested.
func (s *Server) Called(sub string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.Calls {
		if strings.Contains(c, sub) {
			return true
		}
	}
	return false
}

// Doc exposes one stored outgoing document to assertions.
func (s *Server) Doc(id string) (status int, title string, found bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.documents[id]
	if !ok {
		return 0, "", false
	}
	return d.Status, d.Title, true
}

// DocCount is how many outgoing documents the mock holds.
func (s *Server) DocCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.documents)
}

func (s *Server) next(prefix string) string {
	s.counter++
	return fmt.Sprintf("%s-%04d", prefix, s.counter)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func apiError(w http.ResponseWriter, status int, code, reason string) {
	writeJSON(w, status, map[string]any{"code": code, "reason": reason, "details": nil})
}

// ServeHTTP implements the mock API.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests++
	s.Calls = append(s.Calls, r.Method+" "+r.URL.Path)
	n := s.requests
	s.mu.Unlock()

	if r.Header.Get("Authorization") != s.opt.Token {
		apiError(w, http.StatusForbidden, "login_required", "необхідно оновити токен у налаштуваннях")
		return
	}
	if s.opt.FailEvery > 0 && n%s.opt.FailEvery == 0 {
		w.Header().Set("Retry-After", "0")
		apiError(w, http.StatusTooManyRequests, "too_many_requests", "перевищено ліміт запитів")
		return
	}
	path := strings.TrimSuffix(r.URL.Path, "/")
	if s.opt.NoTariff && path != "/api/v2/billing/companies/rates/trials" {
		apiError(w, http.StatusForbidden, "access_denied", "Для доступу до API потрібно оплатити тариф 'Інтеграція'")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.route(w, r, path) {
		return
	}
	apiError(w, http.StatusNotFound, "", "no such endpoint: "+r.Method+" "+path)
}

func body(r *http.Request) map[string]any {
	out := map[string]any{}
	data, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	_ = json.Unmarshal(data, &out)
	return out
}

func bodyList(r *http.Request) []map[string]any {
	var out []map[string]any
	data, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	_ = json.Unmarshal(data, &out)
	return out
}

func strs(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, a := range arr {
		if s, ok := a.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case string:
		i, err := strconv.Atoi(n)
		return i, err == nil
	}
	return 0, false
}

func (d *doc) json(q map[string][]string, s *Server) map[string]any {
	out := map[string]any{
		"id": d.ID, "status": d.Status, "status_text": statusText(d.Status), "title": d.Title,
		"extension": d.Extension, "number": d.Number, "date": d.Date, "amount": d.Amount,
		"category": d.Category, "vendor": "API", "vendor_id": d.VendorID,
		"date_created": d.Created.Format(time.RFC3339), "url": "https://edo.vchasno.ua/app/documents/" + d.ID,
		"is_delivered": d.Delivered, "is_archived": s.archived[d.ID], "is_internal": d.Internal,
		"is_multilateral": d.Multilateral, "processed": d.Processed,
		"signatures_to_finish": d.SignaturesTo, "first_sign_by": d.FirstSignBy, "type": nil,
	}
	if d.Finished != nil {
		out["date_finished"] = d.Finished.Format(time.RFC3339)
	} else {
		out["date_finished"] = nil
	}
	if d.Owner != "" && d.Owner != "12345678" {
		out["edrpou_owner"] = d.Owner
		out["company_name"] = "ТОВ Контрагент"
	}
	if has(q, "with_recipients") {
		out["recipients"] = []map[string]any{{"edrpou": d.Recipient, "emails": []string{d.RecipientMail}, "name": "ТОВ Контрагент", "is_emails_hidden": false}}
	}
	if has(q, "with_connections") {
		out["parent"] = d.Parent
		out["children"] = d.Children
	}
	if has(q, "with_tags") {
		names := []map[string]any{}
		for _, id := range s.docTags[d.ID] {
			if t := s.tags[id]; t != nil {
				names = append(names, map[string]any{"id": t.ID, "name": t.Name})
			}
		}
		out["tags"] = names
	}
	if has(q, "with_document_fields") {
		out["fields"] = s.docFields[d.ID]
	}
	if has(q, "with_versions") {
		out["versions"] = d.Versions
	}
	if has(q, "with_access_settings") {
		out["access_settings"] = map[string]any{"level": d.Access}
	}
	return out
}

func has(q map[string][]string, key string) bool {
	v := q[key]
	return len(v) > 0 && (v[0] == "1" || v[0] == "true")
}

func statusText(code int) string {
	switch code {
	case 7000:
		return "Завантажений"
	case 7001:
		return "Готовий до підпису та надсилання"
	case 7002:
		return "Надісланий контрагенту"
	case 7003:
		return "Підписаний не всіма"
	case 7004:
		return "Очікує підпису контрагента"
	case 7006:
		return "Відхилений"
	case 7007:
		return "Підписаний не всіма"
	case 7008:
		return "Підписаний"
	case 7010:
		return "Надісланий підписантам"
	case 7011:
		return "Анульований"
	}
	return "Невідомий"
}

func sortedDocs(m map[string]*doc) []*doc {
	out := make([]*doc, 0, len(m))
	for _, d := range m {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
