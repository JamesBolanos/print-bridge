package capture

import (
	"bytes"
	"context"
	"strings"
)

// PreviewTicket identifies one admitted attempt. Its context is canceled when
// the job is removed, the store closes, or the attempt completes. Workers must
// call FinishPreview even after cancellation to release their reservation.
type PreviewTicket struct {
	Context context.Context
	store   *Store
	id      uint64
}

type previewAttempt struct {
	jobID  string
	cancel context.CancelFunc
}

// BeginPreview reserves bounded work for a retained job without starting a
// renderer. It rejects duplicate work and clears any previous cached preview.
func (s *Store) BeginPreview(id string) (PreviewTicket, error) {
	return s.BeginPreviewAt(id, 0)
}

// BeginPreviewAt reserves bounded work for a retained job and records the
// requested zero-based label index.
func (s *Store) BeginPreviewAt(id string, labelIndex int) (PreviewTicket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return PreviewTicket{}, ErrClosed
	}
	j, ok := s.jobs[id]
	if !ok {
		return PreviewTicket{}, ErrNotFound
	}
	if j.attempt != 0 {
		return PreviewTicket{}, ErrPreviewBusy
	}
	if len(s.attempts) >= s.limits.PendingPreviews {
		return PreviewTicket{}, ErrPreviewLimit
	}
	s.nextWork++
	ctx, cancel := context.WithCancel(context.Background())
	s.attempts[s.nextWork] = previewAttempt{jobID: id, cancel: cancel}
	j.attempt = s.nextWork
	s.bytes -= len(j.Preview)
	j.Preview = nil
	j.PreviewState, j.PreviewError = "queued", ""
	j.PreviewLabelIndex = labelIndex
	return PreviewTicket{Context: ctx, store: s, id: s.nextWork}, nil
}

// StartPreview marks admitted work as rendering unless its job was removed.
func (s *Store) StartPreview(ticket PreviewTicket) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.previewJob(ticket)
	if !ok {
		return ErrStalePreview
	}
	j.PreviewState = "rendering"
	return nil
}

// FinishPreview releases a reservation and copies a successful result only if
// the original job/attempt still exists. Rejected or failed results preserve the
// payload. Preview storage never evicts other captures to make room.
func (s *Store) FinishPreview(ticket PreviewTicket, data []byte, renderErr error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ticket.store != s {
		return ErrStalePreview
	}
	attempt, exists := s.attempts[ticket.id]
	if !exists {
		return ErrStalePreview
	}
	j, valid := s.previewJob(ticket)
	delete(s.attempts, ticket.id)
	attempt.cancel()
	if !valid {
		return ErrStalePreview
	}
	j.attempt = 0
	if renderErr == nil && len(data) > s.limits.PreviewBytes {
		renderErr = ErrPreviewTooLarge
	}
	if renderErr == nil && len(data) > s.limits.StoredBytes-s.bytes {
		renderErr = ErrStorageFull
	}
	if renderErr != nil {
		j.PreviewState = "failed"
		// Clone the bounded message so a substring cannot retain a large error.
		message := renderErr.Error()
		if len(message) > 512 {
			message = message[:512]
		}
		j.PreviewError = strings.Clone(message)
		return renderErr
	}
	j.Preview = bytes.Clone(data)
	s.bytes += len(data)
	j.PreviewState, j.PreviewError = "ready", ""
	return nil
}

// previewJob resolves a ticket under the lock without trusting its public
// context field or allowing a ticket from another session to match by number.
func (s *Store) previewJob(ticket PreviewTicket) (*entry, bool) {
	if ticket.store != s || s.closed {
		return nil, false
	}
	attempt, ok := s.attempts[ticket.id]
	if !ok {
		return nil, false
	}
	j, ok := s.jobs[attempt.jobID]
	return j, ok && j.attempt == ticket.id
}
