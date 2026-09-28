package model

import "time"

// DBZone is the fixed time zone of all datetime values stored in the
// monitored MSSQL databases: exactly UTC+3, no DST (PLAN.md §4.5, §12 p.3).
var DBZone = time.FixedZone("UTC+3", 3*60*60)

// FromDBNaive reinterprets the wall clock of t (a zone-less MSSQL datetime,
// whatever Location the driver attached to it) as a wall clock in DBZone.
func FromDBNaive(t time.Time) time.Time {
	y, mo, d := t.Date()
	h, mi, s := t.Clock()
	return time.Date(y, mo, d, h, mi, s, t.Nanosecond(), DBZone)
}

// InDBZone converts an instant to the DBZone scale (same instant).
func InDBZone(t time.Time) time.Time { return t.In(DBZone) }

// NowDB returns the current instant expressed in DBZone.
func NowDB() time.Time { return time.Now().In(DBZone) }
