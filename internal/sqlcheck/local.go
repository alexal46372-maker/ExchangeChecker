package sqlcheck

import (
	"context"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Phase 11 (PLAN §12 p. 161, CHK-SQL-11): SQL Server instances of this
// computer. Windows: registry (HKLM\SOFTWARE\Microsoft\Microsoft SQL
// Server, 64- and 32-bit views: InstalledInstances, Instance Names\SQL,
// <id>\MSSQLServer\SuperSocketNetLib\Tcp\IPAll TcpPort / TcpDynamicPorts,
// <id>\Setup Version) and the SQLBrowser service state (SCM, read-only:
// SC_MANAGER_CONNECT + SERVICE_QUERY_STATUS); everywhere: SSRP unicast to
// 127.0.0.1:1434. Off Windows the registry part is unsupported.

// sqlServerKey is the registry key of SQL Server.
const sqlServerKey = `SOFTWARE\Microsoft\Microsoft SQL Server`

// localSource reads the registry / SCM (fake in tests).
type localSource interface {
	supported() bool
	// multiString reads a REG_MULTI_SZ value (view: 64 or 32).
	multiString(view int, path, name string) ([]string, error)
	// str reads a REG_SZ value; ok = false when missing.
	str(view int, path, name string) (string, bool, error)
	// browserState returns the SQLBrowser service state.
	browserState() string
}

// localInstances reads the installed instances from src (both views,
// deduplicated by name; the 64-bit view wins).
func localInstances(src localSource) (names []string, insts []model.SQLServerInstance, firstErr error) {
	host, _ := os.Hostname()
	host = strings.ToUpper(host)
	seen := map[string]bool{}
	for _, view := range []int{64, 32} {
		list, err := src.multiString(view, sqlServerKey, "InstalledInstances")
		if err != nil && firstErr == nil {
			firstErr = err
		}
		for _, name := range list {
			name = strings.TrimSpace(name)
			if name == "" || seen[strings.ToUpper(name)] || len(insts) >= model.MaxSQLBrowseInstances {
				continue
			}
			seen[strings.ToUpper(name)] = true
			names = append(names, name)
			in := model.SQLServerInstance{Host: host, Address: "127.0.0.1", Instance: name, Source: "local"}
			if id, ok, _ := src.str(view, sqlServerKey+`\Instance Names\SQL`, name); ok && id != "" && !strings.ContainsAny(id, `\/`) {
				ipall := sqlServerKey + `\` + id + `\MSSQLServer\SuperSocketNetLib\Tcp\IPAll`
				for _, v := range []string{"TcpPort", "TcpDynamicPorts"} {
					if p, ok, _ := src.str(view, ipall, v); ok && in.TCPPort == 0 {
						in.TCPPort = firstPort(p)
					}
				}
				if v, ok, _ := src.str(view, sqlServerKey+`\`+id+`\Setup`, "Version"); ok {
					in.Version = ssrpValue(v)
				}
			}
			insts = append(insts, in)
		}
	}
	sort.Strings(names)
	return names, insts, firstErr
}

// firstPort parses "1433" / "1433,1434" / " 49172 " (0: none / invalid).
func firstPort(s string) int {
	s = strings.TrimSpace(strings.SplitN(s, ",", 2)[0])
	p, err := strconv.Atoi(s)
	if err != nil || p <= 0 || p > 65535 {
		return 0
	}
	return p
}

// localPhase fills res.Local and asks 127.0.0.1:1434; withSSRP = false
// (broadcast mode) skips the unicast, the broadcast already reached it.
func (b *browseRun) localPhase(ctx context.Context, withSSRP bool) error {
	info := &model.SQLBrowseLocalInfo{Supported: b.opt.local.supported(), Installed: []string{}, BrowserState: model.SQLBrowserUnknown}
	b.res.Local = info
	b.progress("local", 0, 2)
	if info.Supported {
		names, insts, err := localInstances(b.opt.local)
		info.Installed = append(info.Installed, names...)
		if err != nil {
			info.Error = err.Error()
		}
		for _, in := range insts {
			b.add(in)
		}
		info.BrowserState = b.opt.local.browserState()
	}
	b.progress("local", 1, 2)
	if !withSSRP {
		return nil
	}
	before := b.res.Responded
	targets := []*net.UDPAddr{{IP: net.IPv4(127, 0, 0, 1), Port: b.opt.Port}}
	if len(b.opt.Targets) > 0 {
		var err error
		if targets, err = browseTargets(ctx, "", b.opt.Port, b.opt.Targets); err != nil {
			b.sendErr("", "targets", err)
			return nil
		}
	}
	if err := b.ssrpRound(ctx, []byte{ssrpClntUcastEx}, targets, true, true); err != nil {
		return err
	}
	info.BrowserAnswered = b.res.Responded > before
	b.progress("local", 2, 2)
	return nil
}
