package sqlcheck

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Phase 9 (PLAN §12 p. 105, CHK-SQL-06): SQL Server discovery through the
// SQL Server Browser (SSRP, [MC-SQLR], UDP 1434). Networking: Go
// net.ListenPacket (UDP) inside sqlcheck by the explicit phase 9 task
// (POLICY: no hcwinnet equivalent for UDP broadcast); no os/exec.

// SSRP message types ([MC-SQLR] 2.2).
const (
	ssrpClntBcastEx  = 0x02
	ssrpClntUcastEx  = 0x03
	ssrpClntUcastIns = 0x04
	ssrpSvrResp      = 0x05
	// ssrpMaxResp: RESP_DATA is at most 1024 bytes for CLNT_UCAST_INST and
	// 65535 for the others; a datagram cannot be larger anyway.
	ssrpMaxResp = 65535 + 3
	// ssrpMaxValue caps one value (sanitized).
	ssrpMaxValue = 255
	// ssrpMaxDatagrams caps the datagrams handled by one Browse.
	ssrpMaxDatagrams = 4096
)

// SQLBrowserPort is the UDP port of the SQL Server Browser.
const SQLBrowserPort = 1434

// BrowseOptions configures Browse (zero value: defaults).
type BrowseOptions struct {
	// Port of the SQL Browser (0 = 1434).
	Port int
	// Targets overrides the SSRP destinations ("ip:port"), for tests.
	Targets []string
	// MaxInstances caps the result (0 = model.MaxSQLBrowseInstances).
	MaxInstances int
	// Progress, if set, is called synchronously from Browse for every
	// progress event (phase local / ssrp / subnet / host / done); it must
	// return quickly. At most one call per sent SSRP round, per answer and
	// per probed address.
	Progress func(model.SQLBrowseProgress)
	// Sends / SendInterval: SSRP repetitions (0 = 3 / 300 ms).
	Sends        int
	SendInterval time.Duration
	// SubnetHosts overrides the subnet addresses ("ip"), for tests.
	SubnetHosts []string
	// ProbeTimeout per TCP probe (0 = 300 ms); Concurrency (0 = 32).
	ProbeTimeout time.Duration
	Concurrency  int

	dial func(ctx context.Context, addr string) (net.Conn, error) // tests
	// sendFault, if set, fails the matching sends (tests: Windows accepts
	// sends that Linux rejects, so real errors are not portable).
	sendFault func(target string) error
	local     localSource // tests
}

// ListServers is Browse with default options (EC Tray naming).
func ListServers(ctx context.Context, req model.SQLBrowseRequest) (model.SQLBrowseResult, error) {
	return Browse(ctx, req, BrowseOptions{})
}

// browseRun is the state of one Browse.
type browseRun struct {
	opt  BrowseOptions
	res  model.SQLBrowseResult
	mu   sync.Mutex
	seen map[string]model.SQLServerInstance
	// progMu serializes the Progress callback (host mode probes in parallel).
	progMu sync.Mutex
}

func (b *browseRun) progress(phase string, done, total int) {
	if b.opt.Progress == nil {
		return
	}
	b.mu.Lock()
	n := len(b.seen)
	b.mu.Unlock()
	b.progMu.Lock()
	defer b.progMu.Unlock()
	b.opt.Progress(model.SQLBrowseProgress{Phase: phase, Done: done, Total: total, Found: n})
}

// add records an instance (dedupe: server name + instance; TCP-probe
// finds without a name: address + port, dropped when SSRP or local already
// announced that address and port).
func (b *browseRun) add(in model.SQLServerInstance) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := strings.ToUpper(in.Host) + "\x00" + strings.ToUpper(in.Instance)
	if in.Source == "tcp" {
		key = "tcp\x00" + in.Address + ":" + strconv.Itoa(in.TCPPort)
	}
	if _, dup := b.seen[key]; !dup && len(b.seen) < ssrpMaxDatagrams {
		b.seen[key] = in
	}
}

// announced: an SSRP / local instance has the address and port of the
// TCP-probe find in (caller holds mu).
func (b *browseRun) announced(in model.SQLServerInstance) bool {
	for _, o := range b.seen {
		if o.Source != "tcp" && o.TCPPort == in.TCPPort && (o.Address == in.Address || strings.EqualFold(o.Host, in.Host)) {
			return true
		}
	}
	return false
}

func (b *browseRun) sendErr(local, target string, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.res.SendErrorCount++
	if len(b.res.SendErrors) < model.MaxSQLBrowseSendErrors {
		b.res.SendErrors = append(b.res.SendErrors, model.SQLBrowseSendErr{Local: local, Target: target, Error: err.Error()})
	}
}

// Browse finds SQL Server instances (phase 9 + 11, CHK-SQL-06, CHK-SQL-10..12):
//
//   - broadcast: SSRP CLNT_BCAST_EX from one socket per IPv4 interface
//     (bound to its address, ephemeral port) to 255.255.255.255 and the
//     interface's directed broadcast, plus an unbound socket to
//     255.255.255.255; then local;
//   - local: this computer (registry InstalledInstances + TCP ports,
//     SQLBrowser service state; Windows only) and SSRP unicast to
//     127.0.0.1:1434;
//   - host: SSRP CLNT_UCAST_EX / CLNT_UCAST_INST to the host's IPv4
//     addresses and a TDS PRELOGIN probe of host:Port (1433);
//   - subnet: TDS PRELOGIN probe of every address of one /24 (CIDR or the
//     first interface's /24), 32 in parallel, 300 ms each.
//
// SSRP is sent 3 times 300 ms apart; answers are collected until
// req.Timeout() (3 s; unicast: until every address answered). In
// addressed mode only answers from the exact ip:port asked are accepted.
// Send errors are counted and listed (never swallowed). No answer is not
// an error; Advice says what to check. ctx cancel: *Error KindCanceled.
func Browse(ctx context.Context, req model.SQLBrowseRequest, opt BrowseOptions) (model.SQLBrowseResult, error) {
	b := &browseRun{opt: opt, seen: map[string]model.SQLServerInstance{}}
	if err := req.Validate(); err != nil {
		return b.res, &Error{Kind: KindConfig, Op: "browse", Err: err}
	}
	if err := ctx.Err(); err != nil {
		return b.res, ctxErr("browse", err)
	}
	if b.opt.Port <= 0 || b.opt.Port > 65535 {
		b.opt.Port = SQLBrowserPort
	}
	if b.opt.Sends <= 0 {
		b.opt.Sends = model.SQLBrowseSends
	}
	if b.opt.SendInterval <= 0 {
		b.opt.SendInterval = model.SQLBrowseSendIntervalMs * time.Millisecond
	}
	if b.opt.ProbeTimeout <= 0 {
		b.opt.ProbeTimeout = model.SQLProbeTimeoutMs * time.Millisecond
	}
	if b.opt.Concurrency <= 0 || b.opt.Concurrency > model.SQLSubnetConcurrency {
		b.opt.Concurrency = model.SQLSubnetConcurrency
	}
	if b.opt.dial == nil {
		b.opt.dial = func(ctx context.Context, addr string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "tcp4", addr)
		}
	}
	if b.opt.local == nil {
		b.opt.local = systemLocal{}
	}
	max := opt.MaxInstances
	if req.Limit > 0 && (max <= 0 || req.Limit < max) {
		max = req.Limit
	}
	if max <= 0 || max > model.MaxSQLBrowseInstances {
		max = model.MaxSQLBrowseInstances
	}
	mode := req.EffectiveMode()
	b.res.Mode = mode
	ctx, cancel := context.WithTimeout(ctx, time.Duration(req.Timeout())*time.Millisecond)
	defer cancel()
	tcpPort := req.Port
	if tcpPort == 0 {
		tcpPort = model.DefaultSQLPort
	}
	var err error
	switch mode {
	case model.SQLBrowseBroadcast:
		err = b.ssrp(ctx, req, false)
		if err == nil {
			err = b.localPhase(ctx, false)
		}
	case model.SQLBrowseLocal:
		err = b.localPhase(ctx, true)
	case model.SQLBrowseHost:
		// the TCP probe runs alongside SSRP (both within the timeout)
		b.res.TCPProbe = &model.SQLBrowseTCPInfo{Port: tcpPort}
		probed := make(chan struct{})
		go func() { defer close(probed); b.probeHost(ctx, req.Host, tcpPort) }()
		err = b.ssrp(ctx, req, true)
		<-probed
	case model.SQLBrowseSubnet:
		err = b.subnet(ctx, req.CIDR, tcpPort)
	}
	if err != nil {
		return b.res, err
	}
	if cerr := ctx.Err(); errors.Is(cerr, context.Canceled) {
		return b.res, ctxErr("browse", cerr)
	}
	b.mu.Lock()
	for _, in := range b.seen {
		if in.Source == "tcp" && b.announced(in) {
			continue
		}
		b.res.Instances = append(b.res.Instances, in)
	}
	b.mu.Unlock()
	sort.Slice(b.res.Instances, func(i, j int) bool {
		x, y := b.res.Instances[i], b.res.Instances[j]
		if !strings.EqualFold(x.Host, y.Host) {
			return strings.ToUpper(x.Host) < strings.ToUpper(y.Host)
		}
		if !strings.EqualFold(x.Instance, y.Instance) {
			return strings.ToUpper(x.Instance) < strings.ToUpper(y.Instance)
		}
		return x.Address+strconv.Itoa(x.TCPPort) < y.Address+strconv.Itoa(y.TCPPort)
	})
	b.res.Instances, b.res.Truncated = model.CapSQLList(b.res.Instances, max)
	if b.res.Instances == nil {
		b.res.Instances = []model.SQLServerInstance{}
	}
	b.res.Partial = b.res.Partial || b.res.Truncated || b.res.Malformed > 0 || b.res.TCPProbe != nil && b.res.TCPProbe.Truncated
	b.res.Advice = browseAdvice(b.res)
	b.progress("done", 1, 1)
	return b.res, nil
}

// browseAdvice derives the advice keys from the diagnostics.
func browseAdvice(r model.SQLBrowseResult) []string {
	var out []string
	add := func(k string) {
		for _, x := range out {
			if x == k {
				return
			}
		}
		out = append(out, k)
	}
	if r.Sent == 0 && r.SendErrorCount > 0 {
		add(model.MsgSQLBrowseAdviceSendFailed)
	}
	if l := r.Local; l != nil && l.Supported {
		if len(l.Installed) > 0 && l.BrowserState != model.SQLBrowserRunning {
			add(model.MsgSQLBrowseAdviceEnableBrowser)
		}
		for _, in := range r.Instances {
			if in.Source == "local" && in.TCPPort == 0 {
				add(model.MsgSQLBrowseAdviceTCPDisabled)
			}
		}
	}
	if r.Responded == 0 && r.Mode != model.SQLBrowseSubnet {
		if r.Mode != model.SQLBrowseLocal {
			add(model.MsgSQLBrowseAdviceEnableBrowser)
		}
		add(model.MsgSQLBrowseAdviceOpenUDP1434)
	}
	if len(r.Instances) == 0 {
		if r.Mode != model.SQLBrowseHost {
			add(model.MsgSQLBrowseAdviceEnterServer)
		}
		add(model.MsgSQLBrowseAdvicePort1433)
	}
	return out
}

// ssrpSock is one UDP socket of an SSRP round with its destinations.
type ssrpSock struct {
	pc      net.PacketConn
	local   string
	targets []*net.UDPAddr
}

type ssrpPacket struct {
	data []byte
	from net.Addr
}

// ssrp runs the SSRP part (broadcast or addressed).
func (b *browseRun) ssrp(ctx context.Context, req model.SQLBrowseRequest, addressedHost bool) error {
	msg := []byte{ssrpClntBcastEx}
	if req.Host != "" {
		msg = []byte{ssrpClntUcastEx}
		if req.Instance != "" {
			msg = append(append([]byte{ssrpClntUcastIns}, req.Instance...), 0)
		}
	}
	host := ""
	if addressedHost {
		host = req.Host
	}
	targets, err := browseTargets(ctx, host, b.opt.Port, b.opt.Targets)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return ctxErr("browse", ctx.Err())
		}
		b.sendErr("", net.JoinHostPort(req.Host, strconv.Itoa(b.opt.Port)), err)
		return nil
	}
	addressed := host != "" || len(b.opt.Targets) > 0
	return b.ssrpRound(ctx, msg, targets, addressed, req.Host != "")
}

// ssrpRound sends msg Sends times and collects answers until ctx ends
// (stopAll: as soon as every target answered).
func (b *browseRun) ssrpRound(ctx context.Context, msg []byte, targets []*net.UDPAddr, addressed, stopAll bool) error {
	var socks []*ssrpSock
	var lc net.ListenConfig
	if !addressed {
		for _, ip := range interfaceIPv4s() {
			local := ip.IP.String()
			pc, err := lc.ListenPacket(ctx, "udp4", net.JoinHostPort(local, "0"))
			if err != nil {
				b.sendErr(local, "bind", err)
				continue
			}
			s := &ssrpSock{pc: pc, local: local, targets: []*net.UDPAddr{{IP: net.IPv4bcast, Port: b.opt.Port}}}
			if bc := broadcastAddr(ip); bc != nil {
				s.targets = append(s.targets, &net.UDPAddr{IP: bc, Port: b.opt.Port})
			}
			socks = append(socks, s)
		}
	}
	pc, err := lc.ListenPacket(ctx, "udp4", ":0") // fallback / addressed socket
	if err != nil {
		b.sendErr("", "bind", err)
	} else {
		t := targets
		if !addressed {
			t = []*net.UDPAddr{{IP: net.IPv4bcast, Port: b.opt.Port}}
		}
		socks = append(socks, &ssrpSock{pc: pc, targets: t})
	}
	if len(socks) == 0 {
		return nil
	}
	want := map[string]bool{}
	if addressed {
		for _, t := range targets {
			want[(&net.UDPAddr{IP: t.IP.To4(), Port: t.Port}).String()] = true
		}
	}
	var once sync.Once
	closeAll := func() {
		once.Do(func() {
			for _, s := range socks {
				s.pc.Close()
			}
		})
	}
	defer closeAll()
	rctx, rcancel := context.WithCancel(ctx)
	defer rcancel()
	go func() { <-rctx.Done(); closeAll() }()
	pkts := make(chan ssrpPacket, 64)
	var wg sync.WaitGroup
	for _, s := range socks {
		wg.Add(1)
		go func(pc net.PacketConn) {
			defer wg.Done()
			buf := make([]byte, ssrpMaxResp+1)
			for {
				n, from, err := pc.ReadFrom(buf)
				if err != nil {
					return
				}
				select {
				case pkts <- ssrpPacket{append([]byte(nil), buf[:n]...), from}:
				case <-rctx.Done():
					return
				}
			}
		}(s.pc)
	}
	defer wg.Wait()
	defer rcancel()
	total := b.opt.Sends
	responders := map[string]bool{}
	dups := map[string]bool{}
	send := func(round int) {
		for _, s := range socks {
			for _, t := range s.targets {
				if addressed && responders[(&net.UDPAddr{IP: t.IP.To4(), Port: t.Port}).String()] {
					continue // answered: no retry
				}
				var err error
				if b.opt.sendFault != nil {
					err = b.opt.sendFault(t.String())
				}
				if err == nil {
					_, err = s.pc.WriteTo(msg, t)
				}
				if err != nil {
					b.sendErr(s.local, t.String(), err)
				} else {
					b.mu.Lock()
					b.res.Sent++
					b.mu.Unlock()
				}
			}
		}
		b.progress("ssrp", round, total)
	}
	send(1)
	round := 1
	tick := time.NewTicker(b.opt.SendInterval)
	defer tick.Stop()
	for n := 0; n < ssrpMaxDatagrams; {
		select {
		case <-ctx.Done():
			b.finishSSRP(responders, want, stopAll)
			return nil
		case <-tick.C:
			if round < total {
				round++
				send(round)
			}
		case p := <-pkts:
			n++
			addr, src := "", ""
			if ua, ok := p.from.(*net.UDPAddr); ok {
				addr, src = ua.IP.String(), (&net.UDPAddr{IP: ua.IP.To4(), Port: ua.Port}).String()
			}
			if k := src + "\x00" + string(p.data); dups[k] {
				continue // the same answer to a repeated request
			} else if len(dups) < ssrpMaxDatagrams {
				dups[k] = true
			}
			if addressed && !want[src] {
				b.res.Ignored++
				continue
			}
			insts, ok := parseSSRP(p.data)
			if !ok {
				b.res.Malformed++
				continue
			}
			responders[src] = true
			for _, in := range insts {
				in.Address, in.Source = addr, "browser"
				b.add(in)
			}
			b.progress("ssrp", round, total)
			if stopAll && len(responders) >= len(want) {
				b.finishSSRP(responders, want, stopAll)
				return nil
			}
		}
	}
	b.finishSSRP(responders, want, stopAll)
	return nil
}

func (b *browseRun) finishSSRP(responders, want map[string]bool, stopAll bool) {
	b.res.Responders += len(responders)
	b.res.Responded += len(responders)
	if stopAll && len(responders) < len(want) {
		b.res.Partial = true
	}
}

// interfaceIPv4s returns the IPv4 networks of the up, non-loopback
// interfaces (at most 8).
func interfaceIPv4s() []*net.IPNet {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []*net.IPNet
	for _, it := range ifs {
		if it.Flags&net.FlagUp == 0 || it.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := it.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && !n.IP.IsLoopback() && len(out) < 8 {
				out = append(out, &net.IPNet{IP: n.IP.To4(), Mask: n.Mask})
			}
		}
	}
	return out
}

// browseTargets returns the UDP destinations.
func browseTargets(ctx context.Context, host string, port int, override []string) ([]*net.UDPAddr, error) {
	var out []*net.UDPAddr
	if len(override) > 0 {
		for _, t := range override {
			a, err := net.ResolveUDPAddr("udp4", t)
			if err != nil {
				return nil, err
			}
			out = append(out, a)
		}
		return out, nil
	}
	if host != "" {
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			out = append(out, &net.UDPAddr{IP: ip, Port: port})
		}
		if len(out) == 0 {
			return nil, errors.New("no IPv4 address for the host")
		}
		return out, nil
	}
	out = append(out, &net.UDPAddr{IP: net.IPv4bcast, Port: port})
	if addrs, err := net.InterfaceAddrs(); err == nil {
		seen := map[string]bool{}
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok {
				if b := broadcastAddr(n); b != nil && !seen[b.String()] {
					seen[b.String()] = true
					out = append(out, &net.UDPAddr{IP: b, Port: port})
				}
			}
		}
	}
	return out, nil
}

// broadcastAddr is the directed broadcast address of an IPv4 network
// (nil: IPv6, loopback, link-local, /31, /32).
func broadcastAddr(n *net.IPNet) net.IP {
	ip := n.IP.To4()
	if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return nil
	}
	mask := n.Mask
	if len(mask) == net.IPv6len {
		mask = mask[12:]
	}
	if len(mask) != net.IPv4len {
		return nil
	}
	if ones, _ := net.IPMask(mask).Size(); ones == 0 || ones >= 31 {
		return nil
	}
	b := make(net.IP, 4)
	for i := range b {
		b[i] = ip[i] | ^mask[i]
	}
	return b
}

// parseSSRP parses an SVR_RESP datagram: 0x05, RESP_SIZE (uint16 LE),
// RESP_DATA: "ServerName;X;InstanceName;Y;IsClustered;No;Version;V;tcp;1433;np;\\X\pipe\...;;"
// (instances end with ";;"). Truncated / malformed datagrams give false;
// records without ServerName or InstanceName are skipped.
func parseSSRP(b []byte) ([]model.SQLServerInstance, bool) {
	if len(b) < 3 || b[0] != ssrpSvrResp {
		return nil, false
	}
	size := int(binary.LittleEndian.Uint16(b[1:3]))
	if size == 0 || len(b) < 3+size {
		return nil, false
	}
	data := ssrpDecode(b[3 : 3+size])
	var out []model.SQLServerInstance
	for _, rec := range strings.Split(data, ";;") {
		if rec == "" {
			continue
		}
		parts := strings.Split(rec, ";")
		var in model.SQLServerInstance
		for i := 0; i+1 < len(parts); i += 2 {
			v := ssrpValue(parts[i+1])
			switch strings.ToLower(strings.TrimSpace(parts[i])) {
			case "servername":
				in.Host = v
			case "instancename":
				in.Instance = v
			case "version":
				in.Version = v
			case "isclustered":
				in.IsClustered = strings.EqualFold(v, "yes")
			case "tcp":
				if p, err := strconv.Atoi(v); err == nil && p > 0 && p <= 65535 {
					in.TCPPort = p
				}
			case "np":
				in.NamedPipe = v
			}
		}
		if in.Host == "" || in.Instance == "" {
			continue
		}
		out = append(out, in)
	}
	return out, true
}

// ssrpValue makes a received value safe: valid UTF-8, no control
// characters, at most ssrpMaxValue bytes.
func ssrpValue(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	for len(s) > ssrpMaxValue {
		s = strings.ToValidUTF8(s[:ssrpMaxValue], "")
	}
	return s
}
