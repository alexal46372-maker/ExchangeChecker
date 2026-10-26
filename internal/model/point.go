package model

import "time"

// ReasonNoTimestamps is put into ExchangePoint.Message when both LastSend and
// LastRecv are NULL. Decision: such a point is LevelError ("never exchanged
// or cannot tell" must never look healthy).
const ReasonNoTimestamps = "no_timestamps"

// ExchangePoint is the polled state of one 1C exchange node.
// LastSend / LastRecv are instants (already converted with FromDBNaive).
type ExchangePoint struct {
	ProfileID string     `json:"profileId,omitempty"`
	Name      string     `json:"name"`
	LastSend  *time.Time `json:"lastSend,omitempty"`
	LastRecv  *time.Time `json:"lastRecv,omitempty"`
	Level     AlertLevel `json:"level"`
	Message   string     `json:"message,omitempty"`
	CheckedAt time.Time  `json:"checkedAt"`
	// Source is "" for timestamp mappings and PointSourceCounters when the
	// times were derived from message counters (Mapping mode "counters").
	Source string `json:"source,omitempty"`
	// SendBaseline / RecvBaseline mark a counters-mode time that is only the
	// first observation after the service started: the real change happened
	// at or before that moment, so the UI shows "not earlier than service
	// start" instead of an exact time.
	SendBaseline bool `json:"sendBaseline,omitempty"`
	RecvBaseline bool `json:"recvBaseline,omitempty"`
	// PlanTable / PlanNo tag a point read through a 1C exchange-plan table
	// (_NodeNNN, NNN); PlanLabel is the mapping's user label. Empty for
	// non-plan mappings.
	PlanTable string `json:"planTable,omitempty"`
	PlanNo    int    `json:"planNo,omitempty"`
	PlanLabel string `json:"planLabel,omitempty"`
	// SentNo / ReceivedNo: 1C message numbers of a counters-mode point
	// (sqlcheck.ReadNodeStates, phase 11, CHK-MON-18).
	SentNo     *int64 `json:"sentNo,omitempty"`
	ReceivedNo *int64 `json:"receivedNo,omitempty"`
	// NodeKey identifies the 1C exchange-plan node of the point (phase 11).
	NodeKey string `json:"nodeKey,omitempty"`
}

// FreshAt returns the later of LastSend / LastRecv in DBZone; NULLs are ignored.
// ok is false when both are NULL.
func (p ExchangePoint) FreshAt() (t time.Time, ok bool) {
	switch {
	case p.LastSend == nil && p.LastRecv == nil:
		return time.Time{}, false
	case p.LastSend == nil:
		return p.LastRecv.In(DBZone), true
	case p.LastRecv == nil:
		return p.LastSend.In(DBZone), true
	case p.LastRecv.After(*p.LastSend):
		return p.LastRecv.In(DBZone), true
	default:
		return p.LastSend.In(DBZone), true
	}
}

// Lag returns now - FreshAt. A negative lag (DB clock ahead) is clamped
// to 0. ok is false when FreshAt is unknown.
func (p ExchangePoint) Lag(now time.Time) (lag time.Duration, ok bool) {
	f, ok := p.FreshAt()
	if !ok {
		return 0, false
	}
	lag = now.Sub(f)
	if lag < 0 {
		lag = 0
	}
	return lag, true
}

// LevelForLag maps a lag to a level: >= ErrorMinutes -> Error,
// >= WarnMinutes -> Warning, otherwise OK.
func LevelForLag(lag time.Duration, s Settings) AlertLevel {
	switch {
	case lag >= s.ErrorAfter():
		return LevelError
	case lag >= s.WarnAfter():
		return LevelWarning
	default:
		return LevelOK
	}
}

// Evaluate returns the level of point p at instant now. Both timestamps
// NULL -> LevelError (see ReasonNoTimestamps).
func Evaluate(p ExchangePoint, s Settings, now time.Time) AlertLevel {
	lag, ok := p.Lag(now)
	if !ok {
		return LevelError
	}
	return LevelForLag(lag, s)
}

// Refresh recomputes Level, Message and CheckedAt of p.
func (p *ExchangePoint) Refresh(s Settings, now time.Time) {
	p.Level = Evaluate(*p, s, now)
	p.CheckedAt = now.In(DBZone)
	if _, ok := p.FreshAt(); ok {
		p.Message = ""
	} else {
		p.Message = ReasonNoTimestamps
	}
}

// ProfileStatus is the status of one profile (database) with its points.
// Its Level drives the LMB list color: yellow = Warning, red = Error.
type ProfileStatus struct {
	ProfileID string          `json:"profileId"`
	Name      string          `json:"name"`
	Points    []ExchangePoint `json:"points"`
	// Error is a connection/query error; a non-empty Error means LevelError.
	Error string `json:"error,omitempty"`
	// ErrorKey is the localizable message key of Error (e.g.
	// "sql.tls_enable_tls10"), empty when there is none.
	ErrorKey  string    `json:"errorKey,omitempty"`
	CheckedAt time.Time `json:"checkedAt"`
}

// Level returns the worst level across points (LevelError on Error,
// LevelOK when there are no points and no error).
func (ps ProfileStatus) Level() AlertLevel {
	if ps.Error != "" {
		return LevelError
	}
	w := LevelOK
	for _, p := range ps.Points {
		w = Worst(w, p.Level)
	}
	return w
}
