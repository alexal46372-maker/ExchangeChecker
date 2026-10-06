package ipc

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// discHandler adds a fake Discoverer to v2Handler.
type discHandler struct {
	*v2Handler
	mu      sync.Mutex
	calls   []string
	block   chan struct{} // discover blocks until closed or ctx done
	ctxDone chan struct{} // closed when a blocked call saw its ctx end
	started chan struct{}
	cands   []model.DiscoveryCandidate
	pts     []model.NodePoint
	check   model.MappingCheck
	err     error
	reasonD string // truncated reason of discover (discover_by_value: budget)
	reasonN string // of list_node_points
}

func (h *discHandler) rec(c string, ctx context.Context) error {
	h.mu.Lock()
	h.calls = append(h.calls, c)
	block, started, done, err := h.block, h.started, h.ctxDone, h.err
	h.mu.Unlock()
	if started != nil {
		started <- struct{}{}
	}
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			close(done)
			return ctx.Err()
		}
	}
	return err
}

func (h *discHandler) Calls() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.calls...)
}

func (h *discHandler) Discover(ctx context.Context, req model.IPCRequest) ([]model.DiscoveryCandidate, string, error) {
	if err := h.rec("discover:"+req.ProfileID, ctx); err != nil {
		return nil, "", err
	}
	return h.cands, h.reasonD, nil
}

func (h *discHandler) DiscoverByValue(ctx context.Context, req model.IPCRequest) ([]model.DiscoveryCandidate, string, error) {
	if err := h.rec("discover_by_value:"+req.Query.PointName, ctx); err != nil {
		return nil, "", err
	}
	return h.cands, model.TruncatedBudget, nil
}

func (h *discHandler) ListNodePoints(ctx context.Context, req model.IPCRequest) ([]model.NodePoint, string, error) {
	if err := h.rec("list_node_points", ctx); err != nil {
		return nil, "", err
	}
	return h.pts, h.reasonN, nil
}

func (h *discHandler) CheckMapping(ctx context.Context, req model.IPCRequest) (model.MappingCheck, error) {
	if err := h.rec("check_mapping:"+req.Mapping.Table, ctx); err != nil {
		return model.MappingCheck{}, err
	}
	return h.check, nil
}

func newDiscServer(t *testing.T, admin *int32) (*discHandler, *MemListener, *testLog) {
	t.Helper()
	v2, _, _, _ := newV2(t)
	h := &discHandler{v2Handler: v2}
	lg := &testLog{}
	s := NewServer(h, lg)
	s.Authorize = func(io.ReadWriteCloser) (Peer, error) {
		return Peer{Admin: atomic.LoadInt32(admin) == 1, Account: `HOST\alex`}, nil
	}
	l := NewMemListener()
	go s.Serve(l)
	t.Cleanup(func() { s.Close() })
	return h, l, lg
}

func interactiveReqs(id uint64) []model.IPCRequest {
	mp := model.Mapping{Table: "_Node1"}
	return []model.IPCRequest{
		{ID: id, Cmd: model.CmdDiscover, ProfileID: "p1"},
		{ID: id + 1, Cmd: model.CmdDiscoverByValue, ProfileID: "p1", Query: &model.ValueQuery{PointName: "A"}},
		{ID: id + 2, Cmd: model.CmdListNodePoints, ProfileID: "p1"},
		{ID: id + 3, Cmd: model.CmdCheckMapping, ProfileID: "p1", Mapping: &mp, Limit: 5},
	}
}

// TestCHK_IPC_08_ServerAdminAndCapabilities — the four commands are
// announced (discover, discover_by_value, node_points, check_mapping, and
// cancel) only by a handler that implements them (else unknown_command);
// a non-admin client gets access_denied (checked on every request, the
// handler is not called); an admin gets discovery / node_points /
// mapping_check replies.
func TestCHK_IPC_08_ServerAdminAndCapabilities(t *testing.T) {
	var admin int32
	h, l, lg := newDiscServer(t, &admin)
	h.cands = []model.DiscoveryCandidate{{Table: "_Node1", PointCol: "_Description"}}
	h.pts = []model.NodePoint{{PlanTable: "_Node1", Name: "A"}}
	h.check = model.MappingCheck{OK: true, SampleRows: []model.SampleRow{{Point: "A"}}}
	rw, _ := l.Dial()
	defer rw.Close()
	var hello model.IPCMessage
	if err := ReadMessage(rw, &hello); err != nil {
		t.Fatal(err)
	}
	caps := "," + strings.Join(hello.Capabilities, ",") + ","
	for _, c := range []string{model.CapDiscover, model.CapDiscoverByValue, model.CapNodePoints, model.CapCheckMapping, model.CapCancel} {
		if !strings.Contains(caps, ","+c+",") {
			t.Errorf("capability %s missing: %v", c, hello.Capabilities)
		}
	}
	c := &rawConn{t: t, rw: rw, pending: map[uint64]model.IPCMessage{}}
	for _, r := range interactiveReqs(1) {
		m := c.call(r)
		if m.Type != model.MsgError || m.Code != model.ErrCodeAccessDenied || m.ErrorKey != model.MsgMonitorAccessDenied {
			t.Errorf("%s as non-admin: %+v", r.Cmd, m)
		}
	}
	if len(h.Calls()) != 0 || !lg.has("discover refused") {
		t.Fatalf("calls %v, refusal logged %v", h.Calls(), lg.has("discover refused"))
	}
	atomic.StoreInt32(&admin, 1)
	want := []string{model.MsgDiscovery, model.MsgDiscovery, model.MsgNodePoints, model.MsgMappingCheck}
	for i, r := range interactiveReqs(10) {
		m := c.call(r)
		if m.Type != want[i] {
			t.Fatalf("%s: %+v", r.Cmd, m)
		}
		switch m.Type {
		case model.MsgDiscovery:
			byValue := r.Cmd == model.CmdDiscoverByValue
			if len(m.Candidates) != 1 || m.Truncated != byValue || (m.TruncatedReason == model.TruncatedBudget) != byValue {
				t.Errorf("%s: %+v", r.Cmd, m)
			}
		case model.MsgNodePoints:
			if len(m.NodePoints) != 1 || m.Truncated || m.TruncatedReason != "" {
				t.Errorf("node points: %+v", m)
			}
		case model.MsgMappingCheck:
			if m.MappingCheck == nil || !m.MappingCheck.OK || len(m.MappingCheck.SampleRows) != 1 {
				t.Errorf("mapping check: %+v", m.MappingCheck)
			}
		}
	}
	// handler errors keep their code / key
	h.mu.Lock()
	h.err = &model.IPCError{Code: model.ErrCodeBusy, Key: model.MsgDiscoverBusy, Message: "busy"}
	h.mu.Unlock()
	if m := c.call(model.IPCRequest{ID: 20, Cmd: model.CmdDiscover, ProfileID: "p1"}); m.Code != model.ErrCodeBusy || m.ErrorKey != model.MsgDiscoverBusy {
		t.Fatalf("busy: %+v", m)
	}

	// a handler without Discoverer: no capabilities, unknown_command
	_, _, l2, _ := newV2(t)
	rw2, _ := l2.Dial()
	defer rw2.Close()
	var h2 model.IPCMessage
	if err := ReadMessage(rw2, &h2); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(h2.Capabilities, ","), "discover") || strings.Contains(strings.Join(h2.Capabilities, ","), model.CapCheckMapping) {
		t.Fatalf("capabilities without Discoverer: %v", h2.Capabilities)
	}
	c2 := &rawConn{t: t, rw: rw2, pending: map[uint64]model.IPCMessage{}}
	for _, r := range interactiveReqs(1) {
		if m := c2.call(r); m.Code != model.ErrCodeUnknownCommand || m.ErrorKey != model.MsgMonitorUnknownCommand {
			t.Errorf("%s without Discoverer: %+v", r.Cmd, m)
		}
	}
}

// TestCHK_IPC_08_ServerAsyncCancel — a running discover does not block
// ping of the same client; cancel {ref} answers ok canceled and the
// discover error canceled + monitor.canceled (its ctx ended); a
// disconnect cancels a running check_mapping.
func TestCHK_IPC_08_ServerAsyncCancel(t *testing.T) {
	admin := int32(1)
	h, l, _ := newDiscServer(t, &admin)
	h.block, h.ctxDone, h.started = make(chan struct{}), make(chan struct{}), make(chan struct{}, 4)
	c := dialRaw(t, l)
	c.send(model.IPCRequest{ID: 1, Cmd: model.CmdDiscover, ProfileID: "p1"})
	<-h.started
	if m := c.call(model.IPCRequest{ID: 2, Cmd: model.CmdPing}); m.Type != model.MsgPong {
		t.Fatalf("ping during discover: %+v", m)
	}
	if m := c.call(model.IPCRequest{ID: 3, Cmd: model.CmdCancel, Ref: 1}); m.Type != model.MsgOK || m.Result != model.ResultCanceled {
		t.Fatalf("cancel: %+v", m)
	}
	if m := c.reply(1); m.Type != model.MsgError || m.Code != model.ErrCodeCanceled || m.ErrorKey != model.MsgMonitorCanceled {
		t.Fatalf("canceled discover: %+v", m)
	}
	select {
	case <-h.ctxDone:
	case <-time.After(5 * time.Second):
		t.Fatal("discover ctx not canceled")
	}
	// disconnect
	h.mu.Lock()
	h.ctxDone = make(chan struct{})
	done := h.ctxDone
	h.mu.Unlock()
	c2 := dialRaw(t, l)
	mp := model.Mapping{Table: "_Node1"}
	c2.send(model.IPCRequest{ID: 1, Cmd: model.CmdCheckMapping, ProfileID: "p1", Mapping: &mp})
	<-h.started
	c2.rw.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("disconnect did not cancel check_mapping")
	}
}

// TestCHK_IPC_08_ReplySizeLimit — discovery / node_points /
// mapping_check replies stay within 64 KiB of JSON (Вика 06.10): the tail
// is dropped (the longest prefix that fits), truncated /
// mappingCheck.truncated is set; a small reply is unchanged.
func TestCHK_IPC_08_ReplySizeLimit(t *testing.T) {
	big := strings.Repeat("я", model.MaxSampleCellRunes)
	var rows []model.SampleRow
	for i := 0; i < model.MaxSampleRows; i++ { // the PreviewMapping worst case (~50 KiB of names + times)
		at := time.Date(2026, 10, 6, 9, 0, i, 0, model.DBZone)
		rows = append(rows, model.SampleRow{Point: big, LastSend: &at, LastRecv: &at, PointTruncated: true})
	}
	rows4 := make([]model.SampleRow, len(rows)) // 4-byte runes: ~100 KiB
	for i := range rows {
		rows4[i] = rows[i]
		rows4[i].Point = strings.Repeat("\U0001F600", model.MaxSampleCellRunes)
	}
	var cands []model.DiscoveryCandidate
	for i := 0; i < 60; i++ {
		cands = append(cands, model.DiscoveryCandidate{Table: "_Node1", PointCol: "_Description", SampleRows: rows[:5], Hints: []string{model.HintSampled}})
	}
	var pts []model.NodePoint
	for i := 0; i < 5000; i++ {
		pts = append(pts, model.NodePoint{PlanTable: "_Node1", PlanNo: 1, Ref: "0123456789abcdef0123456789abcdef", Name: "Точка обмена номер"})
	}
	size := func(m model.IPCMessage) int { b, _ := json.Marshal(m); return len(b) }
	for _, c := range []struct {
		name string
		m    model.IPCMessage
		n    func(model.IPCMessage) int
		tr   func(model.IPCMessage) bool
	}{
		{"candidates", model.IPCMessage{ID: 1, Type: model.MsgDiscovery, Candidates: cands},
			func(m model.IPCMessage) int { return len(m.Candidates) }, func(m model.IPCMessage) bool { return m.Truncated }},
		{"node points", model.IPCMessage{ID: 1, Type: model.MsgNodePoints, NodePoints: pts},
			func(m model.IPCMessage) int { return len(m.NodePoints) }, func(m model.IPCMessage) bool { return m.Truncated }},
		{"sample rows", model.IPCMessage{ID: 1, Type: model.MsgMappingCheck, MappingCheck: &model.MappingCheck{OK: true, SampleRows: rows4}},
			func(m model.IPCMessage) int { return len(m.MappingCheck.SampleRows) }, func(m model.IPCMessage) bool { return m.MappingCheck.Truncated }},
	} {
		before := c.n(c.m)
		if size(c.m) <= model.IPCMaxInteractiveReply {
			t.Fatalf("%s: test data too small (%d)", c.name, size(c.m))
		}
		m := c.m
		fitInteractiveReply(&m, model.IPCMaxInteractiveReply)
		if s := size(m); s > model.IPCMaxInteractiveReply || !c.tr(m) || c.n(m) == 0 || c.n(m) >= before {
			t.Errorf("%s: size %d, %d of %d items, truncated %v", c.name, s, c.n(m), before, c.tr(m))
		}
		// the longest prefix: one more item would not fit
		more := c.m
		switch {
		case more.Candidates != nil:
			more.Candidates, more.Truncated, more.TruncatedReason = cands[:len(m.Candidates)+1], true, model.TruncatedSize
		case more.NodePoints != nil:
			more.NodePoints, more.Truncated, more.TruncatedReason = pts[:len(m.NodePoints)+1], true, model.TruncatedSize
		default:
			mc := *more.MappingCheck
			mc.SampleRows, mc.Truncated, mc.TruncatedReason = rows4[:len(m.MappingCheck.SampleRows)+1], true, model.TruncatedSize
			more.MappingCheck = &mc
		}
		if size(more) <= model.IPCMaxInteractiveReply {
			t.Errorf("%s: not the longest prefix", c.name)
		}
	}
	small := model.IPCMessage{ID: 1, Type: model.MsgDiscovery, Candidates: cands[:2]}
	fitInteractiveReply(&small, model.IPCMaxInteractiveReply)
	if small.Truncated || len(small.Candidates) != 2 {
		t.Fatalf("small reply changed: %+v", small)
	}

	// through the server
	admin := int32(1)
	h, l, _ := newDiscServer(t, &admin)
	h.check = model.MappingCheck{OK: true, SampleRows: rows4}
	h.pts = pts
	c := dialRaw(t, l)
	mp := model.Mapping{Table: "_Node1"}
	for _, r := range []model.IPCRequest{{ID: 1, Cmd: model.CmdCheckMapping, Mapping: &mp, Limit: 20}, {ID: 2, Cmd: model.CmdListNodePoints}} {
		if err := WriteRequest(c.rw, r); err != nil {
			t.Fatal(err)
		}
		var raw json.RawMessage
		for {
			if err := ReadMessage(c.rw, &raw); err != nil {
				t.Fatal(err)
			}
			var m model.IPCMessage
			json.Unmarshal(raw, &m)
			if m.ID == r.ID {
				if len(raw) > model.IPCMaxInteractiveReply || (m.MappingCheck != nil && !m.MappingCheck.Truncated) || (m.MappingCheck == nil && !m.Truncated) {
					t.Fatalf("%s over the pipe: %d bytes", r.Cmd, len(raw))
				}
				break
			}
		}
	}
}

// TestCHK_IPC_05_InteractiveFieldsOmitEmpty — compatibility (CHK-IPC-05):
// the new request / reply fields are omitempty, so version-1/2 messages
// without them serialize as before; a version-1 client decodes a
// discovery reply (unknown fields ignored).
func TestCHK_IPC_05_InteractiveFieldsOmitEmpty(t *testing.T) {
	b, _ := json.Marshal(model.IPCMessage{ID: 1, Type: model.MsgPong, Version: 2})
	for _, f := range []string{"candidates", "truncated", "truncatedReason", "nodePoints", "mappingCheck"} {
		if strings.Contains(string(b), f) {
			t.Errorf("pong carries %s: %s", f, b)
		}
	}
	rb, _ := json.Marshal(model.IPCRequest{ID: 1, Cmd: model.CmdPing})
	for _, f := range []string{"query", "mapping", "limit"} {
		if strings.Contains(string(rb), f) {
			t.Errorf("ping request carries %s: %s", f, rb)
		}
	}
	d, _ := json.Marshal(model.IPCMessage{ID: 3, Type: model.MsgDiscovery, Candidates: []model.DiscoveryCandidate{{Table: "_Node1"}}, Truncated: true})
	var v1 v1Message
	if err := json.Unmarshal(d, &v1); err != nil || v1.ID != 3 || v1.Type != model.MsgDiscovery {
		t.Fatalf("v1 decode: %+v %v", v1, err)
	}
	// String / GoString do not print the query or the manual SQL
	mp := model.Mapping{ManualSQL: "SELECT secret_column, b, c FROM t"}
	s := model.IPCRequest{Cmd: model.CmdCheckMapping, Mapping: &mp, Query: &model.ValueQuery{PointName: "Склад-7"}, Password: "pw!"}.String()
	if strings.Contains(s, "secret_column") || strings.Contains(s, "Склад-7") || strings.Contains(s, "pw!") || !strings.Contains(s, "mapping:true") {
		t.Fatalf("String: %s", s)
	}
}

// TestCHK_IPC_08_TruncatedReason — truncatedReason (Вика 06.10) is
// present if and only if truncated: "budget" (discovery budget ran out),
// "size" (cut to 64 KiB), "limit" (list_node_points limit); budget wins
// over size, size over limit; no cut — neither field in the JSON
// (discovery, node_points, mappingCheck; CHK-IPC-05).
func TestCHK_IPC_08_TruncatedReason(t *testing.T) {
	big := strings.Repeat("\U0001F600", model.MaxSampleCellRunes)
	var cands []model.DiscoveryCandidate
	var rows []model.SampleRow
	for i := 0; i < model.MaxSampleRows; i++ {
		rows = append(rows, model.SampleRow{Point: big})
	}
	for i := 0; i < 40; i++ {
		cands = append(cands, model.DiscoveryCandidate{Table: "_Node1", SampleRows: rows[:5]})
	}
	var pts []model.NodePoint
	for i := 0; i < 3000; i++ {
		pts = append(pts, model.NodePoint{PlanTable: "_Node1", Ref: "0123456789abcdef0123456789abcdef", Name: "Точка обмена"})
	}
	// as the server builds the reply: Truncated = reason != "", then fit
	fit := func(m model.IPCMessage) model.IPCMessage {
		m.Truncated = m.TruncatedReason != ""
		fitInteractiveReply(&m, model.IPCMaxInteractiveReply)
		return m
	}
	for _, c := range []struct {
		name   string
		m      model.IPCMessage
		reason string
	}{
		{"discovery budget", model.IPCMessage{Type: model.MsgDiscovery, Candidates: cands[:2], TruncatedReason: model.TruncatedBudget}, model.TruncatedBudget},
		{"discovery size", model.IPCMessage{Type: model.MsgDiscovery, Candidates: cands}, model.TruncatedSize},
		{"discovery budget + size", model.IPCMessage{Type: model.MsgDiscovery, Candidates: cands, TruncatedReason: model.TruncatedBudget}, model.TruncatedBudget},
		{"discovery complete", model.IPCMessage{Type: model.MsgDiscovery, Candidates: cands[:2]}, ""},
		{"node points limit", model.IPCMessage{Type: model.MsgNodePoints, NodePoints: pts[:10], TruncatedReason: model.TruncatedLimit}, model.TruncatedLimit},
		{"node points size", model.IPCMessage{Type: model.MsgNodePoints, NodePoints: pts}, model.TruncatedSize},
		{"node points limit + size", model.IPCMessage{Type: model.MsgNodePoints, NodePoints: pts, TruncatedReason: model.TruncatedLimit}, model.TruncatedSize},
		{"node points complete", model.IPCMessage{Type: model.MsgNodePoints, NodePoints: pts[:10]}, ""},
	} {
		m := fit(c.m)
		b, _ := json.Marshal(m)
		if m.TruncatedReason != c.reason || m.Truncated != (c.reason != "") || len(b) > model.IPCMaxInteractiveReply {
			t.Errorf("%s: truncated %v reason %q (%d bytes)", c.name, m.Truncated, m.TruncatedReason, len(b))
		}
		if c.reason == "" && (strings.Contains(string(b), `"truncated"`) || strings.Contains(string(b), "truncatedReason")) {
			t.Errorf("%s: %s", c.name, b)
		}
	}
	mc := fit(model.IPCMessage{Type: model.MsgMappingCheck, MappingCheck: &model.MappingCheck{OK: true, SampleRows: rows}})
	if !mc.MappingCheck.Truncated || mc.MappingCheck.TruncatedReason != model.TruncatedSize || mc.Truncated || mc.TruncatedReason != "" {
		t.Errorf("mapping check size: %+v / %v %q", mc.MappingCheck.Truncated, mc.Truncated, mc.MappingCheck.TruncatedReason)
	}
	small := fit(model.IPCMessage{Type: model.MsgMappingCheck, MappingCheck: &model.MappingCheck{OK: true, SampleRows: rows[:3]}})
	if b, _ := json.Marshal(small); strings.Contains(string(b), "truncated") {
		t.Errorf("mapping check complete: %s", b)
	}

	// over the pipe: the handler's reason, Truncated set by the server
	admin := int32(1)
	h, l, _ := newDiscServer(t, &admin)
	h.cands, h.reasonD, h.pts, h.reasonN = cands[:1], model.TruncatedBudget, pts, model.TruncatedLimit
	c := dialRaw(t, l)
	if m := c.call(model.IPCRequest{ID: 1, Cmd: model.CmdDiscover}); !m.Truncated || m.TruncatedReason != model.TruncatedBudget {
		t.Fatalf("discover budget: %v %q", m.Truncated, m.TruncatedReason)
	}
	if m := c.call(model.IPCRequest{ID: 2, Cmd: model.CmdListNodePoints, Limit: 3000}); !m.Truncated || m.TruncatedReason != model.TruncatedSize {
		t.Fatalf("node points limit + size: %v %q", m.Truncated, m.TruncatedReason)
	}
	h.mu.Lock()
	h.reasonD = ""
	h.mu.Unlock()
	if m := c.call(model.IPCRequest{ID: 3, Cmd: model.CmdDiscover}); m.Truncated || m.TruncatedReason != "" {
		t.Fatalf("discover complete: %v %q", m.Truncated, m.TruncatedReason)
	}
}
