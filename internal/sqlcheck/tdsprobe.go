package sqlcheck

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Phase 11 (PLAN §12 p. 162, CHK-SQL-12): TCP probe with a TDS PRELOGIN
// ([MS-TDS] 2.2.6.5) that reads the server version without logging in:
// the host's port (host mode) and every address of one /24 (subnet mode,
// 32 in parallel, 300 ms each, ctx-cancellable).

// preloginInfo is the parsed PRELOGIN answer.
type preloginInfo struct {
	Version    string // "10.50.1600"
	Encryption string // model.SQLEnc*
}

// Encryption values of the PRELOGIN answer.
const (
	SQLEncOff          = "off"
	SQLEncOn           = "on"
	SQLEncNotSupported = "not_supported"
	SQLEncRequired     = "required"
)

// preloginPacket builds a client PRELOGIN (encryption byte enc).
func preloginPacket(enc byte) []byte {
	// tokens: VERSION(6) ENCRYPTION(1) INSTOPT(1) THREADID(4) MARS(1)
	type tok struct {
		id   byte
		data []byte
	}
	toks := []tok{
		{0, []byte{0, 0, 0, 0, 0, 0}},
		{1, []byte{enc}},
		{2, []byte{0}},
		{3, []byte{0, 0, 0, 0}},
		{4, []byte{0}},
	}
	off := len(toks)*5 + 1
	var hdr, body []byte
	for _, t := range toks {
		hdr = append(hdr, t.id, byte(off>>8), byte(off), 0, byte(len(t.data)))
		body = append(body, t.data...)
		off += len(t.data)
	}
	payload := append(append(hdr, 0xFF), body...)
	pkt := []byte{0x12, 0x01, 0, 0, 0, 0, 1, 0}
	binary.BigEndian.PutUint16(pkt[2:], uint16(8+len(payload)))
	return append(pkt, payload...)
}

// parsePrelogin parses a server PRELOGIN payload (after the 8-byte header).
func parsePrelogin(p []byte) (preloginInfo, error) {
	var out preloginInfo
	for i := 0; ; i += 5 {
		if i >= len(p) {
			return out, errors.New("prelogin: no terminator")
		}
		if p[i] == 0xFF {
			break
		}
		if i+5 > len(p) {
			return out, errors.New("prelogin: short option")
		}
		off, n := int(binary.BigEndian.Uint16(p[i+1:])), int(binary.BigEndian.Uint16(p[i+3:]))
		if off+n > len(p) {
			return out, errors.New("prelogin: option out of range")
		}
		d := p[off : off+n]
		switch p[i] {
		case 0:
			if n >= 4 {
				out.Version = fmt.Sprintf("%d.%d.%d", d[0], d[1], binary.BigEndian.Uint16(d[2:]))
			}
		case 1:
			if n >= 1 {
				switch d[0] {
				case 0:
					out.Encryption = SQLEncOff
				case 1:
					out.Encryption = SQLEncOn
				case 2:
					out.Encryption = SQLEncNotSupported
				case 3:
					out.Encryption = SQLEncRequired
				}
			}
		}
	}
	if out.Version == "" {
		return out, errors.New("prelogin: no version")
	}
	return out, nil
}

// errNotTDS: the port answered, but not with TDS.
var errNotTDS = errors.New("not a TDS server")

// prelogin dials addr and runs the PRELOGIN exchange within timeout.
// open reports whether the TCP connection was established.
func prelogin(ctx context.Context, dial func(context.Context, string) (net.Conn, error), addr string, enc byte, timeout time.Duration) (info preloginInfo, open bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	c, err := dial(ctx, addr)
	if err != nil {
		return info, false, err
	}
	defer c.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			c.Close()
		case <-done:
		}
	}()
	if dl, ok := ctx.Deadline(); ok {
		_ = c.SetDeadline(dl)
	}
	if _, err = c.Write(preloginPacket(enc)); err != nil {
		return info, true, err
	}
	var hdr [8]byte
	if _, err = io.ReadFull(c, hdr[:]); err != nil {
		return info, true, err
	}
	n := int(binary.BigEndian.Uint16(hdr[2:]))
	if hdr[0] != 0x04 || n < 9 || n > 4096 {
		return info, true, errNotTDS
	}
	body := make([]byte, n-8)
	if _, err = io.ReadFull(c, body); err != nil {
		return info, true, err
	}
	info, err = parsePrelogin(body)
	if err != nil {
		return info, true, errNotTDS
	}
	return info, true, nil
}

// probeHost probes host:port (host mode).
func (b *browseRun) probeHost(ctx context.Context, host string, port int) {
	tp := b.res.TCPProbe // set by Browse before the goroutine starts
	b.progress("host", 0, 1)
	addrs, err := lookupIPv4(ctx, host)
	if err == nil && len(addrs) > 0 {
		tp.Probed = 1
		addr := addrs[0].String()
		info, open, perr := prelogin(ctx, b.opt.dial, net.JoinHostPort(addr, strconv.Itoa(port)), 0x02, 4*b.opt.ProbeTimeout)
		b.mu.Lock()
		if open {
			tp.Open = 1
		}
		if perr == nil {
			tp.Prelogin = 1
		}
		b.mu.Unlock()
		if perr == nil {
			b.add(model.SQLServerInstance{Host: host, Address: addr, TCPPort: port, Version: info.Version, Source: "tcp"})
		}
	}
	b.progress("host", 1, 1)
}

// lookupIPv4 resolves host to IPv4 addresses (literal: itself).
func lookupIPv4(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() == nil {
			return nil, errors.New("IPv6 is not supported")
		}
		return []net.IP{ip.To4()}, nil
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
	if err != nil {
		return nil, err
	}
	if len(ips) > 8 {
		ips = ips[:8]
	}
	return ips, nil
}

// subnetHosts returns the addresses of cidr (empty: the first
// interface's /24) and the other local /24s. A CIDR must overlap a local
// interface network (no scans of foreign networks); a /24../30 (checked by
// the model) has at most 254 hosts.
func subnetHosts(cidr string, ifs []*net.IPNet) (hosts []string, used string, others []string, narrowed bool, err error) {
	var locals []*net.IPNet
	for _, n := range ifs {
		_, l, _ := net.ParseCIDR(n.IP.String() + "/24")
		dup := false
		for _, x := range locals {
			dup = dup || x.String() == l.String()
		}
		if !dup {
			locals = append(locals, l)
		}
	}
	var nw *net.IPNet
	if cidr == "" {
		if len(locals) == 0 {
			return nil, "", nil, false, errors.New("no IPv4 interface")
		}
		nw = locals[0]
	} else {
		_, nw, err = net.ParseCIDR(cidr)
		if err != nil || nw.IP.To4() == nil {
			return nil, "", nil, false, errors.New("bad CIDR")
		}
		ok := false
		for _, n := range ifs {
			ok = ok || nw.Contains(n.IP) || n.Contains(nw.IP)
		}
		if !ok {
			return nil, "", nil, false, errNotLocalSubnet
		}
	}
	// narrowed: the interface the scanned network belongs to is wider
	// than /24 (not just ifs[0])
	for _, n := range ifs {
		if nw.Contains(n.IP) || n.Contains(nw.IP) {
			ones, bits := n.Mask.Size()
			narrowed = bits == 32 && ones < 24
			break
		}
	}
	for _, l := range locals {
		if l.String() != nw.String() {
			others = append(others, l.String())
		}
	}
	ones, _ := nw.Mask.Size()
	base := binary.BigEndian.Uint32(nw.IP.To4())
	size := uint32(1) << (32 - ones)
	for i := uint32(1); i+1 < size && len(hosts) < model.MaxSQLSubnetHosts; i++ {
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, base+i)
		hosts = append(hosts, ip.String())
	}
	return hosts, nw.String(), others, narrowed, nil
}

var errNotLocalSubnet = errors.New("subnet is not a local network")

// subnet probes port on every address of the subnet.
func (b *browseRun) subnet(ctx context.Context, cidr string, port int) error {
	tp := &model.SQLBrowseTCPInfo{Port: port, CIDR: cidr, Subnets: []string{}}
	b.res.TCPProbe = tp
	hosts := b.opt.SubnetHosts
	if hosts == nil {
		ifs := interfaceIPv4s
		if b.opt.ifaces != nil {
			ifs = b.opt.ifaces
		}
		h, used, others, narrowed, err := subnetHosts(cidr, ifs())
		if errors.Is(err, errNotLocalSubnet) {
			return &Error{Kind: KindConfig, Op: "browse", Err: &model.FieldError{Field: "cidr", Key: model.ValSQLBrowseCIDR, Msg: "cidr: not a network of this computer"}}
		}
		if err != nil {
			b.sendErr("", "subnet", err)
			return nil
		}
		hosts, tp.CIDR, tp.Narrowed = h, used, narrowed
		tp.Subnets = append(tp.Subnets, others...)
	}
	if len(hosts) > model.MaxSQLSubnetHosts {
		hosts, tp.Truncated = hosts[:model.MaxSQLSubnetHosts], true
	}
	total := len(hosts)
	b.progress("subnet", 0, total)
	jobs := make(chan string)
	var wg sync.WaitGroup
	var mu sync.Mutex
	done := 0
	for w := 0; w < b.opt.Concurrency && w < total; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for h := range jobs {
				info, open, err := prelogin(ctx, b.opt.dial, net.JoinHostPort(h, strconv.Itoa(port)), 0x02, b.opt.ProbeTimeout)
				if err == nil {
					b.add(model.SQLServerInstance{Host: h, Address: h, TCPPort: port, Version: info.Version, Source: "tcp"})
				}
				mu.Lock()
				done++
				tp.Probed++
				if open {
					tp.Open++
				}
				if err == nil {
					tp.Prelogin++
				}
				d := done
				b.progress("subnet", d, total) // under mu: serialized callback
				mu.Unlock()
			}
		}()
	}
feed:
	for _, h := range hosts {
		select {
		case jobs <- h:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	if tp.Probed < total {
		b.res.Partial = true
	}
	return nil
}
