package model

import (
	"encoding/json"
	"time"
)

// Phase 11 (EC Service, CHK-IPC-12 / CHK-IPC-13, contract with EC Tray):
// sql_browse v2 (modes, progress pushes) and sql_test_steps (the server
// check stage by stage, sqlcheck.CheckServer).
const (
	// CmdSQLTestSteps {server, profileId?}: admin, async (cancel {ref});
	// reply MsgSQLTestSteps (IPCMessage.SQLTestSteps).
	CmdSQLTestSteps = "sql_test_steps"
	MsgSQLTestSteps = "sql_test_steps"

	// Progress pushes (same envelope as every push: {"type":...}).
	MsgSQLBrowseProgress    = "sql_browse_progress"
	MsgSQLTestStepsProgress = "sql_test_steps_progress"

	// Capabilities (EC Tray names).
	CapBrowseSubnet = "browse_subnet" // sql_browse mode subnet (+ host / broadcast v2 fields, progress)
	CapBrowseLocal  = "browse_local"  // sql_browse mode local
	CapTestSteps    = "test_steps"    // sql_test_steps

	// Rate limits of sql_browse per mode (softened in phase 11: the
	// former 15 s for any broadcast). Every limit answers rate_limited +
	// retryAfterMs; a browse already running answers busy + retryAfterMs
	// (its remaining time).
	SQLBrowseLocalInterval  = 3 * time.Second
	SQLBrowseSubnetInterval = 15 * time.Second
	// IPCSQLTestStepsTimeout: the whole sql_test_steps request.
	IPCSQLTestStepsTimeout = 60 * time.Second
	// SQLBrowseProgressEvery throttles sql_browse_progress pushes (the
	// first and the "done" phase are always sent; a phase change too).
	SQLBrowseProgressEvery = 100 * time.Millisecond
)

// sqlBrowseProgressPush / sqlTestStepPush are the flat push frames:
// {"type":"sql_browse_progress","ref","phase","done","total","found"},
// {"type":"sql_test_steps_progress","ref","id","ok","ms","detail"?,
// "code"?,"errorKey"?,"adviceKey"?}. The step id is a string and
// collides with IPCMessage.ID, hence the custom (un)marshalling.
type sqlBrowseProgressPush struct {
	Type string `json:"type"`
	Ref  uint64 `json:"ref"`
	SQLBrowseProgress
}

type sqlTestStepPush struct {
	Type string `json:"type"`
	Ref  uint64 `json:"ref"`
	SQLCheckStep
}

type ipcMessageAlias IPCMessage

// MarshalJSON writes the progress pushes flat; every other message as is.
func (m IPCMessage) MarshalJSON() ([]byte, error) {
	switch {
	case m.Type == MsgSQLBrowseProgress && m.BrowseProgress != nil:
		return json.Marshal(sqlBrowseProgressPush{Type: m.Type, Ref: m.Ref, SQLBrowseProgress: *m.BrowseProgress})
	case m.Type == MsgSQLTestStepsProgress && m.TestStep != nil:
		return json.Marshal(sqlTestStepPush{Type: m.Type, Ref: m.Ref, SQLCheckStep: *m.TestStep})
	}
	return json.Marshal(ipcMessageAlias(m))
}

// UnmarshalJSON reads the flat progress pushes into Ref + BrowseProgress /
// TestStep.
func (m *IPCMessage) UnmarshalJSON(b []byte) error {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return err
	}
	switch head.Type {
	case MsgSQLBrowseProgress:
		var p sqlBrowseProgressPush
		if err := json.Unmarshal(b, &p); err != nil {
			return err
		}
		*m = IPCMessage{Type: p.Type, Ref: p.Ref, BrowseProgress: &p.SQLBrowseProgress}
		return nil
	case MsgSQLTestStepsProgress:
		var p sqlTestStepPush
		if err := json.Unmarshal(b, &p); err != nil {
			return err
		}
		*m = IPCMessage{Type: p.Type, Ref: p.Ref, TestStep: &p.SQLCheckStep}
		return nil
	}
	return json.Unmarshal(b, (*ipcMessageAlias)(m))
}
