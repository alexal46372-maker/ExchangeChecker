package sqlcheck

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// fakeTDS answers a PRELOGIN with version 10.50.1600 and encryption enc.
func fakeTDS(t *testing.T, enc byte) string {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				var h [8]byte
				if _, err := io.ReadFull(c, h[:]); err != nil || h[0] != 0x12 {
					return
				}
				body := make([]byte, int(binary.BigEndian.Uint16(h[2:]))-8)
				if _, err := io.ReadFull(c, body); err != nil {
					return
				}
				if _, err := parsePreloginRequest(body); err != nil {
					return
				}
				p := []byte{0, 0, 11, 0, 6, 1, 0, 17, 0, 1, 0xFF, 10, 50, 0x06, 0x40, 0, 0, enc}
				out := append([]byte{0x04, 0x01, 0, 0, 0, 0, 1, 0}, p...)
				binary.BigEndian.PutUint16(out[2:], uint16(len(out)))
				c.Write(out)
			}(c)
		}
	}()
	return ln.Addr().String()
}

// parsePreloginRequest checks the client packet shape (tokens + terminator).
func parsePreloginRequest(p []byte) (int, error) {
	n := 0
	for i := 0; i < len(p); i += 5 {
		if p[i] == 0xFF {
			return n, nil
		}
		n++
	}
	return n, errors.New("no terminator")
}

// CHK-SQL-12: PRELOGIN packet / parser.
func TestCHK_SQL_12_Prelogin(t *testing.T) {
	pkt := preloginPacket(0x02)
	if pkt[0] != 0x12 || int(binary.BigEndian.Uint16(pkt[2:])) != len(pkt) {
		t.Fatalf("%x", pkt)
	}
	if n, err := parsePreloginRequest(pkt[8:]); err != nil || n != 5 {
		t.Fatal(n, err)
	}
	addr := fakeTDS(t, 3)
	info, open, err := prelogin(context.Background(), dialTCP4, addr, 0x02, time.Second)
	if err != nil || !open || info.Version != "10.50.1600" || info.Encryption != SQLEncRequired {
		t.Fatalf("%+v %v %v", info, open, err)
	}
	for _, bad := range [][]byte{{}, {0, 0, 9}, {0, 0, 99, 0, 6, 0xFF}, {1, 0, 6, 0, 1, 0xFF, 0}} {
		if _, err := parsePrelogin(bad); err == nil {
			t.Errorf("%x accepted", bad)
		}
	}
	// a non-TDS listener
	ln, _ := net.Listen("tcp4", "127.0.0.1:0")
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err == nil {
			c.Write([]byte("HTTP/1.0 400 Bad\r\n\r\n"))
			c.Close()
		}
	}()
	if _, open, err := prelogin(context.Background(), dialTCP4, ln.Addr().String(), 0x02, time.Second); !open || err == nil {
		t.Errorf("non-TDS: %v %v", open, err)
	}
}

var dialTCP4 = func(ctx context.Context, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "tcp4", addr)
}

// fakeBrowserAfter answers only from the skip+1-th request on (retries).
func fakeBrowserAfter(t *testing.T, skip int, reply []byte) (*net.UDPAddr, *int32) {
	t.Helper()
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	var n int32
	go func() {
		buf := make([]byte, 512)
		for {
			_, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			if int(atomic.AddInt32(&n, 1)) > skip {
				pc.WriteTo(reply, from)
			}
		}
	}()
	return pc.LocalAddr().(*net.UDPAddr), &n
}

// CHK-SQL-10: SSRP is repeated (3 sends 300 ms apart), answered targets are
// not asked again, progress is reported.
func TestCHK_SQL_10_SSRPRetries(t *testing.T) {
	a, n := fakeBrowserAfter(t, 2, svrResp(srvA))
	var prog []model.SQLBrowseProgress
	res, err := Browse(context.Background(), model.SQLBrowseRequest{Host: "127.0.0.1", TimeoutMs: 3000},
		BrowseOptions{Port: a.Port, Progress: func(p model.SQLBrowseProgress) { prog = append(prog, p) }})
	if err != nil || len(res.Instances) != 2 || res.Sent != 3 || res.Responded != 1 || res.Partial {
		t.Fatalf("%+v %v", res, err)
	}
	if atomic.LoadInt32(n) != 3 {
		t.Errorf("requests %d", *n)
	}
	if len(prog) == 0 || prog[len(prog)-1].Phase != "done" || prog[len(prog)-1].Found == 0 {
		t.Errorf("progress %+v", prog)
	}
	if res.Instances[0].Source != "browser" || res.Mode != model.SQLBrowseHost || res.TCPProbe == nil || res.TCPProbe.Port != 1433 {
		t.Errorf("%+v", res)
	}
	// broadcast (test targets): the full 3 rounds, collected until timeout
	b, nb := fakeBrowserAfter(t, 1, svrResp(srvA))
	res, err = Browse(context.Background(), model.SQLBrowseRequest{TimeoutMs: 1000}, BrowseOptions{Targets: []string{b.String()}})
	if err != nil || len(res.Instances) != 2 || atomic.LoadInt32(nb) < 2 {
		t.Fatalf("%+v %v %d", res, err, *nb)
	}
}

// CHK-SQL-10: send errors are counted and listed, not swallowed; advice.
func TestCHK_SQL_10_SendErrors(t *testing.T) {
	if runtime.GOOS == "linux" { // the real error path: sendto port 0 is EINVAL on Linux
		res, err := Browse(context.Background(), model.SQLBrowseRequest{TimeoutMs: 700}, BrowseOptions{Targets: []string{"127.0.0.1:0"}})
		if err != nil || res.Sent != 0 || res.SendErrorCount != 3 {
			t.Fatalf("%+v %v", res, err)
		}
	}
	fault := func(string) error { return errors.New("sendto: network is unreachable") }
	res, err := Browse(context.Background(), model.SQLBrowseRequest{TimeoutMs: 700}, BrowseOptions{Targets: []string{"127.0.0.1:0"}, sendFault: fault})
	if err != nil {
		t.Fatal(err)
	}
	if res.Sent != 0 || res.SendErrorCount != 3 || len(res.SendErrors) != 3 || res.SendErrors[0].Target != "127.0.0.1:0" || res.SendErrors[0].Error == "" {
		t.Fatalf("%+v", res)
	}
	if !hasKey(res.Advice, model.MsgSQLBrowseAdviceSendFailed) || !hasKey(res.Advice, model.MsgSQLBrowseAdviceOpenUDP1434) || !hasKey(res.Advice, model.MsgSQLBrowseAdvicePort1433) {
		t.Errorf("advice %v", res.Advice)
	}
	for _, k := range res.Advice {
		if model.SQLMessage(k, "ru") == "" || model.SQLMessage(k, "en") == "" {
			t.Errorf("%s has no text", k)
		}
	}
	// the cap of the detail list
	b := &browseRun{seen: map[string]model.SQLServerInstance{}}
	for i := 0; i < 40; i++ {
		b.sendErr("l", "t", errors.New("x"))
	}
	if b.res.SendErrorCount != 40 || len(b.res.SendErrors) != model.MaxSQLBrowseSendErrors {
		t.Errorf("%d %d", b.res.SendErrorCount, len(b.res.SendErrors))
	}
}

func hasKey(l []string, k string) bool {
	for _, x := range l {
		if x == k {
			return true
		}
	}
	return false
}

// CHK-SQL-10: the real broadcast path binds IPv4 sockets on port 0 (one per
// interface + unbound) and keeps going when nothing answers.
func TestCHK_SQL_10_BroadcastSockets(t *testing.T) {
	res, err := Browse(context.Background(), model.SQLBrowseRequest{Mode: model.SQLBrowseBroadcast, TimeoutMs: 700}, BrowseOptions{Port: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Sent+res.SendErrorCount < 3 || res.Local == nil || res.Mode != model.SQLBrowseBroadcast {
		t.Errorf("%+v", res)
	}
	for _, e := range res.SendErrors {
		if e.Target != "bind" && !strings.HasSuffix(e.Target, ":1") {
			t.Errorf("target %+v", e)
		}
	}
}

// CHK-SQL-12: host mode finds a server on its TCP port without the Browser
// (instance with empty name, source tcp).
func TestCHK_SQL_12_HostTCPProbe(t *testing.T) {
	addr := fakeTDS(t, 2)
	_, ps, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(ps)
	res, err := Browse(context.Background(), model.SQLBrowseRequest{Host: "127.0.0.1", Port: port, TimeoutMs: 700}, BrowseOptions{Port: 1})
	if err != nil || len(res.Instances) != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	in := res.Instances[0]
	if in.Source != "tcp" || in.TCPPort != port || in.Version != "10.50.1600" || in.Instance != "" || res.TCPProbe.Prelogin != 1 {
		t.Errorf("%+v %+v", in, res.TCPProbe)
	}
	if hasKey(res.Advice, model.MsgSQLBrowseAdvicePort1433) || !hasKey(res.Advice, model.MsgSQLBrowseAdviceEnableBrowser) {
		t.Errorf("advice %v", res.Advice)
	}
}

// CHK-SQL-12: subnet scan: concurrency bound 32, progress {done,total,found},
// ctx cancel, CIDR rules.
func TestCHK_SQL_12_Subnet(t *testing.T) {
	addr := fakeTDS(t, 2)
	_, ps, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(ps)
	var hosts []string
	for i := 1; i <= 254; i++ {
		hosts = append(hosts, "10.255.0."+strconv.Itoa(i))
	}
	var cur, peak int32
	dial := func(ctx context.Context, a string) (net.Conn, error) {
		c := atomic.AddInt32(&cur, 1)
		defer atomic.AddInt32(&cur, -1)
		for {
			p := atomic.LoadInt32(&peak)
			if c <= p || atomic.CompareAndSwapInt32(&peak, p, c) {
				break
			}
		}
		if strings.HasPrefix(a, "10.255.0.7:") {
			return dialTCP4(ctx, addr)
		}
		select {
		case <-time.After(20 * time.Millisecond):
			return nil, errors.New("refused")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	var mu sync.Mutex
	var last model.SQLBrowseProgress
	calls := 0
	res, err := Browse(context.Background(), model.SQLBrowseRequest{Mode: model.SQLBrowseSubnet, Port: port, TimeoutMs: 5000},
		BrowseOptions{SubnetHosts: hosts, dial: dial, Progress: func(p model.SQLBrowseProgress) {
			mu.Lock()
			if p.Phase == "subnet" {
				last = p
				calls++
			}
			mu.Unlock()
		}})
	if err != nil || len(res.Instances) != 1 || res.Instances[0].Address != "10.255.0.7" || res.TCPProbe.Probed != 254 || res.TCPProbe.Prelogin != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	if peak > model.SQLSubnetConcurrency || peak < 2 {
		t.Errorf("peak %d", peak)
	}
	if last.Done != 254 || last.Total != 254 || last.Found != 1 || calls != 255 {
		t.Errorf("progress %+v %d", last, calls)
	}
	// cancel
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	start := time.Now()
	_, err = Browse(ctx, model.SQLBrowseRequest{Mode: model.SQLBrowseSubnet, TimeoutMs: 5000}, BrowseOptions{SubnetHosts: hosts, dial: dial})
	if KindOf(err) != KindCanceled || time.Since(start) > time.Second {
		t.Errorf("cancel: %v %v", err, time.Since(start))
	}
	// subnetHosts: empty CIDR scans every local /24 (CHK-SQL-15); 254 hosts each
	ifs := []*net.IPNet{{IP: net.IPv4(192, 168, 5, 10).To4(), Mask: net.CIDRMask(16, 32)}, {IP: net.IPv4(10, 1, 2, 3).To4(), Mask: net.CIDRMask(24, 32)}}
	h, used, others, narrowed, err := subnetHosts("", ifs)
	if err != nil || !narrowed || len(h) != 508 || used != "192.168.5.0/24" || h[0] != "192.168.5.1" || h[253] != "192.168.5.254" || h[254] != "10.1.2.1" || h[507] != "10.1.2.254" || len(others) != 1 || others[0] != "10.1.2.0/24" {
		t.Errorf("%d %s %v %v", len(h), used, others, err)
	}
	if _, _, _, nar, _ := subnetHosts("", ifs[1:]); nar {
		t.Error("a /24 interface is not narrowed")
	}
	if h, _, _, nar, err := subnetHosts("10.1.2.0/30", ifs); err != nil || len(h) != 2 || nar {
		t.Errorf("/30: %v %v", h, err)
	}
	if _, _, _, _, err := subnetHosts("172.16.0.0/24", ifs); !errors.Is(err, errNotLocalSubnet) {
		t.Errorf("foreign: %v", err)
	}
	for _, c := range []string{"10.0.0.0/16", "10.0.0.0/31", "fe80::/64", "x", "10.0.0.1"} {
		r := model.SQLBrowseRequest{Mode: model.SQLBrowseSubnet, CIDR: c}
		var fe *model.FieldError
		if err := r.Validate(); !errors.As(err, &fe) || fe.Key != model.ValSQLBrowseCIDR {
			t.Errorf("%s: %v", c, err)
		}
	}
	if _, err := Browse(context.Background(), model.SQLBrowseRequest{Mode: model.SQLBrowseSubnet, CIDR: "203.0.113.0/24"}, BrowseOptions{}); KindOf(err) != KindConfig {
		t.Errorf("foreign subnet: %v", err)
	}
}

type fakeLocal struct {
	multi map[string][]string
	strs  map[string]string
	state string
}

func (fakeLocal) supported() bool { return true }
func (f fakeLocal) multiString(view int, path, name string) ([]string, error) {
	return f.multi[strconv.Itoa(view)+"|"+name], nil
}
func (f fakeLocal) str(view int, path, name string) (string, bool, error) {
	v, ok := f.strs[strconv.Itoa(view)+"|"+path+"|"+name]
	return v, ok, nil
}
func (f fakeLocal) browserState() string { return f.state }

// CHK-SQL-11: local instances from the registry (both views), ports,
// browser state, unicast to 127.0.0.1, advice.
func TestCHK_SQL_11_Local(t *testing.T) {
	k := sqlServerKey
	f := fakeLocal{
		multi: map[string][]string{"64|InstalledInstances": {"MSSQLSERVER", "SQLEXPRESS"}, "32|InstalledInstances": {"SQLEXPRESS", "OLD"}},
		strs: map[string]string{
			"64|" + k + `\Instance Names\SQL|MSSQLSERVER`:                                                "MSSQL10_50.MSSQLSERVER",
			"64|" + k + `\MSSQL10_50.MSSQLSERVER\MSSQLServer\SuperSocketNetLib\Tcp\IPAll|TcpPort`:        "1433",
			"64|" + k + `\MSSQL10_50.MSSQLSERVER\Setup|Version`:                                          "10.50.1600.1",
			"64|" + k + `\Instance Names\SQL|SQLEXPRESS`:                                                 "MSSQL10_50.SQLEXPRESS",
			"64|" + k + `\MSSQL10_50.SQLEXPRESS\MSSQLServer\SuperSocketNetLib\Tcp\IPAll|TcpPort`:         "",
			"64|" + k + `\MSSQL10_50.SQLEXPRESS\MSSQLServer\SuperSocketNetLib\Tcp\IPAll|TcpDynamicPorts`: "49172",
			"32|" + k + `\Instance Names\SQL|OLD`:                                                        `..\evil`,
		},
		state: model.SQLBrowserStopped,
	}
	names, insts, err := localInstances(f)
	if err != nil || strings.Join(names, ",") != "MSSQLSERVER,OLD,SQLEXPRESS" || len(insts) != 3 {
		t.Fatalf("%v %+v %v", names, insts, err)
	}
	if insts[0].TCPPort != 1433 || insts[0].Version != "10.50.1600.1" || insts[1].TCPPort != 49172 || insts[2].TCPPort != 0 || insts[0].Source != "local" || insts[0].Address != "127.0.0.1" {
		t.Errorf("%+v", insts)
	}
	for in, want := range map[string]int{"1433": 1433, " 1433, 1434": 1433, "": 0, "0": 0, "70000": 0, "x": 0} {
		if firstPort(in) != want {
			t.Errorf("%q", in)
		}
	}
	a, _ := fakeBrowserAfter(t, 0, svrResp("ServerName;ME;InstanceName;SQLEXPRESS;tcp;49172;;"))
	res, err := Browse(context.Background(), model.SQLBrowseRequest{Mode: model.SQLBrowseLocal, TimeoutMs: 1000}, BrowseOptions{Targets: []string{a.String()}, local: f})
	if err != nil || res.Local == nil || !res.Local.Supported || len(res.Local.Installed) != 3 || res.Local.BrowserState != model.SQLBrowserStopped || !res.Local.BrowserAnswered {
		t.Fatalf("%+v %v", res, err)
	}
	if !hasKey(res.Advice, model.MsgSQLBrowseAdviceEnableBrowser) || !hasKey(res.Advice, model.MsgSQLBrowseAdviceTCPDisabled) {
		t.Errorf("advice %v", res.Advice)
	}
	// unsupported (non-Windows default)
	res, err = Browse(context.Background(), model.SQLBrowseRequest{Mode: model.SQLBrowseLocal, TimeoutMs: 500}, BrowseOptions{Port: 1, local: unsupportedLocal{}})
	if err != nil || res.Local == nil || res.Local.Supported || res.Local.Installed == nil {
		t.Errorf("%+v %v", res, err)
	}
}

type unsupportedLocal struct{ fakeLocal }

func (unsupportedLocal) supported() bool { return false }

// CHK-SQL-13: step-by-step check against a fake TDS server (no profile:
// parse / dns / tcp / prelogin / tls), the browser stage, failures.
func TestCHK_SQL_13_CheckServerFake(t *testing.T) {
	addr := fakeTDS(t, 2)
	_, ps, _ := net.SplitHostPort(addr)
	var pushed []string
	res, err := CheckServer(context.Background(), "127.0.0.1,"+ps, ServerCheckOptions{Step: func(s model.SQLCheckStep) { pushed = append(pushed, s.ID) }})
	if err != nil || !res.OK || res.Version != "10.50.1600" || res.Encryption != SQLEncNotSupported || strings.Join(pushed, ",") != "parse,dns,tcp,prelogin,tls" {
		t.Fatalf("%+v %v %v", res, err, pushed)
	}
	// encryption required by the server, profile without encryption → tls fails
	req := fakeTDS(t, 3)
	_, rp, _ := net.SplitHostPort(req)
	res, _ = CheckServer(context.Background(), "127.0.0.1,"+rp, ServerCheckOptions{})
	if res.OK || last(res).ID != model.SQLStepTLS || last(res).Code != model.SQLCodeTLS || res.Diagnosis == nil || last(res).AdviceKey != model.MsgSQLCheckAdviceTLS {
		t.Errorf("%+v", res)
	}
	// closed port → tcp
	ln, _ := net.Listen("tcp4", "127.0.0.1:0")
	closed := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	res, _ = CheckServer(context.Background(), "127.0.0.1,"+strconv.Itoa(closed), ServerCheckOptions{})
	if res.OK || last(res).ID != model.SQLStepTCP || last(res).Code != model.SQLCodeNetwork || len(res.Steps) != 3 {
		t.Errorf("%+v", res)
	}
	// forbidden form → parse
	for _, s := range []string{`np:\\x\pipe\sql`, "host,0", "host,x", `\\srv`, ""} {
		res, _ = CheckServer(context.Background(), s, ServerCheckOptions{})
		if res.OK || len(res.Steps) != 1 || res.Steps[0].ID != model.SQLStepParse || res.Steps[0].Code != model.SQLCodeConfig {
			t.Errorf("%q: %+v", s, res)
		}
	}
	// instance without a port → browser stage
	_, port, _ := net.SplitHostPort(addr)
	b, _ := fakeBrowserAfter(t, 0, svrResp("ServerName;X;InstanceName;SQL2008;tcp;"+port+";;"))
	res, _ = CheckServer(context.Background(), `127.0.0.1\SQL2008`, ServerCheckOptions{BrowserPort: b.Port, Timeout: time.Second})
	if !res.OK || res.Steps[2].ID != model.SQLStepBrowser || strconv.Itoa(res.Port) != port {
		t.Errorf("%+v", res)
	}
	res, _ = CheckServer(context.Background(), `127.0.0.1\NOPE`, ServerCheckOptions{BrowserPort: 1, Timeout: 700 * time.Millisecond})
	if res.OK || last(res).ID != model.SQLStepBrowser || last(res).Code != model.SQLCodeBrowserUnreachable {
		t.Errorf("%+v", res)
	}
	// every advice key has RU/EN text
	for _, id := range model.SQLCheckSteps() {
		k := model.SQLCheckAdviceKey(id)
		if model.SQLMessage(k, "ru") == "" || model.SQLMessage(k, "en") == "" {
			t.Errorf("%s", k)
		}
	}
	// ctx
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CheckServer(ctx, "127.0.0.1,"+ps, ServerCheckOptions{}); KindOf(err) != KindCanceled {
		t.Errorf("cancel: %v", err)
	}
}

func last(r model.SQLServerCheckResult) model.SQLCheckStep { return r.Steps[len(r.Steps)-1] }

// CHK-SQL-12 (review): a /16 interface — only its own /24 is scanned and
// the wizard advice subnet_narrowed is emitted; the default / empty mode
// never runs the subnet scan.
func TestCHK_SQL_12_SubnetNarrowedAndDefaultMode(t *testing.T) {
	dials := int32(0)
	dial := func(ctx context.Context, a string) (net.Conn, error) {
		atomic.AddInt32(&dials, 1)
		return nil, errors.New("refused")
	}
	wide := func() []*net.IPNet {
		return []*net.IPNet{{IP: net.IPv4(192, 168, 5, 10).To4(), Mask: net.CIDRMask(16, 32)}}
	}
	res, err := Browse(context.Background(), model.SQLBrowseRequest{Mode: model.SQLBrowseSubnet, TimeoutMs: 5000}, BrowseOptions{dial: dial, ifaces: wide})
	if err != nil || res.TCPProbe == nil || !res.TCPProbe.Narrowed || res.TCPProbe.CIDR != "192.168.5.0/24" || res.TCPProbe.Probed != 254 ||
		!hasKey(res.Advice, model.MsgSQLBrowseAdviceSubnetNarrowed) {
		t.Fatalf("%+v %v", res, err)
	}
	if model.SQLMessage(model.MsgSQLBrowseAdviceSubnetNarrowed, "ru") == "" || model.SQLMessage(model.MsgSQLBrowseAdviceSubnetNarrowed, "en") == "" {
		t.Error("no text")
	}
	// mode "" (no host) is broadcast: no TCP scan at all
	atomic.StoreInt32(&dials, 0)
	res, err = Browse(context.Background(), model.SQLBrowseRequest{TimeoutMs: 500}, BrowseOptions{Port: 1, dial: dial, ifaces: wide})
	if err != nil || res.Mode != model.SQLBrowseBroadcast || res.TCPProbe != nil || atomic.LoadInt32(&dials) != 0 {
		t.Errorf("default mode: %+v %v", res, err)
	}
	if (model.SQLBrowseRequest{}).EffectiveMode() == model.SQLBrowseSubnet || (model.SQLBrowseRequest{Host: "x"}).EffectiveMode() == model.SQLBrowseSubnet {
		t.Error("subnet must be explicit")
	}
	// subnet timeout bound 30 s default / 45 s max (CHK-SQL-15), others 5 s
	if (model.SQLBrowseRequest{Mode: model.SQLBrowseSubnet, TimeoutMs: 30000}).Validate() != nil ||
		(model.SQLBrowseRequest{Mode: model.SQLBrowseSubnet, TimeoutMs: 45001}).Validate() == nil ||
		(model.SQLBrowseRequest{TimeoutMs: 15000}).Validate() == nil ||
		(model.SQLBrowseRequest{Mode: model.SQLBrowseSubnet}).Timeout() != 30000 ||
		model.ClampSQLBrowseTimeoutMsFor(model.SQLBrowseSubnet, 60000) != 45000 || model.ClampSQLBrowseTimeoutMsFor("", 60000) != 5000 {
		t.Error("timeouts")
	}
}

// CHK-SQL-10 (review): 255.255.255.255 goes out once per interface socket
// per round; the unbound socket is only the fallback without interfaces.
func TestCHK_SQL_10_BroadcastOncePerInterface(t *testing.T) {
	lo := func() []*net.IPNet {
		return []*net.IPNet{{IP: net.IPv4(127, 0, 0, 1).To4(), Mask: net.CIDRMask(8, 32)}}
	}
	var mu sync.Mutex
	var targets []string
	rec := func(tg string) error { mu.Lock(); targets = append(targets, tg); mu.Unlock(); return nil }
	res, err := Browse(context.Background(), model.SQLBrowseRequest{Mode: model.SQLBrowseBroadcast, TimeoutMs: 500},
		BrowseOptions{Port: 1, Sends: 2, SendInterval: 100 * time.Millisecond, ifaces: lo, sendFault: rec})
	if err != nil {
		t.Fatal(err)
	}
	bc := 0
	for _, tg := range targets {
		if tg == "255.255.255.255:1" {
			bc++
		}
	}
	// loopback has no directed broadcast: exactly 255.255.255.255 per round,
	// no second copy from the unbound socket
	if bc != 2 || len(targets) != 2 || res.Sent+res.SendErrorCount != 2 {
		t.Errorf("one interface, 2 rounds: %v %+v", targets, res)
	}
	for _, e := range res.SendErrors {
		if e.Local != "127.0.0.1" {
			t.Errorf("unbound socket used: %+v", e)
		}
	}
	targets = nil
	none := func() []*net.IPNet { return nil }
	res, err = Browse(context.Background(), model.SQLBrowseRequest{Mode: model.SQLBrowseBroadcast, TimeoutMs: 500},
		BrowseOptions{Port: 1, Sends: 2, SendInterval: 100 * time.Millisecond, ifaces: none, sendFault: rec})
	if err != nil || len(targets) != 2 || targets[0] != "255.255.255.255:1" || res.Sent+res.SendErrorCount != 2 {
		t.Errorf("fallback: %v %+v %v", targets, res, err)
	}
}

// CHK-SQL-11 (review): SQLBrowser start type Disabled → "disabled" and
// the advice browser_disabled (fake SCM through the local source and the
// pure state mapper used by the Windows SCM reader).
func TestCHK_SQL_11_BrowserDisabled(t *testing.T) {
	cases := []struct {
		state, start uint32
		have         bool
		want         string
	}{
		{1, 4, true, model.SQLBrowserDisabled}, {4, 4, true, model.SQLBrowserDisabled}, {1, 3, true, model.SQLBrowserStopped},
		{4, 2, true, model.SQLBrowserRunning}, {1, 4, false, model.SQLBrowserStopped}, {7, 3, true, model.SQLBrowserPaused},
		{2, 3, true, model.SQLBrowserStartPending}, {3, 3, true, model.SQLBrowserStopPending}, {9, 3, true, model.SQLBrowserUnknown},
	}
	for _, c := range cases {
		if got := mapBrowserState(c.state, c.start, c.have); got != c.want {
			t.Errorf("%+v: %s", c, got)
		}
	}
	f := fakeLocal{multi: map[string][]string{"64|InstalledInstances": {"MSSQLSERVER"}}, state: model.SQLBrowserDisabled}
	res, err := Browse(context.Background(), model.SQLBrowseRequest{Mode: model.SQLBrowseLocal, TimeoutMs: 500}, BrowseOptions{Port: 1, local: f})
	if err != nil || res.Local.BrowserState != model.SQLBrowserDisabled || !hasKey(res.Advice, model.MsgSQLBrowseAdviceBrowserDisabled) || hasKey(res.Advice, model.MsgSQLBrowseAdviceEnableBrowser) {
		t.Fatalf("%+v %v", res, err)
	}
	k := model.MsgSQLBrowseAdviceBrowserDisabled
	if model.SQLMessage(k, "ru") == "" || model.SQLMessage(k, "en") == "" || !hasKey(model.SQLWizardMessageKeys(), k) {
		t.Error("not registered")
	}
}

// CHK-SQL-14: ListPoints (linked time mapping on _Node123) and
// ReadNodeStates (counters on _Node123) report the same NodeKey per node.
func TestCHK_SQL_14_NodeKeyMatch(t *testing.T) {
	st := newFake(fx1CNode(2000)...)
	checkNodeKeysMatch(t, st)
	// unit: the shared key function
	m := model.Mapping{Table: "_Node123"}
	if k := NodeKey(m, []byte{0xab, 0x01}, "001", "x"); k != "_Node123:ab01" {
		t.Error(k)
	}
	if k := NodeKey(m, nil, " 001 ", "x"); k != "_Node123:code:001" {
		t.Error(k)
	}
	if k := NodeKey(model.Mapping{Table: "_InfoRg456"}, []byte{1}, "", ""); k != "" {
		t.Errorf("non-plan time table: %q", k)
	}
	if k := NodeKey(model.Mapping{Mode: model.MappingCounters, Table: "Custom"}, nil, "", "n"); k != "Custom:name:n" {
		t.Error(k)
	}
	// single (non-linked) mapping on a plan table carries the key via MIN(t.k)
	q, err := buildPointsSQL(model.Mapping{Mode: model.MappingManual, Table: "_Node123", PointNameColumn: "_Description", PointKeyColumn: "_IDRRef", LastSendColumn: "_Fld1"})
	if err != nil || !strings.Contains(q, "MIN(t.k)") || !strings.Contains(q, "[_IDRRef] AS k") {
		t.Errorf("%s %v", q, err)
	}
}

// checkNodeKeysMatch runs both paths on st (fake or the real fixture).
func checkNodeKeysMatch(t *testing.T, st Store) {
	t.Helper()
	p := linkedProfile(2000)
	p.ID = "nk"
	pts, err := NewDiscoveringReader(st, p, fixOpt()).ListPoints(context.Background())
	if err != nil || len(pts) == 0 {
		t.Fatalf("%v %v", pts, err)
	}
	c := p
	c.Mapping = model.Mapping{Mode: model.MappingCounters, Table: "_Node123", PointNameColumn: "_Description", PointIDColumn: "_Code", PointKeyColumn: "_IDRRef"}
	opt := fixOpt()
	opt.Counters = model.NewMemCounterStore()
	ns, err := ReadNodeStates(context.Background(), st, c, opt)
	if err != nil || len(ns) == 0 {
		t.Fatalf("%v %v", ns, err)
	}
	byName := map[string]string{}
	for _, n := range ns {
		if !strings.HasPrefix(n.Key, "_Node123:") || len(n.Key) != len("_Node123:")+32 {
			t.Errorf("state key %q", n.Key)
		}
		byName[n.Name] = n.Key
	}
	for _, pt := range pts {
		if pt.NodeKey == "" || pt.NodeKey != byName[pt.Name] {
			t.Errorf("%s: point %q, state %q", pt.Name, pt.NodeKey, byName[pt.Name])
		}
	}
	// counters-mode ListPoints carries the same NodeKey too
	cp, err := NewDiscoveringReader(st, c, opt).ListPoints(context.Background())
	if err != nil || len(cp) != len(ns) {
		t.Fatalf("%v %v", cp, err)
	}
	for _, pt := range cp {
		if pt.NodeKey != byName[pt.Name] {
			t.Errorf("counters point %s: %q vs %q", pt.Name, pt.NodeKey, byName[pt.Name])
		}
	}
	t.Logf("matched %d time points, %d node states", len(pts), len(ns))
}

// CHK-SQL-12: narrowed reflects the interface whose network is scanned
// (empty CIDR scans every local /24 — narrowed if any is from a wide iface).
func TestCHK_SQL_12_NarrowedUsedInterface(t *testing.T) {
	ifs := []*net.IPNet{{IP: net.IPv4(10, 1, 2, 3).To4(), Mask: net.CIDRMask(24, 32)}, {IP: net.IPv4(172, 20, 5, 9).To4(), Mask: net.CIDRMask(16, 32)}}
	h, used, others, nar, err := subnetHosts("", ifs)
	if err != nil || used != "10.1.2.0/24" || !nar || len(h) != 508 || len(others) != 1 || others[0] != "172.20.5.0/24" {
		t.Errorf("empty CIDR all /24s: %d %s %v %v %v", len(h), used, others, nar, err)
	}
	if _, used, _, nar, err := subnetHosts("172.20.7.0/24", ifs); err != nil || used != "172.20.7.0/24" || !nar {
		t.Errorf("inside the /16 interface: %s %v %v", used, nar, err)
	}
	if _, _, _, nar, err := subnetHosts("10.1.2.0/25", ifs); err != nil || nar {
		t.Errorf("inside the /24 interface: %v %v", nar, err)
	}
	rev := []*net.IPNet{ifs[1], ifs[0]}
	if _, used, _, nar, err := subnetHosts("10.1.2.0/24", rev); err != nil || used != "10.1.2.0/24" || nar {
		t.Errorf("ifs[0] is /16 but the /24 one is used: %v %v", nar, err)
	}
}

// CHK-SQL-15: hosts whose first dial is slower than the old 300 ms probe
// budget but within SQLSubnetProbeTimeoutMs are found in a single Browse
// (multi-click with the old 300 ms budget would miss on the first pass).
func TestCHK_SQL_15_ThoroughProbeTimeout(t *testing.T) {
	addr := fakeTDS(t, 2)
	_, ps, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(ps)
	var attempts sync.Map // host -> attempt count
	slow := map[string]bool{"10.9.9.11": true, "10.9.9.12": true, "10.9.9.13": true}
	dial := func(ctx context.Context, a string) (net.Conn, error) {
		host, _, _ := net.SplitHostPort(a)
		n := 1
		if v, ok := attempts.Load(host); ok {
			n = v.(int) + 1
		}
		attempts.Store(host, n)
		if slow[host] && n == 1 {
			// Slower than old SQLProbeTimeoutMs (300), faster than new 1200 ms.
			select {
			case <-time.After(500 * time.Millisecond):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return dialTCP4(ctx, addr)
		}
		if slow[host] {
			return dialTCP4(ctx, addr)
		}
		return nil, errors.New("refused")
	}
	hosts := []string{"10.9.9.11", "10.9.9.12", "10.9.9.13", "10.9.9.14", "10.9.9.15"}
	// Old budget would cancel the slow dials via WithTimeout(300ms).
	oldTO := time.Duration(model.SQLProbeTimeoutMs) * time.Millisecond
	var missed int
	for _, h := range hosts {
		if !slow[h] {
			continue
		}
		c, err := func() (net.Conn, error) {
			ctx, cancel := context.WithTimeout(context.Background(), oldTO)
			defer cancel()
			return dial(ctx, net.JoinHostPort(h, strconv.Itoa(port)))
		}()
		if err != nil {
			missed++
			continue
		}
		c.Close()
	}
	if missed != 3 {
		t.Fatalf("old 300ms budget should miss the 3 slow hosts, missed=%d", missed)
	}
	attempts = sync.Map{}
	res, err := Browse(context.Background(), model.SQLBrowseRequest{Mode: model.SQLBrowseSubnet, Port: port, TimeoutMs: 10000},
		BrowseOptions{SubnetHosts: hosts, dial: dial})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Instances) != 3 || res.TCPProbe == nil || res.TCPProbe.Probed != 5 || res.TCPProbe.Prelogin != 3 || res.Partial {
		t.Fatalf("thorough Browse: %+v %+v", res.Instances, res.TCPProbe)
	}
	if res.TCPProbe.Probed != len(hosts) {
		t.Errorf("Probed==Total want %d got %d", len(hosts), res.TCPProbe.Probed)
	}
}

// CHK-SQL-15: empty CIDR Browse scans every distinct local /24, not only ifs[0].
func TestCHK_SQL_15_MultiSlash24(t *testing.T) {
	addr := fakeTDS(t, 2)
	_, ps, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(ps)
	ifs := func() []*net.IPNet {
		return []*net.IPNet{
			{IP: net.IPv4(10, 0, 0, 5).To4(), Mask: net.CIDRMask(24, 32)},
			{IP: net.IPv4(10, 1, 1, 8).To4(), Mask: net.CIDRMask(24, 32)},
		}
	}
	dial := func(ctx context.Context, a string) (net.Conn, error) {
		if strings.HasPrefix(a, "10.1.1.42:") {
			return dialTCP4(ctx, addr)
		}
		return nil, errors.New("refused")
	}
	res, err := Browse(context.Background(), model.SQLBrowseRequest{Mode: model.SQLBrowseSubnet, Port: port, TimeoutMs: 15000},
		BrowseOptions{dial: dial, ifaces: ifs})
	if err != nil {
		t.Fatal(err)
	}
	if res.TCPProbe == nil || res.TCPProbe.CIDR != "10.0.0.0/24" || len(res.TCPProbe.Subnets) != 1 || res.TCPProbe.Subnets[0] != "10.1.1.0/24" {
		t.Fatalf("tcpProbe %+v", res.TCPProbe)
	}
	if res.TCPProbe.Probed != 508 || res.Partial {
		t.Fatalf("probed %d partial %v", res.TCPProbe.Probed, res.Partial)
	}
	if len(res.Instances) != 1 || res.Instances[0].Address != "10.1.1.42" || res.TCPProbe.Prelogin != 1 {
		t.Fatalf("second-/24 server: %+v %+v", res.Instances, res.TCPProbe)
	}
}
