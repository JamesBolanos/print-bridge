package activity

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type Entry struct {
	Timestamp time.Time
	Endpoint  string
	Target    string
	Outcome   string
	Detail    string
}

func (e Entry) String() string {
	parts := []string{
		e.Timestamp.Format("2006-01-02 15:04:05"),
		e.Endpoint,
		e.Outcome,
	}
	if e.Target != "" {
		parts = append(parts, e.Target)
	}
	if e.Detail != "" {
		parts = append(parts, e.Detail)
	}
	return strings.Join(parts, " | ")
}

func (e Entry) LogLine() string {
	return fmt.Sprintf(
		"%s endpoint=%s target=%q outcome=%s detail=%q\n",
		e.Timestamp.Format(time.RFC3339),
		e.Endpoint,
		sanitize(e.Target),
		sanitize(e.Outcome),
		sanitize(e.Detail),
	)
}

func sanitize(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\t", " ")
	return strings.TrimSpace(value)
}

type Store struct {
	mu      sync.Mutex
	limit   int
	entries []Entry
}

func NewStore(limit int) *Store {
	if limit <= 0 {
		limit = 50
	}
	return &Store{limit: limit}
}

func (s *Store) Add(entry Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries = append([]Entry{entry}, s.entries...)
	if len(s.entries) > s.limit {
		s.entries = s.entries[:s.limit]
	}
}

func (s *Store) List() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries := make([]Entry, len(s.entries))
	copy(entries, s.entries)
	return entries
}
