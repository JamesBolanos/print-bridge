package capture

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCapturePreservesBytesAndReturnsIndependentSnapshots(t *testing.T) {
	s := NewStore()
	t.Cleanup(s.Close)
	payload := []byte(" \r\n\t^XA^FDcafé 世界^FS^XZ\x00<script>raw</script> ")
	want := bytes.Clone(payload)
	before := time.Now()
	meta, err := s.Capture(context.Background(), " printer.local:9100 ", payload)
	require.NoError(t, err)
	assert.WithinDuration(t, before, meta.ReceivedAt, time.Second)
	assert.Equal(t, " printer.local:9100 ", meta.Target)
	assert.Equal(t, len(want), meta.ByteCount)
	assert.Equal(t, "unknown", meta.DetectedType)
	assert.Equal(t, "captured", meta.CaptureState)
	assert.Equal(t, "disabled", meta.PreviewState)
	payload[0] = '!'
	job, err := s.Get(meta.ID)
	require.NoError(t, err)
	assert.Equal(t, want, job.Payload)
	job.Payload[0] = '?'
	listed := s.List()
	require.Len(t, listed, 1)
	listed[0].Target = "changed"
	job, err = s.Get(meta.ID)
	require.NoError(t, err)
	assert.Equal(t, want, job.Payload)
	assert.Equal(t, meta.Target, job.Target)
	assert.Equal(t, len(want)+len(meta.Target), s.Stats().StoredBytes)
}

func TestCaptureLimitsRejectBeforeEviction(t *testing.T) {
	limits := DefaultLimits()
	limits.JobBytes, limits.TargetBytes, limits.StoredBytes = 5, 3, 7
	s := newStore(limits)
	t.Cleanup(s.Close)
	first, err := s.Capture(context.Background(), "ab", []byte("12345"))
	require.NoError(t, err) // exact job and total boundaries
	cases := []struct{ target, payload string }{
		{"", "123456"},   // payload too large
		{"abcd", "1"},    // target too large
		{"abc", "12345"}, // individually exceeds aggregate budget
	}
	for _, tc := range cases {
		_, err := s.Capture(context.Background(), tc.target, []byte(tc.payload))
		require.ErrorIs(t, err, ErrTooLarge)
		assert.Equal(t, []Metadata{first}, s.List())
		assert.Equal(t, 7, s.Stats().StoredBytes)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = s.Capture(ctx, "", []byte("new"))
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, []Metadata{first}, s.List())
}

func TestCaptureEvictsOldestByCountOrBytes(t *testing.T) {
	for _, byCount := range []bool{true, false} {
		name := "bytes"
		if byCount {
			name = "count"
		}
		t.Run(name, func(t *testing.T) {
			limits := DefaultLimits()
			if byCount {
				limits.Jobs = 2
			} else {
				limits.StoredBytes = 6
			}
			s := newStore(limits)
			t.Cleanup(s.Close)
			first, err := s.Capture(context.Background(), "t", []byte("12"))
			require.NoError(t, err)
			second, err := s.Capture(context.Background(), "t", []byte("34"))
			require.NoError(t, err)
			third, err := s.Capture(context.Background(), "t", []byte("56"))
			require.NoError(t, err)
			assert.Equal(t, []Metadata{third, second}, s.List())
			_, err = s.Get(first.ID)
			assert.ErrorIs(t, err, ErrNotFound)
			assert.Equal(t, 6, s.Stats().StoredBytes)
		})
	}
}

func TestClearAndCloseEndJobLifetimes(t *testing.T) {
	s := NewStore()
	first, err := s.Capture(context.Background(), "", []byte("old"))
	require.NoError(t, err)
	ticket, err := s.BeginPreview(first.ID)
	require.NoError(t, err)
	s.Clear()
	assert.ErrorIs(t, ticket.Context.Err(), context.Canceled)
	assert.Empty(t, s.List())
	assert.Zero(t, s.Stats().StoredBytes)
	require.NoError(t, s.Check(context.Background(), "unreachable.invalid"))
	second, err := s.Capture(context.Background(), "", []byte("new"))
	require.NoError(t, err)
	assert.NotEqual(t, first.ID, second.ID)
	assert.ErrorIs(t, s.FinishPreview(ticket, []byte("late"), nil), ErrStalePreview)
	assert.Equal(t, 3, s.Stats().StoredBytes)
	assert.Equal(t, []Metadata{second}, s.List())

	ticket, err = s.BeginPreview(second.ID)
	require.NoError(t, err)
	s.Close()
	s.Close()
	assert.ErrorIs(t, ticket.Context.Err(), context.Canceled)
	assert.ErrorIs(t, s.Check(context.Background(), ""), ErrClosed)
	assert.ErrorIs(t, s.Send(context.Background(), "", []byte("late")), ErrClosed)
	assert.ErrorIs(t, s.FinishPreview(ticket, []byte("late"), nil), ErrStalePreview)
	assert.Equal(t, Stats{Closed: true}, s.Stats())
	newSession := NewStore()
	t.Cleanup(newSession.Close)
	newJob, err := newSession.Capture(context.Background(), "", []byte("new"))
	require.NoError(t, err)
	assert.NotEqual(t, first.ID, newJob.ID)
	assert.Empty(t, s.List())
}

func TestConcurrentCaptureReadClearAndPreview(t *testing.T) {
	limits := DefaultLimits()
	limits.Jobs, limits.StoredBytes, limits.PendingPreviews = 10, 100, 3
	s := newStore(limits)
	t.Cleanup(s.Close)
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				meta, err := s.Capture(context.Background(), "t", []byte("data"))
				if err != nil {
					t.Error(err)
					return
				}
				_, err = s.Get(meta.ID)
				if err != nil && !errors.Is(err, ErrNotFound) {
					t.Error(err)
				}
				_ = s.List()
				ticket, err := s.BeginPreview(meta.ID)
				if err == nil {
					if i%7 == 0 {
						s.Clear()
					}
					err = s.FinishPreview(ticket, []byte("png"), nil)
					if err != nil && !errors.Is(err, ErrStalePreview) && !errors.Is(err, ErrStorageFull) {
						t.Error(err)
					}
				} else if !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrPreviewLimit) {
					t.Error(err)
				}
				stats := s.Stats()
				if stats.Jobs > limits.Jobs || stats.StoredBytes > limits.StoredBytes || stats.StoredBytes < 0 || stats.PendingPreviews > limits.PendingPreviews {
					t.Errorf("limits exceeded: %+v", stats)
				}
			}
		}()
	}
	wg.Wait()
	assert.Zero(t, s.Stats().PendingPreviews)
	s.Clear()
	assert.Equal(t, Stats{}, s.Stats())
}

func TestPreviewReservationAndImmutableResult(t *testing.T) {
	s := NewStore()
	t.Cleanup(s.Close)
	meta, err := s.Capture(context.Background(), "", []byte("payload"))
	require.NoError(t, err)
	ticket, err := s.BeginPreview(meta.ID)
	require.NoError(t, err)
	assert.Equal(t, "queued", s.List()[0].PreviewState)
	_, err = s.BeginPreview(meta.ID)
	assert.ErrorIs(t, err, ErrPreviewBusy)
	require.NoError(t, s.StartPreview(ticket))
	assert.Equal(t, "rendering", s.List()[0].PreviewState)
	data := []byte("preview")
	require.NoError(t, s.FinishPreview(ticket, data, nil))
	assert.ErrorIs(t, ticket.Context.Err(), context.Canceled)
	data[0] = '!'
	job, err := s.Get(meta.ID)
	require.NoError(t, err)
	assert.Equal(t, []byte("preview"), job.Preview)
	assert.Equal(t, "captured", job.CaptureState)
	assert.Equal(t, "ready", job.PreviewState)
	job.Preview[0] = '?'
	job, err = s.Get(meta.ID)
	require.NoError(t, err)
	assert.Equal(t, []byte("preview"), job.Preview)
	assert.Equal(t, 14, s.Stats().StoredBytes)

	retry, err := s.BeginPreview(meta.ID)
	require.NoError(t, err)
	assert.Equal(t, 7, s.Stats().StoredBytes)
	assert.ErrorIs(t, s.FinishPreview(ticket, []byte("stale"), nil), ErrStalePreview)
	assert.NoError(t, retry.Context.Err())
	assert.Equal(t, 1, s.Stats().PendingPreviews)
	renderErr := errors.New(strings.Repeat("x", 1000))
	assert.ErrorIs(t, s.FinishPreview(retry, nil, renderErr), renderErr)
	job, err = s.Get(meta.ID)
	require.NoError(t, err)
	assert.Equal(t, "failed", job.PreviewState)
	assert.Len(t, job.PreviewError, 512)
	assert.Equal(t, []byte("payload"), job.Payload)
	assert.Zero(t, s.Stats().PendingPreviews)
}

func TestPreviewLimitsNeverEvictCaptures(t *testing.T) {
	limits := DefaultLimits()
	limits.PreviewBytes, limits.StoredBytes = 4, 10
	s := newStore(limits)
	t.Cleanup(s.Close)
	first, err := s.Capture(context.Background(), "", []byte("1234"))
	require.NoError(t, err)
	second, err := s.Capture(context.Background(), "", []byte("56"))
	require.NoError(t, err)
	ticket, err := s.BeginPreview(first.ID)
	require.NoError(t, err)
	require.NoError(t, s.FinishPreview(ticket, []byte("abcd"), nil)) // exact boundaries
	assert.Equal(t, 10, s.Stats().StoredBytes)
	ticket, err = s.BeginPreview(second.ID)
	require.NoError(t, err)
	assert.ErrorIs(t, s.FinishPreview(ticket, []byte("a"), nil), ErrStorageFull)
	assert.Equal(t, 2, s.Stats().Jobs)
	ticket, err = s.BeginPreview(first.ID)
	require.NoError(t, err)
	assert.ErrorIs(t, s.FinishPreview(ticket, []byte("abcde"), nil), ErrPreviewTooLarge)
	assert.Equal(t, 6, s.Stats().StoredBytes)
	assert.Equal(t, 2, s.Stats().Jobs)
}

func TestEvictionCancelsWorkAndLateResultCannotRestoreJob(t *testing.T) {
	limits := DefaultLimits()
	limits.Jobs, limits.PendingPreviews = 1, 1
	s := newStore(limits)
	t.Cleanup(s.Close)
	first, err := s.Capture(context.Background(), "", []byte("first"))
	require.NoError(t, err)
	ticket, err := s.BeginPreview(first.ID)
	require.NoError(t, err)
	second, err := s.Capture(context.Background(), "", []byte("second"))
	require.NoError(t, err)
	assert.ErrorIs(t, ticket.Context.Err(), context.Canceled)
	_, err = s.BeginPreview(second.ID)
	assert.ErrorIs(t, err, ErrPreviewLimit) // canceled worker still owns a slot
	assert.ErrorIs(t, s.StartPreview(ticket), ErrStalePreview)
	assert.ErrorIs(t, s.FinishPreview(ticket, []byte("late result"), nil), ErrStalePreview)
	assert.Equal(t, []Metadata{second}, s.List())
	assert.Equal(t, 6, s.Stats().StoredBytes)
	retry, err := s.BeginPreview(second.ID)
	require.NoError(t, err)
	s.Clear()
	third, err := s.Capture(context.Background(), "", []byte("third"))
	require.NoError(t, err)
	_, err = s.BeginPreview(third.ID)
	assert.ErrorIs(t, err, ErrPreviewLimit)
	assert.ErrorIs(t, s.FinishPreview(retry, nil, context.Canceled), ErrStalePreview)
	assert.Zero(t, s.Stats().PendingPreviews)
}

func TestCaptureBudgetIncludesCachedPreview(t *testing.T) {
	limits := DefaultLimits()
	limits.StoredBytes = 6
	s := newStore(limits)
	t.Cleanup(s.Close)
	meta, err := s.Capture(context.Background(), "", []byte("12"))
	require.NoError(t, err)
	ticket, err := s.BeginPreview(meta.ID)
	require.NoError(t, err)
	require.NoError(t, s.FinishPreview(ticket, []byte("3456"), nil))
	_, err = s.Capture(context.Background(), "", []byte("new"))
	require.NoError(t, err)
	_, err = s.Get(meta.ID)
	assert.ErrorIs(t, err, ErrNotFound)
	assert.Equal(t, 3, s.Stats().StoredBytes)
}

func TestPreviewTicketsCannotCrossStores(t *testing.T) {
	a, b := NewStore(), NewStore()
	t.Cleanup(a.Close)
	t.Cleanup(b.Close)
	ma, err := a.Capture(context.Background(), "", []byte("a"))
	require.NoError(t, err)
	mb, err := b.Capture(context.Background(), "", []byte("b"))
	require.NoError(t, err)
	ta, err := a.BeginPreview(ma.ID)
	require.NoError(t, err)
	tb, err := b.BeginPreview(mb.ID)
	require.NoError(t, err)
	assert.ErrorIs(t, b.StartPreview(ta), ErrStalePreview)
	assert.ErrorIs(t, b.FinishPreview(ta, []byte("wrong"), nil), ErrStalePreview)
	assert.Equal(t, 1, b.Stats().PendingPreviews)
	assert.NoError(t, b.FinishPreview(tb, []byte("right"), nil))
}
