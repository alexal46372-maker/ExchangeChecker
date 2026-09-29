package sqlcheck

import (
	"bytes"
	"context"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// CHK-SQL-05 (integration): what go-mssqldb v1.8.2 really puts on the
// wire for each encrypt mode, observed through an in-process TCP proxy
// that records the client -> server bytes:
//
//	encrypt "disable": no TLS at all; the Login7 packet (login, app name,
//	  password only "mangled": nibble swap + XOR 0xA5) and every query are
//	  readable;
//	encrypt "" (driver default): TLS for the Login7 packet only, queries
//	  are readable;
//	encrypt "on": everything is encrypted.

type wireProxy struct {
	ln   net.Listener
	mu   sync.Mutex
	sent bytes.Buffer // client -> server
	wg   sync.WaitGroup
}

func newWireProxy(t *testing.T, target string) *wireProxy {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &wireProxy{ln: ln}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			s, err := net.Dial("tcp", target)
			if err != nil {
				c.Close()
				continue
			}
			p.wg.Add(2)
			go func() { defer p.wg.Done(); io.Copy(s, io.TeeReader(c, p)); s.Close() }()
			go func() { defer p.wg.Done(); io.Copy(c, s); c.Close() }()
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return p
}

func (p *wireProxy) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sent.Write(b)
}

func (p *wireProxy) bytes() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]byte(nil), p.sent.Bytes()...)
}

func ucs2(s string) []byte {
	var b []byte
	for _, r := range s {
		b = append(b, byte(r), byte(r>>8))
	}
	return b
}

// mangled is the TDS Login7 password obfuscation (go-mssqldb manglePassword).
func mangled(pw string) []byte {
	b := ucs2(pw)
	for i, c := range b {
		b[i] = ((c<<4)&0xff | c>>4) ^ 0xA5
	}
	return b
}

func TestCHK_SQL_05_IntegrationEncryptWire(t *testing.T) {
	p, pw := itProfile(t, "ec_1c_plans_off")
	target := net.JoinHostPort(p.Server, strconv.Itoa(p.Port))
	const marker = "ec_wire_marker_42"
	for _, c := range []struct {
		mode       model.EncryptMode
		loginClear bool
		queryClear bool
	}{
		{model.EncryptDisable, true, true},
		{model.EncryptDefault, true, true}, // unset = disable (phase 2d)
		{model.EncryptOptional, false, true},
		{model.EncryptOn, false, false},
	} {
		px := newWireProxy(t, target)
		q := p
		q.Server, q.Port = "127.0.0.1", px.ln.Addr().(*net.TCPAddr).Port
		q.Encrypt, q.TrustServerCert = c.mode, true
		q.ServerPreset = model.ServerPresetMSSQL2008
		st, err := Open(q, pw)
		if err != nil {
			t.Fatal(err)
		}
		var got string
		err = st.DB().QueryRowContext(context.Background(), "SELECT N'"+marker+"'").Scan(&got)
		st.Close()
		px.ln.Close()
		px.wg.Wait()
		if err != nil || got != marker {
			t.Fatalf("encrypt %q: %v", c.mode, err)
		}
		wire := px.bytes()
		loginClear := bytes.Contains(wire, ucs2(AppName)) && bytes.Contains(wire, ucs2(q.Login))
		queryClear := bytes.Contains(wire, ucs2(marker))
		if loginClear != c.loginClear || queryClear != c.queryClear {
			t.Errorf("encrypt %q: login readable %v (want %v), query readable %v (want %v)",
				c.mode, loginClear, c.loginClear, queryClear, c.queryClear)
		}
		if bytes.Contains(wire, ucs2(pw)) {
			t.Errorf("encrypt %q: password in plain UCS-2 on the wire", c.mode)
		}
		if pwMangled := bytes.Contains(wire, mangled(pw)); pwMangled != c.loginClear {
			t.Errorf("encrypt %q: mangled password visible = %v", c.mode, pwMangled)
		}
		t.Logf("encrypt %q: %d bytes client->server, login readable %v, query readable %v", c.mode, len(wire), loginClear, queryClear)
	}
}
