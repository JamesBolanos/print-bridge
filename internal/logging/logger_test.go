package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"print-bridge/internal/activity"
)

func TestRecordWritesLogAndActivity(t *testing.T) {
	store := activity.NewStore(5)
	path := filepath.Join(t.TempDir(), "print-bridge.log")
	logger := New(path, 1024, 3, store)

	entry := activity.Entry{
		Timestamp: time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC),
		Endpoint:  "/print",
		Target:    "127.0.0.1:9100",
		Outcome:   "success",
		Detail:    "OK",
	}
	logger.Record(entry)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "endpoint=/print")
	assert.Len(t, store.List(), 1)
}

func TestReadLinesNewestFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "print-bridge.log")
	require.NoError(t, os.WriteFile(path, []byte("old\nnew\n"), 0o644))

	lines, err := New(path, 1024, 3, nil).ReadLinesNewestFirst(0)

	require.NoError(t, err)
	assert.Equal(t, []string{"new", "old"}, lines)
}

func TestRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "print-bridge.log")
	logger := New(path, 40, 2, nil)

	logger.Record(activity.Entry{Timestamp: time.Now(), Endpoint: "/print", Outcome: "first", Detail: strings.Repeat("a", 50)})
	logger.Record(activity.Entry{Timestamp: time.Now(), Endpoint: "/print", Outcome: "second", Detail: strings.Repeat("b", 50)})

	_, err := os.Stat(path + ".1")
	assert.NoError(t, err)
}
