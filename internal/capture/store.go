// Package capture holds bounded, session-only print payloads for inspection.
package capture

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Limits bounds data owned by a store, independently of HTTP or rendering.
type Limits struct {
	RequestBytes    int64
	JobBytes        int
	Jobs            int
	StoredBytes     int
	PreviewBytes    int
	PendingPreviews int
	TargetBytes     int
}

// DefaultLimits returns the initial inspector budgets, not user settings.
func DefaultLimits() Limits {
	return Limits{
		RequestBytes: 8 << 20, JobBytes: 1 << 20, Jobs: 100,
		StoredBytes: 32 << 20, PreviewBytes: 4 << 20,
		PendingPreviews: 8, TargetBytes: 1024,
	}
}

// Store errors let callers distinguish unavailable, oversized, and stale work.
var (
	ErrClosed          = errors.New("capture store is closed")
	ErrTooLarge        = errors.New("capture exceeds size limit")
	ErrNotFound        = errors.New("captured job not found")
	ErrPreviewBusy     = errors.New("preview already pending")
	ErrPreviewLimit    = errors.New("too many pending previews")
	ErrStalePreview    = errors.New("preview attempt is no longer current")
	ErrPreviewTooLarge = errors.New("preview exceeds size limit")
	ErrStorageFull     = errors.New("insufficient preview storage")
)

// Metadata is a value-only snapshot; listing jobs never copies their payloads.
// DetectedType starts as unknown until a later inspector task classifies it.
type Metadata struct {
	ID                string
	ReceivedAt        time.Time
	Target            string
	ByteCount         int
	DetectedType      string
	CaptureState      string
	PreviewState      string
	PreviewError      string
	PreviewLabelIndex int
}

// Job contains independent copies of the captured payload and cached preview.
type Job struct {
	Metadata
	Payload []byte
	Preview []byte
}

type entry struct {
	Job
	attempt uint64
}

// Stats reports retained data and admitted work, including canceled attempts
// whose workers have not yet acknowledged completion.
type Stats struct {
	Jobs            int
	StoredBytes     int
	PendingPreviews int
	Closed          bool
}

// Store owns immutable captured bytes and bounded preview reservations. All
// methods are safe for concurrent use; callers own returned byte snapshots.
type Store struct {
	mu       sync.Mutex
	limits   Limits
	session  string
	nextID   uint64
	nextWork uint64
	jobs     map[string]*entry
	order    []string // oldest first
	attempts map[uint64]previewAttempt
	bytes    int
	closed   bool
}

// NewStore starts an empty session using the documented default limits.
// Its owner must Close it on application exit, not on listener restart.
func NewStore() *Store { return newStore(DefaultLimits()) }

// newStore accepts smaller internal budgets for boundary tests.
func newStore(limits Limits) *Store {
	return &Store{
		limits: limits, session: rand.Text(), jobs: make(map[string]*entry),
		attempts: make(map[uint64]previewAttempt),
	}
}

// RequestLimit returns the virtual request-body ceiling before JSON decoding.
func (s *Store) RequestLimit() int64 { return s.limits.RequestBytes }

// Capture copies a payload without inspection or normalization, evicting the
// oldest jobs as needed. Oversized inputs are rejected without evicting data.
func (s *Store) Capture(ctx context.Context, target string, payload []byte) (Metadata, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Metadata{}, ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return Metadata{}, err
	}
	if len(payload) > s.limits.JobBytes || len(target) > s.limits.TargetBytes ||
		len(payload) > s.limits.StoredBytes-len(target) {
		return Metadata{}, ErrTooLarge
	}
	size := len(payload) + len(target)
	for len(s.order) >= s.limits.Jobs || s.bytes > s.limits.StoredBytes-size {
		s.removeOldest()
	}
	s.nextID++
	meta := Metadata{
		ID: fmt.Sprintf("%s-%d", s.session, s.nextID), ReceivedAt: time.Now().UTC(),
		Target: strings.Clone(target), ByteCount: len(payload), DetectedType: "unknown",
		CaptureState: "captured", PreviewState: "disabled",
	}
	s.jobs[meta.ID] = &entry{Job: Job{Metadata: meta, Payload: bytes.Clone(payload)}}
	s.order = append(s.order, meta.ID)
	s.bytes += size
	return meta, nil
}

// Send implements the destination contract without any network operations.
func (s *Store) Send(ctx context.Context, target string, payload []byte) error {
	_, err := s.Capture(ctx, target, payload)
	return err
}

// Check reports session availability; target is metadata, never a probe address.
func (s *Store) Check(ctx context.Context, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	return ctx.Err()
}

// List returns newest-first metadata, bounded by the store's job limit.
func (s *Store) List() []Metadata {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Metadata, 0, len(s.order))
	for i := len(s.order) - 1; i >= 0; i-- {
		result = append(result, s.jobs[s.order[i]].Metadata)
	}
	return result
}

// Get copies one job so callers cannot mutate the store's payload or preview.
func (s *Store) Get(id string) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return Job{}, ErrNotFound
	}
	return Job{Metadata: j.Metadata, Payload: bytes.Clone(j.Payload), Preview: bytes.Clone(j.Preview)}, nil
}

// Classify records a lightweight type hint and the initial preview state for a
// retained capture. Classification never changes the source payload.
func (s *Store) Classify(id, detectedType, previewState string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return ErrNotFound
	}
	j.DetectedType = strings.Clone(detectedType)
	j.PreviewState = strings.Clone(previewState)
	j.PreviewError = ""
	return nil
}

// DisablePendingPreviews cancels active work and returns queued/rendering jobs
// to disabled without discarding successful cached previews.
func (s *Store) DisablePendingPreviews() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, attempt := range s.attempts {
		attempt.cancel()
	}
	for _, j := range s.jobs {
		if j.attempt != 0 {
			j.attempt = 0
			j.PreviewState = "disabled"
			j.PreviewError = ""
		}
	}
}

// InvalidatePreviews cancels current attempts and releases every cached preview
// when rendering settings change, while retaining all captured payloads.
func (s *Store) InvalidatePreviews() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, attempt := range s.attempts {
		attempt.cancel()
	}
	for _, j := range s.jobs {
		s.bytes -= len(j.Preview)
		j.Preview = nil
		j.attempt = 0
		j.PreviewState = "disabled"
		j.PreviewError = ""
	}
}

// Stats returns a consistent snapshot of retention and outstanding work.
func (s *Store) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Stats{Jobs: len(s.jobs), StoredBytes: s.bytes, PendingPreviews: len(s.attempts), Closed: s.closed}
}

// Clear drops all retained jobs and cancels their work. Canceled reservations
// still occupy slots until FinishPreview acknowledges them, bounding work even
// when a worker is slow to honor cancellation. The session remains usable.
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clear()
}

// Close permanently disables capture, cancels work, and drops session data.
// It is safe to call repeatedly. Previously returned snapshots belong to callers.
func (s *Store) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.clear()
	clear(s.attempts)
}

// clear releases retained references with the store lock held.
func (s *Store) clear() {
	for _, attempt := range s.attempts {
		attempt.cancel()
	}
	clear(s.jobs)
	s.order = nil
	s.bytes = 0
}

// removeOldest evicts one job and invalidates its preview while holding the lock.
func (s *Store) removeOldest() {
	id := s.order[0]
	j := s.jobs[id]
	if attempt, ok := s.attempts[j.attempt]; ok {
		attempt.cancel()
	}
	s.bytes -= len(j.Payload) + len(j.Target) + len(j.Preview)
	delete(s.jobs, id)
	s.order[0] = ""
	s.order = s.order[1:]
}
