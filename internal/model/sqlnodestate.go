package model

import "time"

// Phase 11 (PLAN §12 p. 165, CHK-SQL-14, EC Service request A): state of a
// 1C exchange-plan node for the stalled-exchange detector. 1C _NodeNNN
// tables keep the message numbers in _SentNo / _ReceivedNo
// (DefaultSentNoColumn / DefaultReceivedNoColumn) and have no time
// columns; LastSend / LastRecv are the times the numbers were last seen
// changing (counters mode, ApplyCounters, DBZone), *Baseline when that is
// only the first observation. FreshAt is the later of the two.
type SQLNodeState struct {
	Key        string `json:"key"`
	Name       string `json:"name"`
	PlanTable  string `json:"planTable,omitempty"`
	PlanNo     int    `json:"planNo,omitempty"`
	PlanLabel  string `json:"planLabel,omitempty"`
	SentNo     int64  `json:"sentNo"`
	ReceivedNo int64  `json:"receivedNo"`
	// LastSend / LastRecv / FreshAt in DBZone.
	LastSend     time.Time `json:"lastSend"`
	LastRecv     time.Time `json:"lastRecv"`
	SendBaseline bool      `json:"sendBaseline,omitempty"`
	RecvBaseline bool      `json:"recvBaseline,omitempty"`
	FreshAt      time.Time `json:"freshAt"`
}

// NodeStateFrom joins one observation with its change times.
func NodeStateFrom(o CounterObservation, t CounterTimes) SQLNodeState {
	s := SQLNodeState{Key: o.Key, Name: o.Name, SentNo: o.SentNo, ReceivedNo: o.ReceivedNo,
		LastSend: t.LastSend, LastRecv: t.LastRecv, SendBaseline: t.SendBaseline, RecvBaseline: t.RecvBaseline, FreshAt: t.LastSend}
	if t.LastRecv.After(s.FreshAt) {
		s.FreshAt = t.LastRecv
	}
	return s
}
