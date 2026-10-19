package model

import (
	"sort"
	"time"
)

// Phase 10 (EC Service, CHK-MON-17): the alarm persists until the error
// is resolved. Every status push and get_status carry HasProblems (always
// sent, false explicitly) and Problems: the active (debounced) alerts as a
// flat list. After every failing check of an active alert the service
// also pushes the alert again (AlertEvent.StillFailing / Repeat), also
// when it is acknowledged, so EC Tray can show its window again.

// CapProblems: the service sends hasProblems / problems and repeat alert
// pushes (stillFailing).
const CapProblems = "problems"

// Problem kinds.
const (
	ProblemNoConnection = "no_connection" // profile: connection / login / timeout / hung
	ProblemError        = "error"         // profile: other error (query, mapping...), point: error level
	ProblemWarning      = "warning"       // point: warning level
)

// ProblemsMax caps StatusSnapshot.Problems (ProblemsTruncated then set).
const ProblemsMax = 500

// Problem is one unresolved alarm.
type Problem struct {
	Kind        string `json:"kind"`
	ProfileID   string `json:"profileId"`
	ProfileName string `json:"profileName,omitempty"`
	// PointKey / PointName: "" for a profile-level problem.
	PointKey  string `json:"pointKey,omitempty"`
	PointName string `json:"pointName,omitempty"`
	// Since: when the point / profile left ok (UTC+3).
	Since        *time.Time `json:"since,omitempty"`
	ErrorKey     string     `json:"errorKey,omitempty"`
	LagMinutes   int        `json:"lagMinutes,omitempty"`
	Acknowledged bool       `json:"acknowledged,omitempty"`
	Repeat       uint32     `json:"repeat,omitempty"`
	// EventID of the transition that raised the current level.
	EventID uint64 `json:"eventId,omitempty"`
}

// connProblemKeys: profile errors reported as no_connection.
var connProblemKeys = map[string]bool{
	MsgMonitorConnError: true, MsgMonitorTimeout: true, MsgMonitorHung: true, MsgMonitorBusy: true,
	MsgTestNetwork: true, MsgTestLogin: true, MsgTestTimeout: true,
	MsgSQLTLSLegacyFailed: true, MsgSQLTLSCertificate: true,
}

// ProblemKind classifies an active alert.
func ProblemKind(e AlertEvent) string {
	switch {
	case e.ProfileLevel() && e.Key == "":
		if connProblemKeys[e.ErrorKey] {
			return ProblemNoConnection
		}
		return ProblemError
	case e.To >= LevelError:
		return ProblemError
	}
	return ProblemWarning
}

func problemRank(k string) int {
	switch k {
	case ProblemNoConnection:
		return 0
	case ProblemError:
		return 1
	}
	return 2
}

// ProblemsOf builds the problem list of the active alerts: sorted by kind
// (no_connection, error, warning), since, profile, point; at most
// ProblemsMax entries (truncated reports a cut).
func ProblemsOf(alerts []AlertEvent) (out []Problem, truncated bool) {
	for _, a := range alerts {
		if a.To == LevelOK {
			continue
		}
		p := Problem{Kind: ProblemKind(a), ProfileID: a.ProfileID, ProfileName: a.ProfileName,
			PointKey: a.Key, PointName: a.PointName, Since: a.Since, ErrorKey: a.ErrorKey,
			LagMinutes: a.LagMinutes, Acknowledged: a.Acknowledged, Repeat: a.Repeat, EventID: a.ID}
		if p.Since == nil {
			at := a.At
			p.Since = &at
		}
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if ra, rb := problemRank(a.Kind), problemRank(b.Kind); ra != rb {
			return ra < rb
		}
		if !a.Since.Equal(*b.Since) {
			return a.Since.Before(*b.Since)
		}
		if a.ProfileID != b.ProfileID {
			return a.ProfileID < b.ProfileID
		}
		return a.PointKey < b.PointKey
	})
	if len(out) > ProblemsMax {
		out, truncated = out[:ProblemsMax], true
	}
	return out, truncated
}
