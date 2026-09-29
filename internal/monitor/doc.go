// Package monitor — the service's poll loop (PLAN.md §8): all enabled
// profiles are polled every PollIntervalSec (default 60 s) with bounded
// concurrency, a start jitter and a per-profile time budget; a panic or a
// hung reader of one profile never affects the others. Config files are
// re-read when their size/mtime change (polled with os.Stat, no fsnotify)
// or on the IPC "reload_config" command. Every poll produces a
// model.StatusSnapshot (FreshAt = max(LastSend, LastRecv), UTC+3, levels
// from the profile thresholds, profile = worst point, error = Error) and
// debounced alert events (internal/alert). The counters-mode state is kept
// in FileCounterStore (counters-state.json next to the config).
//
// Check IDs: CHK-MON-01 (status), CHK-MON-02 (counters state file),
// CHK-MON-03 (poll loop), CHK-MON-04 (config reload), CHK-MON-09
// (SQL Server integration).
package monitor
