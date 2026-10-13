// Package history keeps the poll history of the service (phase 9,
// CHK-SVC-03): per-point and per-profile poll results and the level
// transitions, one JSON line per record, one file per day (UTC+3) under
// %ProgramData%\ExchangeChecker\history. No server, login, password, SQL
// or free error text is ever written; only known error keys.
//
// Writing never blocks the poller: records go through a bounded queue to
// one writer goroutine (dropped and counted when full). Files are only
// appended; a torn last line (crash) is cut off when the file is opened
// again and skipped by readers. Retention: model.HistoryDays days,
// model.HistoryDayCap bytes per day (point records are dropped above it,
// profile and event records keep going up to 1 MiB more) and
// model.HistoryTotalCap bytes for the folder (oldest days removed).
//
// Query serves the IPC command history: a page of at most
// model.IPCMaxInteractiveReply bytes with an opaque continuation cursor.
package history
