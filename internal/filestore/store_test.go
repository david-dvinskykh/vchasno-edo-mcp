package filestore_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/filestore"
)

func newStore(t *testing.T, ttl time.Duration) *filestore.Store {
	t.Helper()
	s, err := filestore.New(filepath.Join(t.TempDir(), "files"), ttl, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestPutAndOpen(t *testing.T) {
	s := newStore(t, time.Hour)
	data := []byte("%PDF-1.4 hello")
	e, err := s.Put("akt.pdf", "application/pdf", "deadbeef", data)
	if err != nil {
		t.Fatal(err)
	}
	if e.Size != len(data) || e.Filename != "akt.pdf" || e.ContentType != "application/pdf" {
		t.Errorf("entry does not describe the file: %+v", e)
	}
	got, body, ok := s.Open(e.ID)
	if !ok {
		t.Fatal("the entry did not resolve")
	}
	if string(body) != string(data) {
		t.Errorf("body round trip failed: %q", body)
	}
	if got.SHA256 != "deadbeef" {
		t.Errorf("checksum was not kept: %q", got.SHA256)
	}
}

func TestIDsAreUnguessable(t *testing.T) {
	s := newStore(t, time.Hour)
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		e, err := s.Put("f.bin", "application/octet-stream", "", []byte{byte(i)})
		if err != nil {
			t.Fatal(err)
		}
		if len(e.ID) < 40 {
			t.Fatalf("id is too short to be a capability: %q", e.ID)
		}
		if seen[e.ID] {
			t.Fatalf("id repeated: %q", e.ID)
		}
		seen[e.ID] = true
	}
}

func TestUnknownIDDoesNotResolve(t *testing.T) {
	s := newStore(t, time.Hour)
	for _, id := range []string{"", "nope", strings.Repeat("A", 43), "../../etc/passwd"} {
		if _, _, ok := s.Open(id); ok {
			t.Errorf("%q should not resolve", id)
		}
	}
}

func TestExpiryHidesTheEntry(t *testing.T) {
	s := newStore(t, 10*time.Millisecond)
	e, err := s.Put("f.bin", "application/octet-stream", "", []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := s.Open(e.ID); !ok {
		t.Fatal("a fresh entry must resolve")
	}
	time.Sleep(30 * time.Millisecond)
	if _, _, ok := s.Open(e.ID); ok {
		t.Error("an expired entry still resolves")
	}
}

func TestSweepRemovesFilesFromDisk(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "files")
	s, err := filestore.New(dir, 5*time.Millisecond, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Put("f.bin", "application/octet-stream", "", []byte("x")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if n := s.Sweep(); n != 1 {
		t.Errorf("sweep removed %d entries, want 1", n)
	}
	if s.Len() != 0 {
		t.Errorf("the entry is still registered")
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 0 {
		t.Errorf("the file is still on disk: %d left", len(files))
	}
}

func TestForget(t *testing.T) {
	s := newStore(t, time.Hour)
	e, err := s.Put("f.bin", "application/octet-stream", "", []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if !s.Forget(e.ID) {
		t.Error("forget reported nothing removed")
	}
	if _, _, ok := s.Open(e.ID); ok {
		t.Error("a forgotten entry still resolves")
	}
	if s.Forget(e.ID) {
		t.Error("forgetting twice should report nothing")
	}
}

func TestZeroTTLFallsBackToAnHour(t *testing.T) {
	s := newStore(t, 0)
	if s.TTL() != time.Hour {
		t.Errorf("TTL: got %v, want 1h", s.TTL())
	}
}
