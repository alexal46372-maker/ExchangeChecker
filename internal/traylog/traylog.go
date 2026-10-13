// Package traylog is the tray log file (phase 9b, CHK-TRAY-28):
// %LOCALAPPDATA%\ExchangeChecker\tray.log, at most MaxSize bytes, one
// older file tray.log.1. Lines never carry passwords or logins: callers
// do not pass them, and Scrub masks key=value pairs that look like
// credentials as a second line of defence.
package traylog

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// MaxSize is the size limit of tray.log; the full file becomes tray.log.1
// (replacing the previous one).
const MaxSize = 1 << 20

// FileName / Dir under %LOCALAPPDATA%.
const (
	FileName = "tray.log"
	Dir      = "ExchangeChecker"
)

// DefaultPath is %LOCALAPPDATA%\ExchangeChecker\tray.log ("" when
// LOCALAPPDATA is not set).
func DefaultPath() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return ""
	}
	return filepath.Join(base, Dir, FileName)
}

// Logger appends lines to a size-limited file. Safe for concurrent use.
type Logger struct {
	Path string
	Max  int64 // 0 = MaxSize
	Now  func() time.Time

	mu sync.Mutex
}

var credRe = regexp.MustCompile(`(?i)\b(password|passwd|pwd|login|user(?: ?id)?|uid)\s*([=:])\s*("[^"]*"|'[^']*'|[^\s;,]+)`)

// Scrub masks credential-looking key=value / key: value pairs.
func Scrub(s string) string { return credRe.ReplaceAllString(s, "$1$2***") }

// Log writes one line (time stamp, scrubbed text, CRLF). Errors are
// returned but a tray never fails because of its log.
func (l *Logger) Log(line string) error {
	if l == nil || l.Path == "" {
		return nil
	}
	now := time.Now
	if l.Now != nil {
		now = l.Now
	}
	max := l.Max
	if max <= 0 {
		max = MaxSize
	}
	text := strings.NewReplacer("\r", " ", "\n", " ").Replace(Scrub(line))
	b := []byte(now().Format("2006-01-02 15:04:05.000 -07:00") + " " + text + "\r\n")
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.Path), 0o700); err != nil {
		return err
	}
	if fi, err := os.Stat(l.Path); err == nil && fi.Size()+int64(len(b)) > max {
		old := l.Path + ".1"
		os.Remove(old)
		if err := os.Rename(l.Path, old); err != nil {
			return fmt.Errorf("traylog: rotate: %w", err)
		}
	}
	f, err := os.OpenFile(l.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// Func adapts the logger to Options.Log.
func (l *Logger) Func() func(string) { return func(s string) { l.Log(s) } }
