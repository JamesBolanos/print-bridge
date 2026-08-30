// Package logging writes rotating activity logs for printer-bridge.
package logging

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"printer-bridge/internal/activity"
)

const (
	// DefaultMaxBytes is the default maximum size for one log file.
	DefaultMaxBytes int64 = 5 * 1024 * 1024
	// DefaultMaxBackups is the default number of rotated log files retained.
	DefaultMaxBackups = 3
)

// Logger writes activity entries to disk and optionally mirrors them in memory.
type Logger struct {
	path       string
	maxBytes   int64
	maxBackups int
	activity   *activity.Store
	mu         sync.Mutex
}

// New creates a rotating activity logger.
func New(path string, maxBytes int64, maxBackups int, activityStore *activity.Store) *Logger {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	if maxBackups < 0 {
		maxBackups = DefaultMaxBackups
	}

	return &Logger{
		path:       path,
		maxBytes:   maxBytes,
		maxBackups: maxBackups,
		activity:   activityStore,
	}
}

// Path returns the active log file path.
func (l *Logger) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// Record writes an activity entry to the in-memory store and log file.
func (l *Logger) Record(entry activity.Entry) {
	if l == nil {
		return
	}
	if l.activity != nil {
		l.activity.Add(entry)
	}
	if err := l.writeLine(entry.LogLine()); err != nil {
		log.Printf("printer-bridge log write failed: %v", err)
	}
}

// ReadLinesNewestFirst returns recent log lines with the newest line first.
func (l *Logger) ReadLinesNewestFirst(limit int) ([]string, error) {
	if l == nil || l.path == "" {
		return nil, nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	data, err := os.ReadFile(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	text := strings.TrimRight(string(data), "\n")
	if text == "" {
		return nil, nil
	}

	lines := strings.Split(text, "\n")
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	if limit > 0 && len(lines) > limit {
		lines = lines[:limit]
	}
	return lines, nil
}

func (l *Logger) writeLine(line string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return err
	}
	if err := l.rotateIfNeeded(int64(len(line))); err != nil {
		return err
	}

	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err = file.WriteString(line); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func (l *Logger) rotateIfNeeded(incoming int64) error {
	if l.maxBytes <= 0 || l.maxBackups == 0 {
		return nil
	}

	info, err := os.Stat(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Size()+incoming <= l.maxBytes {
		return nil
	}

	oldest := l.rotatedPath(l.maxBackups)
	_ = os.Remove(oldest)

	for i := l.maxBackups - 1; i >= 1; i-- {
		src := l.rotatedPath(i)
		dst := l.rotatedPath(i + 1)
		if _, err := os.Stat(src); err == nil {
			if err := os.Rename(src, dst); err != nil {
				return err
			}
		}
	}

	return os.Rename(l.path, l.rotatedPath(1))
}

func (l *Logger) rotatedPath(index int) string {
	return l.path + "." + strconv.Itoa(index)
}
