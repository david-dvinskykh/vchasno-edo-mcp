// Package filestore hands a downloaded file to the caller as a link instead of
// as bytes in the answer.
//
// A tool that fetches a document from Vchasno stores it here and returns a URL;
// the client (or the model) fetches that URL directly, so a 40 MB archive never
// travels through the conversation. The id is the capability: it is 256 bits of
// randomness, the entry expires, and nothing else guards it — which is why the
// lifetime is short and the URL should be treated like a one-time link.
package filestore

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Entry describes one stored file.
type Entry struct {
	ID          string
	Filename    string
	ContentType string
	Size        int
	SHA256      string
	Expires     time.Time

	path string
}

// Store keeps downloaded files on disk and serves them by opaque id.
type Store struct {
	dir    string
	ttl    time.Duration
	logger *slog.Logger

	mu      sync.Mutex
	entries map[string]*Entry
	stop    chan struct{}
}

// New creates a store rooted at dir. A zero ttl means one hour.
func New(dir string, ttl time.Duration, logger *slog.Logger) (*Store, error) {
	if ttl <= 0 {
		ttl = time.Hour
	}
	if logger == nil {
		logger = slog.Default()
	}
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "vchasno-mcp-files")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("cannot create file store %s: %w", dir, err)
	}
	// The registry lives in memory, so files left by a previous process can
	// never be handed out or swept again — on a persistent volume they would
	// simply accumulate. Start from an empty directory.
	if n := clearDir(dir); n > 0 {
		logger.Info("removed files left by a previous run", "count", n, "dir", dir)
	}
	s := &Store{dir: dir, ttl: ttl, logger: logger, entries: map[string]*Entry{}, stop: make(chan struct{})}
	go s.janitor()
	return s, nil
}

// TTL is how long a stored file stays fetchable.
func (s *Store) TTL() time.Duration { return s.ttl }

// Put writes data to disk and returns the entry describing it.
func (s *Store) Put(filename, contentType, sha string, data []byte) (*Entry, error) {
	id, err := randomID()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(s.dir, id)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return nil, fmt.Errorf("cannot write %s: %w", path, err)
	}
	e := &Entry{ID: id, Filename: filename, ContentType: contentType, Size: len(data),
		SHA256: sha, Expires: time.Now().Add(s.ttl), path: path}
	s.mu.Lock()
	s.entries[id] = e
	s.mu.Unlock()
	return e, nil
}

// Open returns the entry and its bytes, or false when the id is unknown or the
// entry has expired. An expired entry is indistinguishable from a wrong id on
// purpose: neither tells a guesser anything.
func (s *Store) Open(id string) (*Entry, []byte, bool) {
	s.mu.Lock()
	e := s.entries[id]
	s.mu.Unlock()
	if e == nil || time.Now().After(e.Expires) {
		return nil, nil, false
	}
	data, err := os.ReadFile(e.path)
	if err != nil {
		s.logger.Warn("stored file is gone", "id", id, "err", err)
		return nil, nil, false
	}
	return e, data, true
}

// Forget removes one entry and its file, for a link that should stop working.
func (s *Store) Forget(id string) bool {
	s.mu.Lock()
	e := s.entries[id]
	delete(s.entries, id)
	s.mu.Unlock()
	if e == nil {
		return false
	}
	_ = os.Remove(e.path)
	return true
}

// Len is how many entries are currently held, expired ones included.
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// Close stops the cleanup goroutine.
func (s *Store) Close() {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
}

// Sweep drops every expired entry and its file. It runs on a timer and is
// exported so a test does not have to wait for one.
func (s *Store) Sweep() int {
	now := time.Now()
	var stale []*Entry
	s.mu.Lock()
	for id, e := range s.entries {
		if now.After(e.Expires) {
			stale = append(stale, e)
			delete(s.entries, id)
		}
	}
	s.mu.Unlock()
	for _, e := range stale {
		_ = os.Remove(e.path)
	}
	return len(stale)
}

func (s *Store) janitor() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			if n := s.Sweep(); n > 0 {
				s.logger.Debug("file store swept", "removed", n)
			}
		case <-s.stop:
			return
		}
	}
}

func randomID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// clearDir removes the store's leftovers from an earlier process.
func clearDir(dir string) int {
	items, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	removed := 0
	for _, item := range items {
		if item.IsDir() {
			continue
		}
		if os.Remove(filepath.Join(dir, item.Name())) == nil {
			removed++
		}
	}
	return removed
}
