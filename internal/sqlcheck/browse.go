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

// Phase 9 (PLAN §12 p. 96, CHK-SQL-06): SQL Server discovery through the
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
	// Targets overrides the destinations ("ip:port"), for tests.
	Targets []string
	// MaxInstances caps the result (0 = model.MaxSQLBrowseInstances).
	MaxInstances int
}

// ListServers is Browse with default options (EC Tray naming).
func ListServers(ctx context.Context, req model.SQLBrowseRequest) (model.SQLBrowseResult, error) {
	return Browse(ctx, req, BrowseOptions{})
}

// Browse asks the SQL Server Browser for instances: for req.Host empty a
// broadcast CLNT_BCAST_EX to 255.255.255.255 and the directed broadcast of
// every IPv4 interface; otherwise CLNT_UCAST_EX (or CLNT_UCAST_INST for
// req.Instance) to the host's IPv4 addresses. Answers are collected until
// req.Timeout() (unicast: until every address answered), deduplicated by
// server name + instance, sorted and capped (Truncated). No answers is not
// an error (Browser stopped, firewall, UDP 1434 blocked). ctx cancel /
// expiry: *Error KindCanceled / KindTimeout.
func Browse(ctx context.Context, req model.SQLBrowseRequest, opt BrowseOptions) (model.SQLBrowseResult, error) {
	var res model.SQLBrowseResult
	if err := req.Validate(); err != nil {
		return res, &Error{Kind: KindConfig, Op: "browse", Err: err}
	}
	if err := ctx.Err(); err != nil {
		return res, ctxErr("browse", err)
	}
	port := opt.Port
	if port <= 0 || port > 65535 {
		port = SQLBrowserPort
	}
	max := opt.MaxInstances
	if req.Limit > 0 && (max <= 0 || req.Limit < max) {
		max = req.Limit
	}
	if max <= 0 || max > model.MaxSQLBrowseInstances {
		max = model.MaxSQLBrowseInstances
	}
	timeout := time.Duration(req.Timeout()) * time.Millisecond
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	msg := []byte{ssrpClntBcastEx}
	if req.Host != "" {
		msg = []byte{ssrpClntUcastEx}
		if req.Instance != "" {
			msg = append(append([]byte{ssrpClntUcastIns}, req.Instance...), 0)
		}
	}
	targets, err := browseTargets(ctx, req.Host, port, opt.Targets)
	if err != nil {
		if ctx.Err() != nil && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return res, ctxErr("browse", ctx.Err())
		}
		return res, &Error{Kind: KindConn, Op: "browse", Err: err}
	}
	var lc net.ListenConfig
	pc, err := lc.ListenPacket(ctx, "udp4", ":0")
	if err != nil {
		return res, &Error{Kind: KindConn, Op: "browse", Err: err}
	}
	var once sync.Once
	closeConn := func() { once.Do(func() { pc.Close() }) }
	defer closeConn()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			closeConn()
		case <-stop:
		}
	}()
	sent := 0
	want := map[string]bool{}
	for _, t := range targets {
		if _, err := pc.WriteTo(msg, t); err == nil {
			sent++
			want[t.IP.String()] = true
		}
	}
	if sent == 0 {
		return res, &Error{Kind: KindConn, Op: "browse", Err: errors.New("no SQL Browser request could be sent")}
	}
	unicast := req.Host != "" && len(opt.Targets) == 0
	dl, _ := ctx.Deadline()
	_ = pc.SetReadDeadline(dl)
	buf := make([]byte, ssrpMaxResp+1)
	seen := map[string]model.SQLServerInstance{}
	responders := map[string]bool{}
	for n := 0; n < ssrpMaxDatagrams; n++ {
		k, from, err := pc.ReadFrom(buf)
		if err != nil {
			break // deadline, ctx (conn closed) or socket error
		}
		addr := ""
		if ua, ok := from.(*net.UDPAddr); ok {
			addr = ua.IP.String()
		}
		insts, ok := parseSSRP(buf[:k])
		if !ok {
			res.Malformed++
			continue
		}
		responders[addr] = true
		for _, in := range insts {
			in.Address = addr
			key := strings.ToUpper(in.Host) + "\x00" + strings.ToUpper(in.Instance)
			if _, dup := seen[key]; !dup && len(seen) < ssrpMaxDatagrams {
				seen[key] = in
			}
		}
		if unicast && len(responders) >= len(want) {
			break
		}
	}
	if err := ctx.Err(); errors.Is(err, context.Canceled) {
		return res, ctxErr("browse", err)
	}
	for _, in := range seen {
		res.Instances = append(res.Instances, in)
	}
	sort.Slice(res.Instances, func(i, j int) bool {
		a, b := res.Instances[i], res.Instances[j]
		if !strings.EqualFold(a.Host, b.Host) {
			return strings.ToUpper(a.Host) < strings.ToUpper(b.Host)
		}
		return strings.ToUpper(a.Instance) < strings.ToUpper(b.Instance)
	})
	res.Instances, res.Truncated = model.CapSQLList(res.Instances, max)
	if res.Instances == nil {
		res.Instances = []model.SQLServerInstance{}
	}
	res.Responders = len(responders)
	res.Partial = res.Truncated || res.Malformed > 0 || unicast && len(responders) < len(want)
	return res, nil
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
	data := string(b[3 : 3+size])
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
