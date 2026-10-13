package sqlcheck

import (
	"context"
	"encoding/binary"
	"net"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

func svrResp(data string) []byte {
	b := []byte{ssrpSvrResp, 0, 0}
	binary.LittleEndian.PutUint16(b[1:], uint16(len(data)))
	return append(b, data...)
}

// fakeBrowser is a UDP responder on loopback answering every request with
// replies; got records the requests.
type fakeBrowser struct {
	pc  net.PacketConn
	mu  sync.Mutex
	got [][]byte
}

func newFakeBrowser(t *testing.T, replies ...[]byte) *fakeBrowser {
	t.Helper()
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeBrowser{pc: pc}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			f.mu.Lock()
			f.got = append(f.got, append([]byte(nil), buf[:n]...))
			f.mu.Unlock()
			for _, r := range replies {
				pc.WriteTo(r, from)
			}
		}
	}()
	return f
}

func (f *fakeBrowser) addr() string { return f.pc.LocalAddr().String() }

func (f *fakeBrowser) requests() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]byte(nil), f.got...)
}

const srvA = "ServerName;SRV-A;InstanceName;MSSQLSERVER;IsClustered;No;Version;10.50.6000.34;tcp;1433;np;\\\\SRV-A\\pipe\\sql\\query;;" +
	"ServerName;SRV-A;InstanceName;SQL2008;IsClustered;Yes;Version;10.0.6000.29;;"

// CHK-SQL-06: broadcast discovery against fake responders, dedupe, sort.
func TestCHK_SQL_06_BrowseBroadcast(t *testing.T) {
	a := newFakeBrowser(t, svrResp(srvA), svrResp(srvA)) // duplicate answer
	b := newFakeBrowser(t, svrResp("ServerName;srv-b;InstanceName;EC\x01X;Version;9.00.5000.00;tcp;99999;;"), []byte("garbage"))
	start := time.Now()
	res, err := Browse(context.Background(), model.SQLBrowseRequest{TimeoutMs: 400}, BrowseOptions{Targets: []string{a.addr(), b.addr()}})
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d < 350*time.Millisecond || d > 3*time.Second {
		t.Errorf("broadcast must collect until the timeout: %v", d)
	}
	if len(res.Instances) != 3 || res.Responders != 1 || res.Truncated {
		// Both fakes answer from 127.0.0.1: one responder address.
		t.Fatalf("%+v", res)
	}
	i0, i1, i2 := res.Instances[0], res.Instances[1], res.Instances[2]
	if i0.Host != "SRV-A" || i0.Instance != "MSSQLSERVER" || i0.TCPPort != 1433 || i0.Version != "10.50.6000.34" || i0.IsClustered || i0.NamedPipe == "" || i0.Address != "127.0.0.1" || i0.Server() != "SRV-A" {
		t.Errorf("%+v", i0)
	}
	if i1.Instance != "SQL2008" || i1.TCPPort != 0 || !i1.IsClustered || i1.Server() != `SRV-A\SQL2008` {
		t.Errorf("%+v", i1)
	}
	if i2.Host != "srv-b" || i2.Instance != "ECX" || i2.TCPPort != 0 {
		t.Errorf("sanitize / port range: %+v", i2)
	}
	for _, r := range append(a.requests(), b.requests()...) {
		if len(r) != 1 || r[0] != ssrpClntBcastEx {
			t.Errorf("request %x", r)
		}
	}
	// MaxInstances cap.
	res, err = Browse(context.Background(), model.SQLBrowseRequest{TimeoutMs: 200}, BrowseOptions{Targets: []string{a.addr()}, MaxInstances: 1})
	if err != nil || len(res.Instances) != 1 || !res.Truncated {
		t.Errorf("cap: %+v %v", res, err)
	}
}

// CHK-SQL-06: unicast requests (CLNT_UCAST_EX / CLNT_UCAST_INST).
func TestCHK_SQL_06_BrowseUnicast(t *testing.T) {
	a := newFakeBrowser(t, svrResp(srvA))
	res, err := Browse(context.Background(), model.SQLBrowseRequest{Host: "127.0.0.1", Instance: "SQL2008", TimeoutMs: 1000}, BrowseOptions{Targets: []string{a.addr()}})
	if err != nil || len(res.Instances) != 2 {
		t.Fatalf("%+v %v", res, err)
	}
	if r := a.requests(); len(r) != 1 || string(r[0]) != "\x04SQL2008\x00" {
		t.Errorf("%q", r)
	}
	if _, err := Browse(context.Background(), model.SQLBrowseRequest{Host: "127.0.0.1", TimeoutMs: 300}, BrowseOptions{Targets: []string{a.addr()}}); err != nil {
		t.Fatal(err)
	}
	if r := a.requests(); len(r) != 2 || string(r[1]) != "\x03" {
		t.Errorf("%q", r)
	}
	// Real unicast path (host resolution, port option) against loopback.
	port := a.pc.LocalAddr().(*net.UDPAddr).Port
	start := time.Now()
	res, err = Browse(context.Background(), model.SQLBrowseRequest{Host: "127.0.0.1", TimeoutMs: 5000}, BrowseOptions{Port: port})
	if err != nil || len(res.Instances) != 2 || time.Since(start) > 3*time.Second {
		t.Errorf("unicast must stop once the host answered: %+v %v %v", res, err, time.Since(start))
	}
}

// CHK-SQL-06: silence, timeout, ctx cancel, invalid request.
func TestCHK_SQL_06_BrowseTimeoutCancel(t *testing.T) {
	base := runtime.NumGoroutine()
	silent := newFakeBrowser(t) // reads, never answers
	res, err := Browse(context.Background(), model.SQLBrowseRequest{TimeoutMs: 200}, BrowseOptions{Targets: []string{silent.addr()}})
	if err != nil || len(res.Instances) != 0 || res.Instances == nil || res.Responders != 0 {
		t.Errorf("silence: %+v %v", res, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	_, err = Browse(ctx, model.SQLBrowseRequest{TimeoutMs: 10000}, BrowseOptions{Targets: []string{silent.addr()}})
	if KindOf(err) != KindCanceled || time.Since(start) > 2*time.Second {
		t.Errorf("cancel: %v after %v", err, time.Since(start))
	}
	if _, err := Browse(ctx, model.SQLBrowseRequest{}, BrowseOptions{Targets: []string{silent.addr()}}); KindOf(err) != KindCanceled {
		t.Errorf("canceled before start: %v", err)
	}
	for _, r := range []model.SQLBrowseRequest{{Host: `\\evil\pipe\x`}, {Host: "np:evil"}, {Instance: "X"}, {TimeoutMs: 10}} {
		if _, err := Browse(context.Background(), r, BrowseOptions{}); KindOf(err) != KindConfig {
			t.Errorf("%+v: %v", r, err)
		}
	}
	settleGoroutines(t, base+2) // the silent responder's reader
}

// CHK-SQL-06: SVR_RESP parser.
func TestCHK_SQL_06_ParseSSRP(t *testing.T) {
	bad := [][]byte{nil, {0x05}, {0x05, 0, 0}, {0x04, 1, 0, 'x'}, svrResp(srvA)[:20]}
	for _, b := range bad {
		if _, ok := parseSSRP(b); ok {
			t.Errorf("%q accepted", b)
		}
	}
	// Size field larger than the datagram (truncated) / extra bytes ignored.
	full := svrResp("ServerName;S;InstanceName;I;;")
	if in, ok := parseSSRP(append(full, "junk"...)); !ok || len(in) != 1 {
		t.Error("trailing bytes")
	}
	cases := map[string]int{
		"ServerName;S;InstanceName;I;;ServerName;S2;InstanceName;I2":    2, // last record without ";;"
		"InstanceName;I;tcp;1433;;":                                     0, // no server name
		"ServerName;S;;":                                                0, // no instance
		"servername;S;INSTANCENAME;I;TCP;abc;;":                         1,
		"ServerName;S;InstanceName;I;odd":                               1,
		";;;;;;":                                                        0,
		"ServerName;" + strings.Repeat("x", 1000) + ";InstanceName;I;;": 1,
	}
	for data, n := range cases {
		in, ok := parseSSRP(svrResp(data))
		if !ok || len(in) != n {
			t.Errorf("%.40q: %d %v", data, len(in), ok)
		}
		for _, x := range in {
			if len(x.Host) > ssrpMaxValue || x.TCPPort != 0 && x.TCPPort != 1433 {
				t.Errorf("%+v", x)
			}
		}
	}
	if v := ssrpValue("a\x00b\xffc\u0085" + strings.Repeat("я", 200)); len(v) > ssrpMaxValue || strings.ContainsAny(v, "\x00\u0085") || !strings.HasPrefix(v, "abc") {
		t.Errorf("%q", v)
	}
}

// CHK-SQL-06: directed broadcast addresses.
func TestCHK_SQL_06_BroadcastAddr(t *testing.T) {
	cases := map[string]string{
		"192.168.1.10/24": "192.168.1.255", "10.1.2.3/8": "10.255.255.255", "172.16.5.4/20": "172.16.15.255",
		"127.0.0.1/8": "", "169.254.1.1/16": "", "10.0.0.1/32": "", "10.0.0.1/31": "", "fe80::1/64": "", "2001:db8::1/64": "",
	}
	for cidr, want := range cases {
		ip, n, _ := net.ParseCIDR(cidr)
		n.IP = ip
		got := broadcastAddr(n)
		if (want == "" && got != nil) || (want != "" && got.String() != want) {
			t.Errorf("%s: %v", cidr, got)
		}
	}
	ts, err := browseTargets(context.Background(), "", 1434, nil)
	if err != nil || len(ts) == 0 || !ts[0].IP.Equal(net.IPv4bcast) || ts[0].Port != 1434 {
		t.Errorf("%v %v", ts, err)
	}
}

// CHK-SQL-09: the internal pre-dial guard against np:/lpc:/admin: and UNC.
func TestCHK_SQL_09_ForbiddenServerForm(t *testing.T) {
	for _, s := range []string{"np:evil", "NP:evil", " lpc:x", "admin:srv", "tcp:srv", "sm:x", `\\evil\pipe\sql\query`, `\evil`, "//evil/pipe/x", `srv\pipe\x`} {
		if !forbiddenServerForm(s) {
			t.Errorf("%q allowed", s)
		}
		p := model.Profile{Server: s, AuthMode: model.AuthWindows, Encrypt: model.EncryptDisable}
		if _, err := driverConfig(p, ""); err == nil {
			t.Errorf("%q: driverConfig accepted", s)
		}
	}
	for _, s := range []string{"srv", `srv\SQL2008`, "10.0.0.5", "srv.corp.local", "npsrv", "admins-pc"} {
		if forbiddenServerForm(s) {
			t.Errorf("%q rejected", s)
		}
	}
}
