package ipc

import (
	"encoding/json"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

type xferHandler struct {
	*v2Handler
	mu  sync.Mutex
	got []string
}

func (h *xferHandler) ExportConfig() (model.ConfigExport, error) {
	return model.ConfigExport{Format: model.ConfigFormatV1, Profiles: []model.Profile{{ID: "p1", HasPassword: true}}}, nil
}

func (h *xferHandler) ImportConfig(cfg *model.ConfigExport, mode, rev string, dry bool, account string) (model.ImportResult, error) {
	h.mu.Lock()
	h.got = append(h.got, mode+"/"+rev+"/"+account)
	h.mu.Unlock()
	return model.ImportResult{DryRun: dry, Mode: mode, Revision: "r2", Added: []string{"p9"}, Updated: []string{}, Removed: []string{},
		NeedPassword: []string{}}, nil
}

func newXferServer(t *testing.T, admin *int32) (*xferHandler, *MemListener) {
	t.Helper()
	v2, _, _, _ := newV2(t)
	h := &xferHandler{v2Handler: v2}
	s := NewServer(h, &testLog{})
	s.Authorize = func(io.ReadWriteCloser) (Peer, error) {
		return Peer{Admin: atomic.LoadInt32(admin) == 1, Account: `HOST\alex`}, nil
	}
	l := NewMemListener()
	go s.Serve(l)
	t.Cleanup(func() { s.Close() })
	return h, l
}

// closed reports whether the server dropped the connection.
func closed(t *testing.T, rw io.ReadWriteCloser) bool {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		for {
			var m model.IPCMessage
			if err := ReadMessage(rw, &m); err != nil {
				done <- err
				return
			}
			if m.ID != 0 {
				done <- nil
				return
			}
		}
	}()
	select {
	case err := <-done:
		return err != nil
	case <-time.After(5 * time.Second):
		t.Fatal("no reply and no close")
	}
	return false
}

// TestCHK_CFG_06_IPCLimitsAndShapes — export_config / import_config are
// announced, admin only; an import_config request above 64 KiB (up to
// 1 MiB) is accepted only from an elevated administrator; any other
// command above 64 KiB, or a request above 1 MiB, drops the client.
func TestCHK_CFG_06_IPCLimitsAndShapes(t *testing.T) {
	var admin int32 = 1
	h, l := newXferServer(t, &admin)
	big := json.RawMessage(`{"pad":"` + strings.Repeat("x", 200<<10) + `"}`)
	imp := model.IPCRequest{ID: 7, Cmd: model.CmdImportConfig, Mode: model.ImportReplace, ExpectedRevision: "r1", DryRun: true,
		Config: &model.ConfigExport{Format: model.ConfigFormatV1, Profiles: []model.Profile{}, Tray: big}}

	c := dialRaw(t, l)
	c.send(model.IPCRequest{ID: 1, Cmd: model.CmdExportConfig})
	if r := c.reply(1); r.Type != model.MsgConfigExport || r.Export == nil || r.Export.Format != "ec-config/1" {
		t.Fatalf("export %+v", r)
	}
	c.send(imp)
	r := c.reply(7)
	if r.Type != model.MsgOK || r.Result != model.ResultImportChecked || r.Revision != "r2" || r.Import == nil ||
		strings.Join(r.Import.Added, ",") != "p9" || r.Reloaded {
		t.Fatalf("import %+v", r)
	}
	b, _ := json.Marshal(r)
	for _, k := range []string{`"dryRun":true`, `"needPassword":[]`, `"removed":[]`, `"updated":[]`, `"mode":"replace"`} {
		if !strings.Contains(string(b), k) {
			t.Fatalf("reply json %s lacks %s", b, k)
		}
	}
	if len(h.got) != 1 || h.got[0] != `replace/r1/HOST\alex` {
		t.Fatalf("handler %v", h.got)
	}

	// admin, another command above 64 KiB: dropped
	rw, _ := l.Dial()
	readHello(t, rw)
	go func() {
		_ = WriteFrame(rw, model.IPCRequest{ID: 2, Cmd: model.CmdPing, ProfileID: strings.Repeat("x", 100<<10)}, 2<<20)
	}()
	if !closed(t, rw) {
		t.Fatal("large ping accepted")
	}
	// admin, import_config above 1 MiB: dropped
	rw2, _ := l.Dial()
	readHello(t, rw2)
	huge := imp
	huge.Config = &model.ConfigExport{Format: model.ConfigFormatV1, Tray: json.RawMessage(`"` + strings.Repeat("x", 1<<20) + `"`)}
	go func() { _ = WriteFrame(rw2, huge, 4<<20) }() // the server may drop before the body
	if !closed(t, rw2) {
		t.Fatal("import above 1 MiB accepted")
	}
	// not an administrator: the large import is dropped, a small one denied
	atomic.StoreInt32(&admin, 0)
	rw3, _ := l.Dial()
	readHello(t, rw3)
	go func() { _ = WriteRequest(rw3, imp) }()
	if !closed(t, rw3) {
		t.Fatal("large import from a non-admin accepted")
	}
	c4 := dialRaw(t, l)
	c4.send(model.IPCRequest{ID: 9, Cmd: model.CmdImportConfig, Config: &model.ConfigExport{Format: model.ConfigFormatV1}})
	if r := c4.reply(9); r.Type != model.MsgError || r.Code != model.ErrCodeAccessDenied {
		t.Fatalf("non-admin import %+v", r)
	}
	// the client side refuses import_config above 1 MiB
	if err := WriteRequest(io.Discard, huge); err != ErrTooLarge {
		t.Fatalf("client write %v", err)
	}
}

type xferViberHandler struct {
	*xferHandler
	include bool
	impInc  bool
}

func (h *xferViberHandler) ExportConfigOpts(include bool) (model.ConfigExport, error) {
	h.include = include
	exp, err := h.ExportConfig()
	if include {
		exp.ContainsViberRecipients = true
		st := model.DefaultSettings()
		st.Viber.Recipients = []model.ViberRecipient{{ID: "r1", Name: "A"}}
		v := st.Viber.Stored()
		st.Viber = &v
		exp.Settings = &st
	}
	return exp, err
}

func (h *xferViberHandler) ImportConfigOpts(cfg *model.ConfigExport, mode, rev string, dry, include bool, account string) (model.ImportResult, error) {
	h.impInc = include
	return h.ImportConfig(cfg, mode, rev, dry, account)
}

func newXferViberServer(t *testing.T, admin *int32) (*xferViberHandler, *MemListener) {
	t.Helper()
	v2, _, _, _ := newV2(t)
	h := &xferViberHandler{xferHandler: &xferHandler{v2Handler: v2}}
	s := NewServer(h, &testLog{})
	s.Authorize = func(io.ReadWriteCloser) (Peer, error) {
		return Peer{Admin: atomic.LoadInt32(admin) == 1, Account: `HOST\alex`}, nil
	}
	l := NewMemListener()
	go s.Serve(l)
	t.Cleanup(func() { s.Close() })
	return h, l
}

// TestCHK_IPC_14_IncludeViberRecipients — hello advertises
// export_viber_recipients; export_config / import_config forward
// includeViberRecipients; export JSON carries containsViberRecipients and
// recipients without hasToken/status/token.
func TestCHK_IPC_14_IncludeViberRecipients(t *testing.T) {
	var admin int32 = 1
	h, l := newXferViberServer(t, &admin)
	rw, err := l.Dial()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rw.Close() })
	var hello model.IPCMessage
	if err := ReadMessage(rw, &hello); err != nil {
		t.Fatal(err)
	}
	caps := "," + strings.Join(hello.Capabilities, ",") + ","
	if !strings.Contains(caps, ","+model.CapExportViberRecipients+",") || !strings.Contains(caps, ","+model.CapExportConfig+",") {
		t.Fatalf("caps %v", hello.Capabilities)
	}
	c := &rawConn{t: t, rw: rw, pending: map[uint64]model.IPCMessage{}}
	c.send(model.IPCRequest{ID: 1, Cmd: model.CmdExportConfig, IncludeViberRecipients: true})
	m := c.reply(1)
	if m.Type != model.MsgConfigExport || m.Export == nil || !h.include || !m.Export.ContainsViberRecipients ||
		m.Export.Settings == nil || m.Export.Settings.Viber == nil || len(m.Export.Settings.Viber.Recipients) != 1 {
		t.Fatalf("export %+v include=%v", m, h.include)
	}
	b, _ := json.Marshal(m.Export)
	if strings.Contains(string(b), `"hasToken"`) || strings.Contains(string(b), `"status"`) ||
		!strings.Contains(string(b), `"containsViberRecipients":true`) ||
		!strings.Contains(string(b), `"recipients":[{"id":"r1","name":"A"}]`) {
		t.Fatalf("export json %s", b)
	}

	c.send(model.IPCRequest{ID: 2, Cmd: model.CmdImportConfig, IncludeViberRecipients: true,
		Mode: model.ImportMerge, ExpectedRevision: "r1", Config: &model.ConfigExport{Format: model.ConfigFormatV1}})
	m = c.reply(2)
	if m.Type != model.MsgOK || !h.impInc {
		t.Fatalf("import %+v impInc=%v", m, h.impInc)
	}
}
