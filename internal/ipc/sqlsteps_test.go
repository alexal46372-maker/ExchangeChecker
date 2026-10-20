package ipc

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

type wiz2Handler struct{ wizHandler }

func (h *wiz2Handler) SQLBrowseProgress(ctx context.Context, req model.IPCRequest, p func(model.SQLBrowseProgress)) (model.SQLBrowseResult, error) {
	for i := 0; i < 50; i++ { // throttled: first, phase change, done
		p(model.SQLBrowseProgress{Phase: "subnet", Done: i, Total: 254, Found: 1})
	}
	p(model.SQLBrowseProgress{Phase: "done", Done: 254, Total: 254, Found: 1})
	return model.SQLBrowseResult{Mode: req.Mode, Sent: 0, Responded: 1, Instances: []model.SQLServerInstance{{Host: "srv", Instance: "MSSQLSERVER", Source: "tcp", TCPPort: 1433}},
		TCPProbe: &model.SQLBrowseTCPInfo{Port: 1433, Probed: 254, Open: 1, Prelogin: 1, CIDR: req.CIDR}}, nil
}

func (h *wiz2Handler) SQLTestSteps(ctx context.Context, req model.IPCRequest, s func(model.SQLCheckStep)) (model.SQLServerCheckResult, error) {
	st := []model.SQLCheckStep{{ID: "parse", OK: true}, {ID: "tcp", OK: false, Ms: 300, Code: "timeout", ErrorKey: "sql.diag_timeout",
		AdviceKey: model.MsgSQLCheckAdviceTCP, Detail: "10.0.0.5:1433"}}
	for _, x := range st {
		s(x)
	}
	return model.SQLServerCheckResult{Host: req.Server, Port: 1433, Steps: st}, nil
}

// TestCHK_IPC_12_ProgressFrames — the progress pushes are flat frames in
// the push envelope and decode back into Ref + BrowseProgress / TestStep.
func TestCHK_IPC_12_ProgressFrames(t *testing.T) {
	b, _ := json.Marshal(model.IPCMessage{Type: model.MsgSQLBrowseProgress, Ref: 7,
		BrowseProgress: &model.SQLBrowseProgress{Phase: "ssrp", Done: 1, Total: 3, Found: 2}})
	if string(b) != `{"type":"sql_browse_progress","ref":7,"phase":"ssrp","done":1,"total":3,"found":2}` {
		t.Errorf("browse frame %s", b)
	}
	b2, _ := json.Marshal(model.IPCMessage{Type: model.MsgSQLTestStepsProgress, Ref: 9,
		TestStep: &model.SQLCheckStep{ID: "tcp", OK: false, Ms: 300, Code: "timeout", ErrorKey: "sql.diag_timeout", AdviceKey: "sql.check_advice_tcp", Detail: "10.0.0.5:1433"}})
	if string(b2) != `{"type":"sql_test_steps_progress","ref":9,"id":"tcp","ok":false,"ms":300,"code":"timeout","errorKey":"sql.diag_timeout","adviceKey":"sql.check_advice_tcp","detail":"10.0.0.5:1433"}` {
		t.Errorf("step frame %s", b2)
	}
	var m model.IPCMessage
	if err := json.Unmarshal(b2, &m); err != nil || m.Ref != 9 || m.TestStep == nil || m.TestStep.ID != "tcp" || m.ID != 0 {
		t.Errorf("decode %+v %v", m, err)
	}
	// ordinary messages unchanged
	b3, _ := json.Marshal(model.IPCMessage{ID: 3, Type: model.MsgPong})
	if err := json.Unmarshal(b3, &m); err != nil || m.ID != 3 || m.Type != model.MsgPong || strings.Contains(string(b3), "ref") {
		t.Errorf("pong %s %+v", b3, m)
	}
}

// TestCHK_IPC_12_BrowseAndStepsIPC — capabilities browse_subnet /
// browse_local / test_steps; sql_browse pushes throttled
// sql_browse_progress {ref = request id} before the reply with the v2
// fields; sql_test_steps pushes one sql_test_steps_progress per stage and
// replies sqlTestSteps; both admin-only.
func TestCHK_IPC_12_BrowseAndStepsIPC(t *testing.T) {
	var admin int32
	h := &wiz2Handler{wizHandler{fakeHandler: fakeHandler{snap: testSnapshot()}, started: make(chan struct{}, 1), canceled: make(chan struct{}, 1)}}
	srv := NewServer(h, nil)
	srv.Authorize = func(io.ReadWriteCloser) (Peer, error) { return Peer{Admin: atomic.LoadInt32(&admin) == 1}, nil }
	l := NewMemListener()
	go srv.Serve(l)
	defer srv.Close()
	rw, err := l.Dial()
	if err != nil {
		t.Fatal(err)
	}
	pushes := make(chan model.IPCMessage, 256)
	c := NewClient(rw, func(m model.IPCMessage) { pushes <- m })
	defer c.Close()
	hello := <-pushes
	caps := "," + strings.Join(hello.Capabilities, ",") + ","
	for _, k := range []string{"browse_subnet", "browse_local", "test_steps"} {
		if !strings.Contains(caps, ","+k+",") {
			t.Errorf("capability %s: %s", k, caps)
		}
	}
	for _, cmd := range []string{model.CmdSQLBrowse, model.CmdSQLTestSteps} {
		if m := do(t, c, model.IPCRequest{Cmd: cmd, Server: "srv"}); m.Code != model.ErrCodeAccessDenied {
			t.Errorf("%s as a user %+v", cmd, m)
		}
	}
	atomic.StoreInt32(&admin, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m, err := c.Do(ctx, model.IPCRequest{Cmd: model.CmdSQLBrowse, Mode: "subnet", CIDR: "192.168.1.0/24"})
	if err != nil || m.SQLBrowse == nil || m.SQLBrowse.Mode != "subnet" || m.SQLBrowse.TCPProbe == nil || m.SQLBrowse.TCPProbe.CIDR != "192.168.1.0/24" {
		t.Fatalf("browse %+v %v", m, err)
	}
	var prog []model.SQLBrowseProgress
	var ref uint64
	for len(pushes) > 0 {
		p := <-pushes
		if p.Type == model.MsgSQLBrowseProgress {
			prog, ref = append(prog, *p.BrowseProgress), p.Ref
		}
	}
	if len(prog) < 2 || len(prog) > 10 || prog[0].Done != 0 || prog[len(prog)-1].Phase != "done" || ref == 0 {
		t.Errorf("progress %d %+v ref %d", len(prog), prog, ref)
	}
	m, err = c.Do(ctx, model.IPCRequest{Cmd: model.CmdSQLTestSteps, Server: "srv,1433"})
	if err != nil || m.Type != model.MsgSQLTestSteps || m.SQLTestSteps == nil || len(m.SQLTestSteps.Steps) != 2 {
		t.Fatalf("steps %+v %v", m, err)
	}
	var steps []model.SQLCheckStep
	for len(pushes) > 0 {
		if p := <-pushes; p.Type == model.MsgSQLTestStepsProgress && p.Ref == m.ID {
			steps = append(steps, *p.TestStep)
		}
	}
	if len(steps) != 2 || steps[1].AdviceKey != model.MsgSQLCheckAdviceTCP || steps[1].Detail != "10.0.0.5:1433" {
		t.Errorf("step pushes %+v", steps)
	}
}
