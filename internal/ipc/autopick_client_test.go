package ipc

import (
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_SQL_16_FitAutoPickClient — the discover reply's autoPick follows
// the client's client_hello: with autopick_plans_v2 merged plans are
// applied, without it the old contract (autopick_multiple_plans,
// AutoApply=false, no plans field).
func TestCHK_SQL_16_FitAutoPickClient(t *testing.T) {
	cands := func() []model.DiscoveryCandidate {
		c := func(plan string, score int) model.DiscoveryCandidate {
			return model.DiscoveryCandidate{Mode: model.CandidateSingle, Table: plan, SendCol: "s", RecvCol: "r", Points: 3, Score: score, PlanTable: plan}
		}
		return []model.DiscoveryCandidate{c("_Node1", 155), c("_Node2", 148)}
	}
	newM := model.IPCMessage{Type: model.MsgDiscovery, Candidates: cands()}
	fitAutoPick(&newM, model.IPCMaxInteractiveReply, true)
	if a := newM.AutoPick; a == nil || !a.AutoApply || a.ReasonKey != model.MsgSQLAutoPickOK || len(a.Plans) != 2 {
		t.Errorf("v2 client %+v", a)
	}
	oldM := model.IPCMessage{Type: model.MsgDiscovery, Candidates: cands()}
	fitAutoPick(&oldM, model.IPCMaxInteractiveReply, false)
	if a := oldM.AutoPick; a == nil || a.AutoApply || a.ReasonKey != model.MsgSQLAutoPickMultiplePlans || a.Plans != nil {
		t.Errorf("old client %+v", a)
	}
}
