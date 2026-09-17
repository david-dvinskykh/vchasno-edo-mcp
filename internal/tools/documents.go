package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/vchasno"
)

// Reading documents: the outgoing register, the incoming register, one
// document by id, bulk statuses and the incremental sync window.

type listDocumentsIn struct {
	DateFrom         string `json:"date_from,omitempty" jsonschema:"Uploaded to Vchasno on or after this date (YYYY-MM-DD or YYYY-MM-DDTHH:MM)"`
	DateTo           string `json:"date_to,omitempty" jsonschema:"Uploaded to Vchasno on or before this date"`
	DateDocumentFrom string `json:"date_document_from,omitempty" jsonschema:"Document's own date on or after this date"`
	DateDocumentTo   string `json:"date_document_to,omitempty" jsonschema:"Document's own date on or before this date"`
	DateFinishedFrom string `json:"date_finished_from,omitempty" jsonschema:"Finished (fully signed or rejected) on or after this date"`
	DateFinishedTo   string `json:"date_finished_to,omitempty" jsonschema:"Finished on or before this date"`
	DateRejectedFrom string `json:"date_rejected_from,omitempty" jsonschema:"Rejected on or after this date"`
	DateRejectedTo   string `json:"date_rejected_to,omitempty" jsonschema:"Rejected on or before this date"`

	Status          string   `json:"status,omitempty" jsonschema:"Document status: a code (7000 7001 7002 7003 7004 7006 7007 7008 7010 7011) or a name (uploaded, ready_to_send, sent_for_first_signature, waiting_for_counterparty, signed, rejected, revoked)"`
	Category        string   `json:"category,omitempty" jsonschema:"Document type: numeric id, or the title of the type (Рахунок, Акт наданих послуг, Договір …). Several allowed, comma-separated"`
	Extension       string   `json:"extension,omitempty" jsonschema:"File extension of the document, e.g. .pdf or .xml"`
	RecipientEdrpou string   `json:"recipient_edrpou,omitempty" jsonschema:"ЄДРПОУ/ІПН of the counterparty the document was sent to"`
	Number          string   `json:"number,omitempty" jsonschema:"External document number"`
	Vendor          string   `json:"vendor,omitempty" jsonschema:"Which integration uploaded the document, e.g. API or 1C"`
	VendorID        string   `json:"vendor_id,omitempty" jsonschema:"Document id in your own system, if it was passed at upload"`
	IDs             []string `json:"ids,omitempty" jsonschema:"Return only these document ids"`

	AmountEq  string `json:"amount_eq,omitempty" jsonschema:"Exact amount in hryvnias, e.g. 1234.56 (ignores the other amount filters)"`
	AmountGte string `json:"amount_min,omitempty" jsonschema:"Minimum amount in hryvnias"`
	AmountLte string `json:"amount_max,omitempty" jsonschema:"Maximum amount in hryvnias"`

	IsDelivered    string `json:"is_delivered,omitempty" jsonschema:"true = the counterparty has seen the document in Vchasno"`
	IsInternal     string `json:"is_internal,omitempty" jsonschema:"true = internal documents of your own company only"`
	IsArchived     string `json:"is_archived,omitempty" jsonschema:"true = only documents sitting in the archive"`
	IsPublicShared string `json:"is_public_shared,omitempty" jsonschema:"true = only documents with an active public link"`
	Processed      string `json:"processed,omitempty" jsonschema:"true = only documents your company marked as processed, false = only unprocessed ones"`
	HasChanged     string `json:"has_changed,omitempty" jsonschema:"true = only documents changed since your last such request. WARNING: reading resets the flag; for repeatable sync use sync_changed_documents instead"`
	NotTagged      string `json:"not_tagged,omitempty" jsonschema:"true = only documents without any label"`
	TagID          string `json:"tag,omitempty" jsonschema:"Label id or label name; only documents carrying it"`
	ReviewState    string `json:"review_state,omitempty" jsonschema:"Internal approval state: without_any, pending, approved, rejected"`
	SDStatus       string `json:"sd_status,omitempty" jsonschema:"Structured-data recognition state: pending, awaiting_validation, confirmed, downloaded, error"`

	With   []string `json:"with,omitempty" jsonschema:"Extra blocks to include: recipients, connections, fields, versions, tags, delete_requests, access_settings, template. Each costs the API extra work, so ask only for what you need"`
	Limit  int      `json:"limit,omitempty" jsonschema:"Maximum documents to return (default 25)"`
	Pages  int      `json:"pages,omitempty" jsonschema:"How many cursor pages to walk (default 1). Vchasno pages are fixed-size, so use this to reach further back"`
	Cursor string   `json:"cursor,omitempty" jsonschema:"next_cursor from a previous answer, to continue where it stopped"`
}

type documentsOut struct {
	Direction  string             `json:"direction"`
	Count      int                `json:"count"`
	NextCursor *string            `json:"next_cursor,omitempty"`
	Filters    map[string]any     `json:"filters_applied,omitempty"`
	Notes      []string           `json:"notes,omitempty"`
	Documents  []vchasno.Document `json:"documents"`
}

func (d *Deps) buildFilter(ctx context.Context, in listDocumentsIn) (vchasno.DocumentFilter, []string, error) {
	var notes []string
	f := vchasno.DocumentFilter{
		DateFrom: in.DateFrom, DateTo: in.DateTo,
		DateDocumentFrom: in.DateDocumentFrom, DateDocumentTo: in.DateDocumentTo,
		DateFinishedFrom: in.DateFinishedFrom, DateFinishedTo: in.DateFinishedTo,
		DateRejectedFrom: in.DateRejectedFrom, DateRejectedTo: in.DateRejectedTo,
		Extension: in.Extension, RecipientEdrpou: in.RecipientEdrpou, Number: in.Number,
		Vendor: in.Vendor, VendorID: in.VendorID, IDs: in.IDs, Cursor: in.Cursor,
	}
	if in.Status != "" {
		code, err := vchasno.ParseStatus(in.Status)
		if err != nil {
			return f, nil, err
		}
		f.Status = fmt.Sprint(code)
	}
	for _, c := range splitList(in.Category) {
		if n, err := atoiSafe(c); err == nil {
			f.Category = append(f.Category, n)
			continue
		}
		if id, found := d.Sess.ResolveCategoryID(ctx, c); found {
			f.Category = append(f.Category, id)
			notes = append(notes, fmt.Sprintf("document type %q resolved to category %d", c, id))
			continue
		}
		return f, nil, fmt.Errorf("unknown document type %q; call list_document_categories to see the ids", c)
	}
	if in.ReviewState != "" {
		if !contains(vchasno.ReviewStates, in.ReviewState) {
			return f, nil, fmt.Errorf("review_state must be one of %s", strings.Join(vchasno.ReviewStates, ", "))
		}
		f.ReviewState = in.ReviewState
	}
	if in.SDStatus != "" {
		if !contains(vchasno.SDStatuses, in.SDStatus) {
			return f, nil, fmt.Errorf("sd_status must be one of %s", strings.Join(vchasno.SDStatuses, ", "))
		}
		f.SDStatus = in.SDStatus
	}
	if in.TagID != "" {
		id, err := d.Sess.ResolveTagID(ctx, in.TagID)
		if err != nil {
			return f, nil, err
		}
		f.TagID = id
	}
	var err error
	if f.AmountEq, err = amountPtr(in.AmountEq); err != nil {
		return f, nil, err
	}
	if f.AmountGte, err = amountPtr(in.AmountGte); err != nil {
		return f, nil, err
	}
	if f.AmountLte, err = amountPtr(in.AmountLte); err != nil {
		return f, nil, err
	}
	if f.IsDelivered, err = boolPtr(in.IsDelivered); err != nil {
		return f, nil, fmt.Errorf("is_delivered: %w", err)
	}
	if f.IsInternal, err = boolPtr(in.IsInternal); err != nil {
		return f, nil, fmt.Errorf("is_internal: %w", err)
	}
	if f.IsArchived, err = boolPtr(in.IsArchived); err != nil {
		return f, nil, fmt.Errorf("is_archived: %w", err)
	}
	if f.IsPublicShared, err = boolPtr(in.IsPublicShared); err != nil {
		return f, nil, fmt.Errorf("is_public_shared: %w", err)
	}
	if f.Processed, err = boolPtr(in.Processed); err != nil {
		return f, nil, fmt.Errorf("processed: %w", err)
	}
	if f.HasChanged, err = boolPtr(in.HasChanged); err != nil {
		return f, nil, fmt.Errorf("has_changed: %w", err)
	}
	if f.HasChanged != nil && *f.HasChanged {
		notes = append(notes, "has_changed=true clears the flag on every document it returns; a repeat of the same request answers nothing. Use sync_changed_documents for an idempotent window.")
	}
	if f.NotTagged, err = boolPtr(in.NotTagged); err != nil {
		return f, nil, fmt.Errorf("not_tagged: %w", err)
	}
	applyWith(&f, in.With)
	return f, notes, nil
}

func applyWith(f *vchasno.DocumentFilter, with []string) {
	t := true
	for _, w := range with {
		switch strings.ToLower(strings.TrimSpace(w)) {
		case "recipients":
			f.WithRecipients = &t
		case "connections", "children", "parent":
			f.WithConnections = &t
		case "fields", "document_fields":
			f.WithDocumentFields = &t
		case "versions":
			f.WithVersions = &t
		case "tags":
			f.WithTags = &t
		case "delete_requests":
			f.WithDeleteRequests = &t
		case "access_settings":
			f.WithAccessSettings = &t
		case "template":
			f.WithTemplate = &t
		}
	}
}

// collect walks cursor pages until limit documents are gathered or the pages
// budget runs out. Vchasno decides the page size itself, so this is the only
// way to answer "the last 200 documents" in one tool call.
func (d *Deps) collect(ctx context.Context, f vchasno.DocumentFilter, limit, pages int,
	fetch func(context.Context, vchasno.DocumentFilter) (*vchasno.DocumentList, error)) ([]vchasno.Document, *string, error) {
	out := make([]vchasno.Document, 0, limit)
	cursor := f.Cursor
	for page := 0; page < pages; page++ {
		f.Cursor = cursor
		list, err := fetch(ctx, f)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, list.Documents...)
		if len(out) >= limit || list.NextCursor == nil || *list.NextCursor == "" {
			if list.NextCursor != nil && *list.NextCursor != "" {
				cursor = *list.NextCursor
				if len(out) > limit {
					out = out[:limit]
				}
				return out, &cursor, nil
			}
			if len(out) > limit {
				out = out[:limit]
			}
			return out, nil, nil
		}
		cursor = *list.NextCursor
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, &cursor, nil
}

func (d *Deps) enrich(ctx context.Context, docs []vchasno.Document) {
	cats := d.Sess.Categories(ctx)
	for i := range docs {
		docs[i].Enrich(cats)
	}
}

type listIncomingIn struct {
	DateCreatedFrom  string   `json:"date_created_from,omitempty" jsonschema:"Uploaded to Vchasno by the sender on or after this date"`
	DateCreatedTo    string   `json:"date_created_to,omitempty" jsonschema:"Uploaded on or before this date"`
	DateSentFrom     string   `json:"date_sent_from,omitempty" jsonschema:"Sent to you on or after this date"`
	DateSentTo       string   `json:"date_sent_to,omitempty" jsonschema:"Sent to you on or before this date"`
	DateDocumentFrom string   `json:"date_document_from,omitempty" jsonschema:"Document's own date on or after this date"`
	DateDocumentTo   string   `json:"date_document_to,omitempty" jsonschema:"Document's own date on or before this date"`
	DateFinishedFrom string   `json:"date_finished_from,omitempty" jsonschema:"Finished on or after this date"`
	DateFinishedTo   string   `json:"date_finished_to,omitempty" jsonschema:"Finished on or before this date"`
	EdrpouOwner      string   `json:"edrpou_owner,omitempty" jsonschema:"ЄДРПОУ/ІПН of the counterparty that sent the document"`
	Status           string   `json:"status,omitempty" jsonschema:"Status code or name (see list_documents)"`
	Category         string   `json:"category,omitempty" jsonschema:"Document type: numeric id or title, comma-separated for several"`
	Extension        string   `json:"extension,omitempty" jsonschema:"File extension, e.g. .pdf"`
	IDs              []string `json:"ids,omitempty" jsonschema:"Return only these document ids"`
	AmountEq         string   `json:"amount_eq,omitempty" jsonschema:"Exact amount in hryvnias"`
	AmountGte        string   `json:"amount_min,omitempty" jsonschema:"Minimum amount in hryvnias"`
	AmountLte        string   `json:"amount_max,omitempty" jsonschema:"Maximum amount in hryvnias"`
	Processed        string   `json:"processed,omitempty" jsonschema:"true = only processed, false = only unprocessed. The usual inbox query is processed=false"`
	IsArchived       string   `json:"is_archived,omitempty" jsonschema:"true = only archived documents"`
	IsPublicShared   string   `json:"is_public_shared,omitempty" jsonschema:"true = only documents with a public link"`
	NotTagged        string   `json:"not_tagged,omitempty" jsonschema:"true = only documents without labels"`
	TagID            string   `json:"tag,omitempty" jsonschema:"Label id or name"`
	ReviewState      string   `json:"review_state,omitempty" jsonschema:"without_any, pending, approved or rejected"`
	SDStatus         string   `json:"sd_status,omitempty" jsonschema:"pending, awaiting_validation, confirmed, downloaded or error"`
	With             []string `json:"with,omitempty" jsonschema:"Extra blocks: recipients, connections, fields, versions, delete_requests, access_settings, template"`
	Limit            int      `json:"limit,omitempty" jsonschema:"Maximum documents to return (default 25)"`
	Pages            int      `json:"pages,omitempty" jsonschema:"How many cursor pages to walk (default 1)"`
	Cursor           string   `json:"cursor,omitempty" jsonschema:"next_cursor from a previous answer"`
}

type getDocumentIn struct {
	ID   string   `json:"id" jsonschema:"Document id in Vchasno"`
	With []string `json:"with,omitempty" jsonschema:"Extra blocks: recipients, connections, fields, versions, tags, delete_requests, access_settings, template (default: recipients, connections, tags, fields)"`
	Full bool     `json:"full,omitempty" jsonschema:"Also fetch signatures, comments, approval state, extra parameters and the public link in one answer"`
}

type documentDossier struct {
	Document   vchasno.Document        `json:"document"`
	Signatures []vchasno.FullSignature `json:"signatures,omitempty"`
	Flows      []vchasno.Flow          `json:"multilateral_flow,omitempty"`
	Comments   []vchasno.Comment       `json:"comments,omitempty"`
	Review     *vchasno.ReviewStatus   `json:"review,omitempty"`
	Fields     []vchasno.DocumentField `json:"extra_fields,omitempty"`
	PublicLink *vchasno.SharedLink     `json:"public_link,omitempty"`
	Partial    []string                `json:"could_not_read,omitempty"`
}

type statusesIn struct {
	IDs    []string `json:"ids" jsonschema:"Document ids, up to 500 per call"`
	IDsCSV string   `json:"ids_csv,omitempty" jsonschema:"Alternative to ids: the same list comma-separated"`
}

type syncIn struct {
	ChangedFrom string   `json:"changed_from" jsonschema:"Start of the sync window, ISO 8601 (e.g. 2026-09-17T08:00). Take it from the previous run's window_start minus a few minutes: changes reach the search index with a small delay"`
	ChangedTo   string   `json:"changed_to,omitempty" jsonschema:"End of the window, exclusive. Defaults to now"`
	Direction   string   `json:"direction,omitempty" jsonschema:"outgoing (default), incoming, or both. Incoming documents have no changed_from filter in the API, so 'incoming' falls back to date_created"`
	With        []string `json:"with,omitempty" jsonschema:"Extra blocks: recipients, connections, fields, versions, tags"`
	Limit       int      `json:"limit,omitempty" jsonschema:"Maximum documents to return (default 100)"`
	Pages       int      `json:"pages,omitempty" jsonschema:"How many cursor pages to walk (default 5)"`
}

func (d *Deps) registerDocuments(srv *mcp.Server) {
	addRead(srv, "list_documents", "Вихідні документи",
		"Outgoing documents — everything your company uploaded to Vchasno, with every filter the API supports: dates (upload, document, finish, rejection), status, type, counterparty ЄДРПОУ, number, amount range, labels, delivery, approval and recognition state. Statuses and types come back with their meaning. This is the main entry point for questions about documents your company sent.",
		func(ctx context.Context, _ *mcp.CallToolRequest, in listDocumentsIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			f, notes, err := d.buildFilter(ctx, in)
			if err != nil {
				return fail(err)
			}
			docs, cursor, err := d.collect(ctx, f, d.limit(in.Limit), d.pages(in.Pages), d.api(ctx).ListDocuments)
			if err != nil {
				return fail(err)
			}
			d.enrich(ctx, docs)
			return ok(documentsOut{Direction: "outgoing", Count: len(docs), NextCursor: cursor, Notes: notes, Documents: docs})
		})

	addRead(srv, "list_incoming_documents", "Вхідні документи",
		"Incoming documents — everything counterparties sent to your company. Same shape as list_documents, with the sender's ЄДРПОУ and company name filled in. The everyday inbox query is processed=false, which shows what nobody in your company has handled yet.",
		func(ctx context.Context, _ *mcp.CallToolRequest, in listIncomingIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			shared := listDocumentsIn{
				DateDocumentFrom: in.DateDocumentFrom, DateDocumentTo: in.DateDocumentTo,
				DateFinishedFrom: in.DateFinishedFrom, DateFinishedTo: in.DateFinishedTo,
				Status: in.Status, Category: in.Category, Extension: in.Extension, IDs: in.IDs,
				AmountEq: in.AmountEq, AmountGte: in.AmountGte, AmountLte: in.AmountLte,
				Processed: in.Processed, IsArchived: in.IsArchived, IsPublicShared: in.IsPublicShared,
				NotTagged: in.NotTagged, TagID: in.TagID, ReviewState: in.ReviewState, SDStatus: in.SDStatus,
				With: in.With, Cursor: in.Cursor,
			}
			f, notes, err := d.buildFilter(ctx, shared)
			if err != nil {
				return fail(err)
			}
			f.DateCreatedFrom, f.DateCreatedTo = in.DateCreatedFrom, in.DateCreatedTo
			f.DateSentFrom, f.DateSentTo = in.DateSentFrom, in.DateSentTo
			f.EdrpouOwner = in.EdrpouOwner
			docs, cursor, err := d.collect(ctx, f, d.limit(in.Limit), d.pages(in.Pages), d.api(ctx).ListIncomingDocuments)
			if err != nil {
				return fail(err)
			}
			d.enrich(ctx, docs)
			return ok(documentsOut{Direction: "incoming", Count: len(docs), NextCursor: cursor, Notes: notes, Documents: docs})
		})

	addRead(srv, "get_document", "Документ",
		"One document by id. With full=true it also gathers the signatures with their certificates, the multilateral route, the comments, the internal-approval state, the extra parameters and the public link — the whole dossier in one call instead of six.",
		func(ctx context.Context, _ *mcp.CallToolRequest, in getDocumentIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if strings.TrimSpace(in.ID) == "" {
				return failf("id is required")
			}
			with := in.With
			if len(with) == 0 {
				with = []string{"recipients", "connections", "tags", "fields"}
			}
			var f vchasno.DocumentFilter
			applyWith(&f, with)
			doc, err := d.api(ctx).GetDocument(ctx, in.ID, f)
			if err != nil {
				return fail(err)
			}
			doc.Enrich(d.Sess.Categories(ctx))
			res := documentDossier{Document: *doc}
			if in.Full {
				api := d.api(ctx)
				if sigs, err := api.ListSignatures(ctx, in.ID); err == nil {
					res.Signatures = sigs
				} else {
					res.Partial = append(res.Partial, "signatures: "+err.Error())
				}
				if doc.IsMultilateral != nil && *doc.IsMultilateral {
					if flows, err := api.ListFlows(ctx, in.ID); err == nil {
						res.Flows = flows
					} else {
						res.Partial = append(res.Partial, "multilateral_flow: "+err.Error())
					}
				}
				if comments, err := api.DocumentComments(ctx, in.ID); err == nil {
					res.Comments = comments
				} else {
					res.Partial = append(res.Partial, "comments: "+err.Error())
				}
				if review, err := api.ReviewState(ctx, in.ID); err == nil && review.Status != "" {
					res.Review = review
				}
				if fields, err := api.GetDocumentFields(ctx, in.ID); err == nil {
					res.Fields = fields
				}
				if link, err := api.GetSharedLink(ctx, in.ID); err == nil && link.ID != "" {
					res.PublicLink = link
				}
			}
			return ok(res)
		})

	addRead(srv, "get_document_statuses", "Статуси пакета документів",
		"Status of up to 500 documents in one request — the cheapest way to poll a batch you uploaded earlier. Each row carries the code, the service's own text and the plain-language meaning.",
		func(ctx context.Context, _ *mcp.CallToolRequest, in statusesIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			ids := mergeIDs(in.IDs, in.IDsCSV)
			if len(ids) == 0 {
				return failf("pass at least one document id")
			}
			if len(ids) > 500 {
				return failf("the API accepts at most 500 ids per call, got %d", len(ids))
			}
			list, err := d.api(ctx).DocumentStatuses(ctx, ids)
			if err != nil {
				return fail(err)
			}
			for i := range list.DataList {
				list.DataList[i].Meaning = vchasno.StatusMeaning[list.DataList[i].StatusID]
			}
			return ok(map[string]any{"count": len(list.DataList), "statuses": list.DataList})
		})

	addRead(srv, "sync_changed_documents", "Синхронізація змін",
		"Incremental sync: every document changed inside a time window (upload, comment, signature, sending, rejection). Unlike has_changed, reading it changes nothing, so the same window always answers the same set — which is what an integration needs to recover after a failure. Overlap the previous window by a few minutes: changes reach the index with a small delay.",
		func(ctx context.Context, _ *mcp.CallToolRequest, in syncIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if strings.TrimSpace(in.ChangedFrom) == "" {
				return failf("changed_from is required")
			}
			to := in.ChangedTo
			if to == "" {
				to = time.Now().Format("2006-01-02T15:04")
			}
			limit, pages := d.limit(in.Limit), d.pages(in.Pages)
			if in.Limit == 0 {
				limit = 100
				if limit > d.Cfg.MaxPageSize*d.Cfg.MaxPages {
					limit = d.Cfg.MaxPageSize * d.Cfg.MaxPages
				}
			}
			if in.Pages == 0 {
				pages = d.pages(5)
			}
			direction := strings.ToLower(strings.TrimSpace(in.Direction))
			if direction == "" {
				direction = "outgoing"
			}
			out := map[string]any{"window_start": in.ChangedFrom, "window_end": to, "direction": direction}
			var notes []string
			if direction == "outgoing" || direction == "both" {
				var f vchasno.DocumentFilter
				applyWith(&f, in.With)
				f.ChangedFrom, f.ChangedTo = in.ChangedFrom, to
				docs, cursor, err := d.collect(ctx, f, limit, pages, d.api(ctx).ListDocuments)
				if err != nil {
					return fail(err)
				}
				d.enrich(ctx, docs)
				out["outgoing"] = docs
				out["outgoing_count"] = len(docs)
				if cursor != nil {
					out["outgoing_next_cursor"] = *cursor
					notes = append(notes, "more outgoing documents remain: pass outgoing_next_cursor to list_documents")
				}
			}
			if direction == "incoming" || direction == "both" {
				var f vchasno.DocumentFilter
				applyWith(&f, in.With)
				f.DateCreatedFrom, f.DateCreatedTo = in.ChangedFrom, to
				docs, cursor, err := d.collect(ctx, f, limit, pages, d.api(ctx).ListIncomingDocuments)
				if err != nil {
					return fail(err)
				}
				d.enrich(ctx, docs)
				out["incoming"] = docs
				out["incoming_count"] = len(docs)
				if cursor != nil {
					out["incoming_next_cursor"] = *cursor
				}
				notes = append(notes, "incoming documents have no changed_from filter in the API; the window was applied to date_created instead, so edits of older incoming documents are not covered")
			}
			out["next_window_start"] = to
			if len(notes) > 0 {
				out["notes"] = notes
			}
			return ok(out)
		})
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if strings.EqualFold(s, v) {
			return true
		}
	}
	return false
}

func atoiSafe(s string) (int, error) {
	var n int
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a number")
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}
