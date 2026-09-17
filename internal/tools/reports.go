package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Action-history reports and the server's own health check.

type reportIn struct {
	Kind     string `json:"kind,omitempty" jsonschema:"documents (default) = what happened to documents; users = what employees did"`
	DateFrom string `json:"date_from" jsonschema:"Start of the period, YYYY-MM-DD. May not be more than a year in the past"`
	DateTo   string `json:"date_to" jsonschema:"End of the period, YYYY-MM-DD. At most 30 days after date_from"`
	Wait     bool   `json:"wait,omitempty" jsonschema:"Poll until the report is built and return its download link in the same call (up to about a minute)"`
}

type reportIDIn struct {
	ReportID string `json:"report_id" jsonschema:"Report id from request_actions_report"`
}

func (d *Deps) registerReports(srv *mcp.Server) {
	addRead(srv, "request_actions_report", "Звіт про дії",
		"Queue an xlsx report of the action history — either what happened to documents or what employees did. The period may start at most a year ago and may not exceed 30 days. Report building is asynchronous: with wait=true this tool polls and returns the finished file, otherwise it returns a report_id for get_actions_report.",
		func(ctx context.Context, _ *mcp.CallToolRequest, in reportIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			if in.DateFrom == "" || in.DateTo == "" {
				return failf("date_from and date_to are required")
			}
			if err := checkReportPeriod(in.DateFrom, in.DateTo); err != nil {
				return fail(err)
			}
			kind := in.Kind
			if kind == "" {
				kind = "documents"
			}
			var reportID string
			switch kind {
			case "documents":
				r, err := d.api(ctx).RequestDocumentActionsReport(ctx, in.DateFrom, in.DateTo)
				if err != nil {
					return fail(err)
				}
				reportID = r.ReportID
			case "users":
				r, err := d.api(ctx).RequestUserActionsReport(ctx, in.DateFrom, in.DateTo)
				if err != nil {
					return fail(err)
				}
				reportID = r.ReportID
			default:
				return failf("kind must be documents or users")
			}
			out := map[string]any{"ok": true, "kind": kind, "report_id": reportID, "period": in.DateFrom + " … " + in.DateTo}
			if !in.Wait {
				out["next_step"] = "poll get_actions_report with this report_id"
				return ok(out)
			}
			saved, status, err := d.waitForReport(ctx, reportID, false)
			if err != nil {
				out["status"] = status
				out["error"] = err.Error()
				out["next_step"] = "the report is still building; poll get_actions_report with this report_id"
				return ok(out)
			}
			out["status"] = status
			out["file"] = saved
			return ok(out)
		})

	addRead(srv, "get_actions_report", "Отримати звіт",
		"Check whether a queued action report is ready and download it. status=pending means keep waiting, ready means the xlsx is attached, not_found means there is no such report.",
		func(ctx context.Context, _ *mcp.CallToolRequest, in reportIDIn) (*mcp.CallToolResult, any, error) {
			if err := d.requireOpen(); err != nil {
				return fail(err)
			}
			st, err := d.api(ctx).ReportStatusOf(ctx, in.ReportID)
			if err != nil {
				return fail(err)
			}
			out := map[string]any{"report_id": in.ReportID, "status": st.Status, "filename": st.Filename}
			if st.Status != "ready" {
				return ok(out)
			}
			resp, err := d.api(ctx).DownloadReport(ctx, in.ReportID)
			if err != nil {
				return fail(err)
			}
			name := st.Filename
			if name == "" {
				name = in.ReportID + ".xlsx"
			}
			saved, err := d.saveDownload(resp, name, false)
			if err != nil {
				return fail(err)
			}
			out["file"] = saved
			return ok(out)
		})
}

// waitForReport polls a queued report for about a minute.
func (d *Deps) waitForReport(ctx context.Context, reportID string, inline bool) (*savedFile, string, error) {
	deadline := time.Now().Add(60 * time.Second)
	status := "pending"
	for time.Now().Before(deadline) {
		st, err := d.api(ctx).ReportStatusOf(ctx, reportID)
		if err != nil {
			return nil, status, err
		}
		status = st.Status
		switch st.Status {
		case "ready":
			resp, derr := d.api(ctx).DownloadReport(ctx, reportID)
			if derr != nil {
				return nil, status, derr
			}
			name := st.Filename
			if name == "" {
				name = reportID + ".xlsx"
			}
			saved, serr := d.saveDownload(resp, name, inline)
			return saved, status, serr
		case "not_found":
			return nil, status, fmt.Errorf("no report with id %s", reportID)
		}
		select {
		case <-time.After(3 * time.Second):
		case <-ctx.Done():
			return nil, status, ctx.Err()
		}
	}
	return nil, status, fmt.Errorf("the report was still building after 60 seconds")
}

func checkReportPeriod(from, to string) error {
	f, err := time.Parse("2006-01-02", strings.TrimSpace(from)[:min(10, len(strings.TrimSpace(from)))])
	if err != nil {
		return fmt.Errorf("date_from must be YYYY-MM-DD")
	}
	t, err := time.Parse("2006-01-02", strings.TrimSpace(to)[:min(10, len(strings.TrimSpace(to)))])
	if err != nil {
		return fmt.Errorf("date_to must be YYYY-MM-DD")
	}
	if t.Before(f) {
		return fmt.Errorf("date_to is before date_from")
	}
	if t.Sub(f) > 30*24*time.Hour {
		return fmt.Errorf("the period may not exceed 30 days, got %d", int(t.Sub(f).Hours()/24))
	}
	if time.Since(f) > 366*24*time.Hour {
		return fmt.Errorf("date_from may not be more than a year in the past")
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
