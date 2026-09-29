package model

import (
	"strings"
	"time"
)

// CandidateMode tells how a DiscoveryCandidate reads its timestamps.
type CandidateMode string

const (
	// CandidateSingle: point name and timestamps live in one table.
	CandidateSingle CandidateMode = "single"
	// CandidateLinked: points table + times table joined by a key, e.g.
	// 1C _Reference77._IDRRef = _InfoRg88._Fld89RRef.
	CandidateLinked CandidateMode = "linked"
	// CandidateCounters: fallback without stored times: the 1C exchange-plan
	// counters _SentNo/_ReceivedNo (SendCol/RecvCol name the counter
	// columns); the service observes when they change.
	CandidateCounters CandidateMode = "counters"
)

// Machine-readable discovery hints (DiscoveryCandidate.Hints). The tray
// localizes them; Reason is a non-localized English summary.
const (
	HintLinked          = "linked"             // points table + linked times table (binary(16) ref)
	HintSameTable       = "same_table"         // timestamps are columns of the points table
	HintNodeTable       = "node_table"         // points table is a 1C exchange-plan table _NodeNNN (bonus)
	HintGeneric         = "generic"            // name-based heuristic (non-1C schema)
	HintByValue         = "by_value"           // found by DiscoverByValue
	HintExactMatch      = "exact_match"        // point name matched exactly (else: substring)
	HintNearApproxTime  = "near_approx_time"   // a timestamp is within the tolerance of the given time
	HintAllPointsFresh  = "all_points_fresh"   // every point was updated close to the freshest one
	HintYearOffset2000  = "year_offset_2000"   // 1C +2000 year offset detected
	HintSendRecvByName  = "send_recv_by_name"  // send/recv columns chosen by column names
	HintSendRecvByOrder = "send_recv_by_order" // two freshest series, assigned by column order
	HintSingleSeries    = "single_series"      // only one timestamp column
	HintNoTimestamps    = "no_timestamps"      // no non-empty timestamps found yet
	HintFutureDates     = "future_dates"       // freshest value is in the future
	HintSampled         = "sampled"            // statistics computed on a bounded sample
	HintSampleFailed    = "sample_failed"      // SampleRows could not be read
	HintTruncated       = "truncated"          // the discovery time budget ran out; results are partial
	HintCounters        = "counters_fallback"  // times will be observed counter changes (baseline = service start)
)

// SampleRow is one example row of a candidate, already converted with the
// candidate's YearOffset and interpreted as UTC+3 (DBZone).
type SampleRow struct {
	Point    string     `json:"point"`
	LastSend *time.Time `json:"lastSend,omitempty"`
	LastRecv *time.Time `json:"lastRecv,omitempty"`
}

// DiscoveryCandidate is one proposed mapping returned by Discovery
// (PLAN.md §5.3). It is JSON-serializable for IPC (service -> tray).
type DiscoveryCandidate struct {
	Mode   CandidateMode `json:"mode"`
	Schema string        `json:"schema,omitempty"`
	// Table is the points table (single mode: also holds the timestamps).
	Table       string `json:"table"`
	PointCol    string `json:"pointCol"`
	PointIDCol  string `json:"pointIdCol,omitempty"`
	PointKeyCol string `json:"pointKeyCol,omitempty"`
	SendCol     string `json:"sendCol,omitempty"`
	RecvCol     string `json:"recvCol,omitempty"`
	// Linked mode: TimesTable.LinkCol = Table.PointKeyCol.
	TimesSchema string `json:"timesSchema,omitempty"`
	TimesTable  string `json:"timesTable,omitempty"`
	LinkCol     string `json:"linkCol,omitempty"`
	// YearOffset is 0 or YearOffset1C.
	YearOffset int        `json:"yearOffset"`
	FreshestAt *time.Time `json:"freshestAt,omitempty"`
	// Points is the number of points read through the mapping (0 = not read).
	Points int `json:"points,omitempty"`
	// Matched is the row of the searched point (DiscoverByValue only).
	Matched    *SampleRow  `json:"matched,omitempty"`
	SampleRows []SampleRow `json:"sampleRows,omitempty"`
	Score      int         `json:"score"`
	Reason     string      `json:"reason"`
	Hints      []string    `json:"hints,omitempty"`
}

// Mapping converts the candidate into a profile mapping (Mode auto).
func (c DiscoveryCandidate) Mapping() Mapping {
	m := Mapping{
		Mode:            MappingAuto,
		Schema:          c.Schema,
		Table:           c.Table,
		PointNameColumn: c.PointCol,
		PointIDColumn:   c.PointIDCol,
		LastSendColumn:  c.SendCol,
		LastRecvColumn:  c.RecvCol,
		YearOffset:      c.YearOffset,
	}
	if c.Mode == CandidateCounters {
		m.Mode = MappingCounters
		m.PointKeyColumn = c.PointKeyCol
		m.LastSendColumn, m.LastRecvColumn = "", ""
		m.SentNoColumn, m.ReceivedNoColumn = c.SendCol, c.RecvCol
		m.YearOffset = 0
	}
	if c.Mode == CandidateLinked {
		m.PointKeyColumn = c.PointKeyCol
		m.TimesSchema = c.TimesSchema
		m.TimesTable = c.TimesTable
		m.TimesLinkColumn = c.LinkCol
	}
	return m
}

// HasHint reports whether the candidate carries hint h.
func (c DiscoveryCandidate) HasHint(h string) bool {
	for _, x := range c.Hints {
		if x == h {
			return true
		}
	}
	return false
}

// Key identifies the candidate's table/column choice (for de-duplication).
func (c DiscoveryCandidate) Key() string {
	return strings.Join([]string{string(c.Mode), c.Schema, c.Table, c.PointCol, c.PointKeyCol,
		c.TimesSchema, c.TimesTable, c.LinkCol, c.SendCol, c.RecvCol}, "\x00")
}

// DefaultValueToleranceMin is the default ValueQuery tolerance (1 day).
const DefaultValueToleranceMin = 24 * 60

// ValueQuery is the input of DiscoverByValue: the user types the name of a
// known exchange point and, optionally, the approximate time of its last
// exchange. JSON-serializable for IPC (tray -> service).
type ValueQuery struct {
	PointName string `json:"pointName"`
	// ApproxTime is an instant (the tray converts the UTC+3 input).
	ApproxTime *time.Time `json:"approxTime,omitempty"`
	// ToleranceMin: a timestamp within this distance of ApproxTime counts
	// as "near" (0 = DefaultValueToleranceMin).
	ToleranceMin int `json:"toleranceMin,omitempty"`
}

// Tolerance returns ToleranceMin as a duration (default 1 day).
func (q ValueQuery) Tolerance() time.Duration {
	if q.ToleranceMin <= 0 {
		return DefaultValueToleranceMin * time.Minute
	}
	return time.Duration(q.ToleranceMin) * time.Minute
}
