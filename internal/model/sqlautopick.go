package model

// Phase 9 (PLAN §12 p. 98, CHK-SQL-08): automatic choice of the Discovery
// candidate. The wizard may apply a candidate without asking only when the
// result is unambiguous: one exchange plan, a strong best candidate and a
// clear margin over the runner-up. Thresholds are in Discovery score
// points (sqlcheck heuristics: base 35 + name/series hints + freshness +
// link quality + _Node bonus 40 + per-point check): a linked candidate of
// an exchange-plan table _NodeNNN with fresh send and recv series scores
// ~140-160; the best non-1C / non-node candidates stay below 120.

const (
	// SQLAutoPickMinScore: the best candidate must score at least this.
	SQLAutoPickMinScore = 120
	// SQLAutoPickMinMargin: and lead the runner-up by at least this.
	SQLAutoPickMinMargin = 30
)

// Auto-pick reason keys (SQLAutoPick.ReasonKey; RU/EN in SQLMessage).
const (
	MsgSQLAutoPickOK            = "sql.autopick_ok"             // applied automatically
	MsgSQLAutoPickNone          = "sql.autopick_none"           // no candidate
	MsgSQLAutoPickMultiplePlans = "sql.autopick_multiple_plans" // several exchange plans: the user chooses
	MsgSQLAutoPickCounters      = "sql.autopick_counters"       // best is the counters fallback
	MsgSQLAutoPickIncomplete    = "sql.autopick_incomplete"     // best lacks send+recv, points, or the search was cut short
	MsgSQLAutoPickLowScore      = "sql.autopick_low_score"      // best scores below SQLAutoPickMinScore
	MsgSQLAutoPickAmbiguous     = "sql.autopick_ambiguous"      // runner-up within SQLAutoPickMinMargin (or a tie)
)

// SQLAutoPick is the auto-pick decision for a Discovery result.
type SQLAutoPick struct {
	// Apply: the candidate at Index may be applied without asking.
	Apply bool `json:"apply"`
	// Index of the best candidate in the list (-1: none).
	Index int `json:"index"`
	// CandidateKey is DiscoveryCandidate.Key() of the best candidate.
	CandidateKey string `json:"candidateKey,omitempty"`
	ReasonKey    string `json:"reasonKey"`
	Score        int    `json:"score,omitempty"`
	// Margin is Score minus the runner-up's score (Score if alone).
	Margin int `json:"margin,omitempty"`
}

// SQLAutoPickFor decides on the candidates of one Discovery run (any
// order). It never applies when candidates come from more than one
// exchange plan table, when the best one is the counters fallback, lacks
// a send or recv column, read no points, carries truncated /
// no_timestamps / future_dates / single_series hints, scores below
// SQLAutoPickMinScore or leads by less than SQLAutoPickMinMargin.
func SQLAutoPickFor(cands []DiscoveryCandidate) SQLAutoPick {
	if len(cands) == 0 {
		return SQLAutoPick{Index: -1, ReasonKey: MsgSQLAutoPickNone}
	}
	best := 0
	for i, c := range cands {
		if c.Score > cands[best].Score {
			best = i
		}
	}
	second, has2 := 0, false
	for i, c := range cands {
		if i != best && (!has2 || c.Score > second) {
			second, has2 = c.Score, true
		}
	}
	b := cands[best]
	res := SQLAutoPick{Index: best, CandidateKey: b.Key(), Score: b.Score, Margin: b.Score}
	if has2 {
		res.Margin = b.Score - second
	}
	plans := map[string]bool{}
	for _, c := range cands {
		if c.PlanTable != "" {
			plans[c.PlanTable] = true
		}
	}
	incomplete := b.SendCol == "" || b.RecvCol == "" || b.Points == 0
	for _, h := range []string{HintTruncated, HintNoTimestamps, HintFutureDates, HintSingleSeries} {
		incomplete = incomplete || b.HasHint(h)
	}
	switch {
	case len(plans) > 1:
		res.ReasonKey = MsgSQLAutoPickMultiplePlans
	case b.Mode == CandidateCounters:
		res.ReasonKey = MsgSQLAutoPickCounters
	case incomplete:
		res.ReasonKey = MsgSQLAutoPickIncomplete
	case b.Score < SQLAutoPickMinScore:
		res.ReasonKey = MsgSQLAutoPickLowScore
	case res.Margin < SQLAutoPickMinMargin:
		res.ReasonKey = MsgSQLAutoPickAmbiguous
	default:
		res.Apply, res.ReasonKey = true, MsgSQLAutoPickOK
	}
	return res
}
