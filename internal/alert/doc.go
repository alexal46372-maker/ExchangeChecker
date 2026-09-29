// Package alert — warning/error transitions of points and profiles with
// debounce (anti-flap), alert texts RU/EN, sinks: Windows Event Log
// (advapi32 RegisterEventSourceW / ReportEventW via internal/winapi, no
// helper processes) and a size-rotated log file (PLAN.md §6.3, §8).
//
// Check ID: CHK-MON-05.
package alert
