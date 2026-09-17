// Package vchasno is a typed client for the Vchasno.EDO public API (v2).
//
// It owns everything transport-level that every endpoint needs: the
// Authorization header, the 10 requests/second per company rate limit,
// retries on 429 and 5xx, and turning error bodies into *Error.
package vchasno

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Credentials identify one Vchasno company context: the API host plus the
// user+company token that Vchasno issues in the web cabinet.
type Credentials struct {
	BaseURL string
	Token   string
}

// Redacted returns the token with only its head visible, for logs.
func (c Credentials) Redacted() string {
	if len(c.Token) <= 6 {
		return "***"
	}
	return c.Token[:4] + "…" + strconv.Itoa(len(c.Token))
}

// Options tune one client.
type Options struct {
	Timeout       time.Duration
	MaxRPS        float64
	RetryAttempts int
	MaxUpload     int64
	Logger        *slog.Logger
	HTTPClient    *http.Client
}

// Client talks to one Vchasno company.
type Client struct {
	creds   Credentials
	http    *http.Client
	limiter *limiter
	retries int
	maxUp   int64
	log     *slog.Logger
}

// New builds a client. Zero-valued options fall back to safe defaults.
func New(creds Credentials, opt Options) *Client {
	if opt.Timeout <= 0 {
		opt.Timeout = 90 * time.Second
	}
	if opt.MaxRPS <= 0 {
		opt.MaxRPS = 8
	}
	if opt.RetryAttempts <= 0 {
		opt.RetryAttempts = 3
	}
	if opt.MaxUpload <= 0 {
		opt.MaxUpload = 15 << 20
	}
	if opt.Logger == nil {
		opt.Logger = slog.Default()
	}
	hc := opt.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: opt.Timeout}
	}
	creds.BaseURL = strings.TrimRight(creds.BaseURL, "/")
	if creds.BaseURL == "" {
		creds.BaseURL = "https://edo.vchasno.ua"
	}
	return &Client{creds: creds, http: hc, limiter: newLimiter(opt.MaxRPS), retries: opt.RetryAttempts, maxUp: opt.MaxUpload, log: opt.Logger}
}

// Creds exposes the credentials this client was built with.
func (c *Client) Creds() Credentials { return c.creds }

// MaxUpload is the largest single file the server accepts.
func (c *Client) MaxUpload() int64 { return c.maxUp }

// ── request plumbing ────────────────────────────────────────────

// Values is a query string under construction; nil values are skipped so that
// tools can pass optional filters without branching on every one of them.
type Values struct{ v url.Values }

// Q starts an empty query.
func Q() *Values { return &Values{v: url.Values{}} }

// Str adds a string parameter unless it is empty.
func (q *Values) Str(key, val string) *Values {
	if strings.TrimSpace(val) != "" {
		q.v.Set(key, strings.TrimSpace(val))
	}
	return q
}

// Strs repeats a parameter once per value, the way Vchasno expects ids=a&ids=b.
func (q *Values) Strs(key string, vals []string) *Values {
	for _, s := range vals {
		if strings.TrimSpace(s) != "" {
			q.v.Add(key, strings.TrimSpace(s))
		}
	}
	return q
}

// Int adds an integer parameter unless it is zero.
func (q *Values) Int(key string, val int) *Values {
	if val != 0 {
		q.v.Set(key, strconv.Itoa(val))
	}
	return q
}

// Ints repeats an integer parameter once per value.
func (q *Values) Ints(key string, vals []int) *Values {
	for _, n := range vals {
		q.v.Add(key, strconv.Itoa(n))
	}
	return q
}

// Bool adds a 1/0 parameter only when the pointer is set, so that "false"
// stays distinguishable from "not asked".
func (q *Values) Bool(key string, val *bool) *Values {
	if val != nil {
		if *val {
			q.v.Set(key, "1")
		} else {
			q.v.Set(key, "0")
		}
	}
	return q
}

// Raw returns the encoded query.
func (q *Values) Raw() string {
	if q == nil {
		return ""
	}
	return q.v.Encode()
}

// Len reports how many parameters were set.
func (q *Values) Len() int {
	if q == nil {
		return 0
	}
	return len(q.v)
}

// Response is a raw API answer with the body already read.
type Response struct {
	Status      int
	Body        []byte
	ContentType string
	Filename    string // from Content-Disposition, when the answer is a file
}

// looksJSON reports whether the body is worth decoding. Several Vchasno
// endpoints answer a successful write with plain text ("201: Created") or with
// nothing at all; treating that as a decode failure would report a successful
// call as an error.
func (r *Response) looksJSON() bool {
	body := bytes.TrimSpace(r.Body)
	if len(body) == 0 {
		return false
	}
	if ct := strings.ToLower(r.ContentType); ct != "" && !strings.Contains(ct, "json") {
		return false
	}
	return body[0] == '{' || body[0] == '['
}

// JSON unmarshals the body into out. A zero-length body is not an error:
// several Vchasno endpoints answer 200/204 with nothing.
func (r *Response) JSON(out any) error {
	if out == nil || len(bytes.TrimSpace(r.Body)) == 0 {
		return nil
	}
	if err := json.Unmarshal(r.Body, out); err != nil {
		return fmt.Errorf("cannot decode response (%s): %w", clip(string(r.Body), 200), err)
	}
	return nil
}

// Get performs a GET and decodes JSON into out.
func (c *Client) Get(ctx context.Context, path string, q *Values, out any) (*Response, error) {
	return c.do(ctx, http.MethodGet, path, q, nil, "", out)
}

// GetRaw performs a GET and returns the untouched body (file downloads).
func (c *Client) GetRaw(ctx context.Context, path string, q *Values) (*Response, error) {
	return c.do(ctx, http.MethodGet, path, q, nil, "", nil)
}

// Post sends a JSON body (nil for an empty one) and decodes the answer.
func (c *Client) Post(ctx context.Context, path string, q *Values, body, out any) (*Response, error) {
	return c.doJSON(ctx, http.MethodPost, path, q, body, out)
}

// Patch sends a JSON body with PATCH.
func (c *Client) Patch(ctx context.Context, path string, q *Values, body, out any) (*Response, error) {
	return c.doJSON(ctx, http.MethodPatch, path, q, body, out)
}

// Put sends a JSON body with PUT.
func (c *Client) Put(ctx context.Context, path string, q *Values, body, out any) (*Response, error) {
	return c.doJSON(ctx, http.MethodPut, path, q, body, out)
}

// Delete sends DELETE, optionally with a JSON body: several Vchasno endpoints
// (tag disconnection, delete locks) carry their payload on DELETE.
func (c *Client) Delete(ctx context.Context, path string, q *Values, body, out any) (*Response, error) {
	if body == nil {
		return c.do(ctx, http.MethodDelete, path, q, nil, "", out)
	}
	return c.doJSON(ctx, http.MethodDelete, path, q, body, out)
}

func (c *Client) doJSON(ctx context.Context, method, path string, q *Values, body, out any) (*Response, error) {
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("cannot encode request body: %w", err)
		}
	}
	return c.do(ctx, method, path, q, payload, "application/json", out)
}

// FilePart is one file of a multipart upload.
type FilePart struct {
	Field    string
	Filename string
	Content  []byte
}

// PostMultipart uploads files with optional text fields.
func (c *Client) PostMultipart(ctx context.Context, path string, q *Values, files []FilePart, fields map[string][]string, out any) (*Response, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for key, vals := range fields {
		for _, v := range vals {
			if err := mw.WriteField(key, v); err != nil {
				return nil, err
			}
		}
	}
	for _, f := range files {
		if int64(len(f.Content)) > c.maxUp {
			return nil, fmt.Errorf("file %q is %d bytes, over the %d byte limit", f.Filename, len(f.Content), c.maxUp)
		}
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, escapeQuotes(f.Field), escapeQuotes(f.Filename)))
		h.Set("Content-Type", contentTypeFor(f.Filename))
		w, err := mw.CreatePart(h)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(f.Content); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return c.do(ctx, http.MethodPost, path, q, buf.Bytes(), mw.FormDataContentType(), out)
}

func (c *Client) do(ctx context.Context, method, path string, q *Values, body []byte, contentType string, out any) (*Response, error) {
	u := c.creds.BaseURL + path
	if raw := q.Raw(); raw != "" {
		u += "?" + raw
	}
	var last error
	for attempt := 1; attempt <= c.retries; attempt++ {
		if err := c.limiter.wait(ctx); err != nil {
			return nil, err
		}
		var rdr io.Reader
		if body != nil {
			rdr = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, u, rdr)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", c.creds.Token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "vchasno-edo-mcp")
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			last = fmt.Errorf("vchasno %s %s: %w", method, path, err)
			if ctx.Err() != nil || attempt == c.retries {
				return nil, last
			}
			c.backoff(ctx, attempt, 0)
			continue
		}
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
		_ = resp.Body.Close()
		if readErr != nil {
			last = fmt.Errorf("vchasno %s %s: reading body: %w", method, path, readErr)
			if attempt == c.retries {
				return nil, last
			}
			c.backoff(ctx, attempt, 0)
			continue
		}
		r := &Response{Status: resp.StatusCode, Body: raw, ContentType: resp.Header.Get("Content-Type"), Filename: filenameFrom(resp.Header.Get("Content-Disposition"))}
		if resp.StatusCode >= 400 {
			apiErr := parseError(method, path, r)
			if apiErr.RetryDue && attempt < c.retries {
				c.log.Warn("vchasno request failed, retrying", "method", method, "path", path, "status", resp.StatusCode, "attempt", attempt)
				c.backoff(ctx, attempt, retryAfter(resp))
				last = apiErr
				continue
			}
			return r, apiErr
		}
		if out != nil && r.looksJSON() {
			if err := r.JSON(out); err != nil {
				return r, err
			}
		}
		return r, nil
	}
	if last == nil {
		last = errors.New("request failed")
	}
	return nil, last
}

func (c *Client) backoff(ctx context.Context, attempt int, hinted time.Duration) {
	d := hinted
	if d <= 0 {
		d = time.Duration(1<<uint(attempt-1)) * 500 * time.Millisecond
	}
	if d > 15*time.Second {
		d = 15 * time.Second
	}
	select {
	case <-time.After(d):
	case <-ctx.Done():
	}
}

func retryAfter(resp *http.Response) time.Duration {
	if v := resp.Header.Get("Retry-After"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 {
			return time.Duration(n) * time.Second
		}
	}
	return 0
}

func parseError(method, path string, r *Response) *Error {
	e := &Error{Status: r.Status, Method: method, Path: path}
	var payload struct {
		Code    string `json:"code"`
		Reason  string `json:"reason"`
		Detail  string `json:"detail"`
		Message string `json:"message"`
		Details any    `json:"details"`
	}
	if err := json.Unmarshal(r.Body, &payload); err == nil {
		e.Code, e.Reason, e.Details = payload.Code, payload.Reason, payload.Details
		if e.Reason == "" {
			e.Reason = firstNonEmpty(payload.Detail, payload.Message)
		}
	}
	if e.Code == "" && e.Reason == "" {
		e.Snippet = clip(strings.TrimSpace(string(r.Body)), 400)
	}
	e.RetryDue = r.Status == http.StatusTooManyRequests || r.Status >= 500
	return e
}

// ── small helpers ───────────────────────────────────────────────

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func escapeQuotes(s string) string {
	return strings.NewReplacer("\\", "\\\\", `"`, `\"`).Replace(s)
}

func contentTypeFor(name string) string {
	if ct := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

func filenameFrom(disposition string) string {
	if disposition == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(disposition)
	if err != nil {
		return ""
	}
	if n := params["filename*"]; n != "" {
		return n
	}
	return params["filename"]
}

// ── rate limiting ───────────────────────────────────────────────

// limiter is a token bucket sized to the documented company-wide limit of
// 10 requests per second; going over it costs every integration of the
// company a 429, so the client paces itself rather than reacting.
type limiter struct {
	interval time.Duration
	tokens   chan struct{}
}

func newLimiter(rps float64) *limiter {
	burst := int(rps)
	if burst < 1 {
		burst = 1
	}
	l := &limiter{interval: time.Duration(float64(time.Second) / rps), tokens: make(chan struct{}, burst)}
	for i := 0; i < burst; i++ {
		l.tokens <- struct{}{}
	}
	go func() {
		t := time.NewTicker(l.interval)
		defer t.Stop()
		for range t.C {
			select {
			case l.tokens <- struct{}{}:
			default:
			}
		}
	}()
	return l
}

func (l *limiter) wait(ctx context.Context) error {
	select {
	case <-l.tokens:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
