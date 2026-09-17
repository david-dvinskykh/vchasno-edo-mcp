package vchasno_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/vchasno"
)

func TestParseAmountUAH(t *testing.T) {
	cases := map[string]int64{
		"0":         0,
		"1":         100,
		"1.5":       150,
		"1.50":      150,
		"1,56":      156,
		"1234.56":   123456,
		"-12.34":    -1234,
		"12 500.00": 1250000,
	}
	for in, want := range cases {
		got, err := vchasno.ParseAmountUAH(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got != want {
			t.Errorf("%q: got %d kopiykas, want %d", in, got, want)
		}
	}
	if _, err := vchasno.ParseAmountUAH("abc"); err == nil {
		t.Error("expected an error for a non-numeric amount")
	}
	if _, err := vchasno.ParseAmountUAH(""); err == nil {
		t.Error("expected an error for an empty amount")
	}
}

func TestFormatKopiykas(t *testing.T) {
	cases := map[int64]string{0: "0.00", 5: "0.05", 150: "1.50", 123456: "1234.56", -1234: "-12.34"}
	for in, want := range cases {
		if got := vchasno.FormatKopiykas(in); got != want {
			t.Errorf("%d: got %q, want %q", in, got, want)
		}
	}
}

func TestParseStatus(t *testing.T) {
	for in, want := range map[string]int{"7008": 7008, "signed": 7008, "rejected": 7006, "uploaded": 7000, "waiting": 7004, "": 0} {
		got, err := vchasno.ParseStatus(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got != want {
			t.Errorf("%q: got %d, want %d", in, got, want)
		}
	}
	if _, err := vchasno.ParseStatus("nonsense"); err == nil {
		t.Error("expected an error for an unknown status name")
	}
}

func TestValuesSkipsEmpties(t *testing.T) {
	yes, no := true, false
	q := vchasno.Q().Str("a", "1").Str("b", "").Str("c", "  ").
		Int("d", 5).Int("e", 0).
		Bool("f", &yes).Bool("g", &no).Bool("h", nil).
		Strs("i", []string{"x", "", "y"})
	raw := q.Raw()
	for _, want := range []string{"a=1", "d=5", "f=1", "g=0", "i=x", "i=y"} {
		if !strings.Contains(raw, want) {
			t.Errorf("query %q is missing %q", raw, want)
		}
	}
	for _, unwanted := range []string{"b=", "c=", "e=", "h="} {
		if strings.Contains(raw, unwanted) {
			t.Errorf("query %q should not contain %q", raw, unwanted)
		}
	}
}

func TestDocumentEnrich(t *testing.T) {
	amount := int64(123456)
	cat := 2
	d := vchasno.Document{Status: 7004, Amount: &amount, Category: &cat}
	d.Enrich(map[int]string{2: "Рахунок"})
	if d.StatusMeaning == "" {
		t.Error("status meaning was not filled in")
	}
	if d.CategoryTitle != "Рахунок" {
		t.Errorf("category title: got %q", d.CategoryTitle)
	}
	if d.AmountUAH == nil || *d.AmountUAH != "1234.56" {
		t.Errorf("amount in hryvnias: got %v", d.AmountUAH)
	}
}

func TestErrorHints(t *testing.T) {
	for _, tc := range []struct {
		err  *vchasno.Error
		want string
	}{
		{&vchasno.Error{Status: 403, Code: "access_denied"}, "Інтеграція"},
		{&vchasno.Error{Status: 403, Code: "login_required"}, "token"},
		{&vchasno.Error{Status: 429}, "second"},
		{&vchasno.Error{Status: 404}, "no access"},
	} {
		if !strings.Contains(tc.err.Hint(), tc.want) {
			t.Errorf("hint for %v does not mention %q: %q", tc.err.Code, tc.want, tc.err.Hint())
		}
	}
}

func TestClientRetriesOn429(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"code":"too_many_requests","reason":"slow down"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"roles":[{"id":"r1","email":"a@b.ua"}]}`))
	}))
	defer srv.Close()

	c := vchasno.New(vchasno.Credentials{BaseURL: srv.URL, Token: "t"}, vchasno.Options{MaxRPS: 50, RetryAttempts: 4})
	list, err := c.ListRoles(context.Background())
	if err != nil {
		t.Fatalf("expected the retry to succeed: %v", err)
	}
	if len(list.Roles) != 1 {
		t.Fatalf("got %d roles", len(list.Roles))
	}
	if atomic.LoadInt32(&calls) != 3 {
		t.Errorf("expected 3 attempts, got %d", calls)
	}
}

func TestClientDoesNotRetryOn403(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":"access_denied","reason":"нема тарифу"}`))
	}))
	defer srv.Close()

	c := vchasno.New(vchasno.Credentials{BaseURL: srv.URL, Token: "t"}, vchasno.Options{MaxRPS: 50, RetryAttempts: 4})
	_, err := c.GetBilling(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	ae, ok := err.(*vchasno.Error)
	if !ok {
		t.Fatalf("expected *vchasno.Error, got %T", err)
	}
	if ae.Code != "access_denied" || ae.Status != 403 {
		t.Errorf("got %+v", ae)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("a 403 must not be retried, got %d attempts", calls)
	}
}

func TestClientSendsAuthorizationHeader(t *testing.T) {
	got := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := vchasno.New(vchasno.Credentials{BaseURL: srv.URL, Token: "secret-token"}, vchasno.Options{MaxRPS: 50})
	if _, err := c.GetBilling(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h := <-got; h != "secret-token" {
		t.Errorf("Authorization header: got %q", h)
	}
}

func TestRateLimiterPaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := vchasno.New(vchasno.Credentials{BaseURL: srv.URL, Token: "t"}, vchasno.Options{MaxRPS: 4})
	start := time.Now()
	for i := 0; i < 7; i++ {
		if _, err := c.GetBilling(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	// Four tokens are available at once, the remaining three arrive one per
	// 250ms, so seven calls cannot finish instantly.
	if elapsed := time.Since(start); elapsed < 400*time.Millisecond {
		t.Errorf("7 calls at 4 rps took only %v; the limiter is not pacing", elapsed)
	}
}

func TestCredentialsRedacted(t *testing.T) {
	c := vchasno.Credentials{Token: "IJr_ieRGXGbhIfmqepoGORSVidnNMZaGNAFU"}
	red := c.Redacted()
	if strings.Contains(red, "GORSVidnNMZaGNAFU") {
		t.Errorf("the redacted form leaks the token: %q", red)
	}
	if (vchasno.Credentials{Token: "abc"}).Redacted() != "***" {
		t.Error("a short token must be fully masked")
	}
}
