package model

import "strings"

// Phase 9 (PLAN §12 p. 107, CHK-SQL-08): automatic choice of the Discovery
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
	MsgSQLAutoPickMultiplePlans = "sql.autopick_multiple_plans" // kept for old services (CHK-SQL-16: plans are merged, no longer emitted)
	MsgSQLAutoPickCountersOK    = "sql.autopick_counters_ok"    // counters-only 1C 8.1 plans applied automatically (CHK-SQL-16)
	MsgSQLAutoPickCounters      = "sql.autopick_counters"       // best is the counters fallback
	MsgSQLAutoPickIncomplete    = "sql.autopick_incomplete"     // best lacks send+recv, points, or the search was cut short
	MsgSQLAutoPickLowScore      = "sql.autopick_low_score"      // best scores below SQLAutoPickMinScore
	MsgSQLAutoPickAmbiguous     = "sql.autopick_ambiguous"      // non-plan runner-up for the same table within SQLAutoPickMinMargin (or a tie)
)

// CapAutoPickPlans: the service's discover autoPick carries Plans and
// merges exchange plans (CHK-SQL-16; auto-pick version 2).
const CapAutoPickPlans = "autopick_plans_v2"

// SQLAutoPick is the auto-pick decision for a Discovery result.
type SQLAutoPick struct {
	// AutoApply: the candidate at Index may be applied without asking.
	AutoApply bool `json:"autoApply"`
	// Index of the best candidate in the list (-1: none).
	Index int `json:"index"`
	// CandidateKey is DiscoveryCandidate.Key() of the best candidate.
	CandidateKey string `json:"candidateKey,omitempty"`
	ReasonKey    string `json:"reasonKey"`
	Score        int    `json:"score,omitempty"`
	// Margin is Score minus the runner-up's score (Score if alone).
	Margin int `json:"margin,omitempty"`
	// Confidence of the best candidate (0..1, SQLConfidence) and the
	// server-side threshold AutoPickMin (= SQLAutoPickMinConfidence).
	Confidence  float64 `json:"confidence"`
	AutoPickMin float64 `json:"autoPickMin"`
	// Ranks: one entry per candidate, same order as the input list.
	Ranks []SQLCandidateRank `json:"ranks"`
	// Plans: index of the chosen candidate of every exchange plan, main
	// plan first (= Index); empty without _NodeNNN candidates
	// (CHK-SQL-16, capability CapAutoPickPlans).
	Plans []int `json:"plans,omitempty"`
}

// SQLCandidateRank annotates one Discovery candidate for the wizard
// (DiscoveryCandidate itself is unchanged).
type SQLCandidateRank struct {
	// Rank: 1 = best (score descending, input order on ties).
	Rank       int      `json:"rank"`
	Confidence float64  `json:"confidence"`
	Reasons    []string `json:"reasons"`
}

// Confidence scale: SQLConfidence(score) = score / SQLConfidenceFullScore
// clamped to 0..1, so SQLAutoPickMinScore maps to 0.6.
const (
	SQLConfidenceFullScore   = 200
	SQLAutoPickMinConfidence = float64(SQLAutoPickMinScore) / SQLConfidenceFullScore
)

// Candidate reason keys (SQLCandidateRank.Reasons; RU/EN in SQLMessage).
const (
	MsgSQLCandExchangePlan = "sql.cand_exchange_plan" // exchange plan table (_NodeNNN)
	MsgSQLCandSendRecv     = "sql.cand_send_recv"     // both send and recv times
	MsgSQLCandCounters     = "sql.cand_counters"      // message counters, no times
	MsgSQLCandIncomplete   = "sql.cand_incomplete"    // missing send/recv, points or cut short
	MsgSQLCandLowScore     = "sql.cand_low_score"     // below the auto-pick score
)

// SQLCandidateReasonKeys lists the candidate reason keys.
func SQLCandidateReasonKeys() []string {
	return []string{MsgSQLCandExchangePlan, MsgSQLCandSendRecv, MsgSQLCandCounters, MsgSQLCandIncomplete, MsgSQLCandLowScore}
}

// SQLConfidence maps a Discovery score to 0..1.
func SQLConfidence(score int) float64 {
	c := float64(score) / SQLConfidenceFullScore
	if c < 0 {
		return 0
	}
	if c > 1 {
		return 1
	}
	return c
}

func candIncomplete(c DiscoveryCandidate) bool {
	inc := c.SendCol == "" || c.RecvCol == "" || c.Points == 0
	for _, h := range []string{HintTruncated, HintNoTimestamps, HintFutureDates, HintSingleSeries} {
		inc = inc || c.HasHint(h)
	}
	return inc
}

func sqlCandidateRanks(cands []DiscoveryCandidate) []SQLCandidateRank {
	out := make([]SQLCandidateRank, len(cands))
	for i, c := range cands {
		rank := 1
		for j, o := range cands {
			if o.Score > c.Score || o.Score == c.Score && j < i {
				rank++
			}
		}
		r := SQLCandidateRank{Rank: rank, Confidence: SQLConfidence(c.Score), Reasons: []string{}}
		if c.PlanTable != "" {
			r.Reasons = append(r.Reasons, MsgSQLCandExchangePlan)
		}
		if c.SendCol != "" && c.RecvCol != "" {
			r.Reasons = append(r.Reasons, MsgSQLCandSendRecv)
		}
		if c.Mode == CandidateCounters {
			r.Reasons = append(r.Reasons, MsgSQLCandCounters)
		} else if candIncomplete(c) {
			r.Reasons = append(r.Reasons, MsgSQLCandIncomplete)
		}
		if c.Score < SQLAutoPickMinScore {
			r.Reasons = append(r.Reasons, MsgSQLCandLowScore)
		}
		out[i] = r
	}
	return out
}

// planOf is the exchange plan of a candidate: PlanTable, or the times
// table's plan for a linked candidate whose point table is a _NodeNNN.
func planOf(c DiscoveryCandidate) string { return c.PlanTable }

// planPref orders candidates inside one plan (CHK-SQL-16): a complete
// linked/single time candidate first, then counters, then an incomplete
// time candidate; higher score wins inside each class.
func planPref(c DiscoveryCandidate) int {
	switch {
	case c.Mode != CandidateCounters && !candIncomplete(c):
		return 0
	case c.Mode == CandidateCounters:
		return 1
	}
	return 2
}

// SQLAutoPickFor decides on the candidates of one Discovery run (any
// order; CHK-SQL-08, CHK-SQL-16..18). Candidates of exchange plans
// (_NodeNNN) are grouped by PlanTable and the best one of each plan is
// kept (planPref, then score); several plans are merged, not ambiguous:
// the main plan has the highest score, then more points, then the lower
// PlanNo; Plans lists the chosen candidate of each plan, main first. A
// complete time candidate or a counters-only plan (1C 8.1 without time
// registers, reason autopick_counters_ok) is applied automatically.
// Without plans the old rules hold (complete, SQLAutoPickMinScore), and
// ambiguity is reported only when a runner-up for the SAME table is
// within SQLAutoPickMinMargin.
func SQLAutoPickFor(cands []DiscoveryCandidate) SQLAutoPick {
	if len(cands) == 0 {
		return SQLAutoPick{Index: -1, ReasonKey: MsgSQLAutoPickNone, AutoPickMin: SQLAutoPickMinConfidence, Ranks: []SQLCandidateRank{}}
	}
	ranks := sqlCandidateRanks(cands)
	// best candidate per plan
	perPlan := map[string]int{}
	var order []string
	for i, c := range cands {
		pl := planOf(c)
		if pl == "" {
			continue
		}
		j, ok := perPlan[pl]
		if !ok {
			perPlan[pl] = i
			order = append(order, pl)
			continue
		}
		o := cands[j]
		if pc, po := planPref(c), planPref(o); pc < po || pc == po && c.Score > o.Score {
			perPlan[pl] = i
		}
	}
	margin := func(best int) int {
		second, has := 0, false
		for i, c := range cands {
			if i != best && (!has || c.Score > second) {
				second, has = c.Score, true
			}
		}
		if !has {
			return cands[best].Score
		}
		return cands[best].Score - second
	}
	mk := func(best int) SQLAutoPick {
		b := cands[best]
		return SQLAutoPick{Index: best, CandidateKey: b.Key(), Score: b.Score, Margin: margin(best),
			Confidence: SQLConfidence(b.Score), AutoPickMin: SQLAutoPickMinConfidence, Ranks: ranks}
	}
	if len(perPlan) > 0 {
		idx := make([]int, 0, len(order))
		for _, pl := range order {
			idx = append(idx, perPlan[pl])
		}
		// main plan first: class, score, points, lower PlanNo, table name
		less := func(a, b DiscoveryCandidate) bool {
			if pa, pb := planPref(a), planPref(b); pa != pb {
				return pa < pb
			}
			if a.Score != b.Score {
				return a.Score > b.Score
			}
			if a.Points != b.Points {
				return a.Points > b.Points
			}
			if a.PlanNo != b.PlanNo {
				return a.PlanNo < b.PlanNo
			}
			return a.PlanTable < b.PlanTable
		}
		for i := 1; i < len(idx); i++ { // insertion sort: stable, deterministic
			for j := i; j > 0 && less(cands[idx[j]], cands[idx[j-1]]); j-- {
				idx[j], idx[j-1] = idx[j-1], idx[j]
			}
		}
		res := mk(idx[0])
		res.Plans = idx
		b := cands[idx[0]]
		switch {
		case b.Mode == CandidateCounters:
			if b.Points == 0 {
				res.ReasonKey = MsgSQLAutoPickIncomplete
			} else {
				res.AutoApply, res.ReasonKey = true, MsgSQLAutoPickCountersOK
			}
		case candIncomplete(b):
			res.ReasonKey = MsgSQLAutoPickIncomplete
		default:
			res.AutoApply, res.ReasonKey = true, MsgSQLAutoPickOK
		}
		return res
	}
	best := 0
	for i, c := range cands {
		if c.Score > cands[best].Score {
			best = i
		}
	}
	res := mk(best)
	b := cands[best]
	ambiguous := false
	for i, c := range cands {
		if i != best && strings.EqualFold(c.Schema, b.Schema) && strings.EqualFold(c.Table, b.Table) && b.Score-c.Score < SQLAutoPickMinMargin {
			ambiguous = true
		}
	}
	switch {
	case b.Mode == CandidateCounters:
		res.ReasonKey = MsgSQLAutoPickCounters
	case candIncomplete(b):
		res.ReasonKey = MsgSQLAutoPickIncomplete
	case b.Score < SQLAutoPickMinScore:
		res.ReasonKey = MsgSQLAutoPickLowScore
	case ambiguous:
		res.ReasonKey = MsgSQLAutoPickAmbiguous
	default:
		res.AutoApply, res.ReasonKey = true, MsgSQLAutoPickOK
	}
	return res
}
