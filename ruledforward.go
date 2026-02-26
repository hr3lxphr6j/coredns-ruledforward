package ruledforward

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/debug"
	"github.com/coredns/coredns/plugin/pkg/proxy"
	"github.com/coredns/coredns/request"

	"github.com/miekg/dns"
)

const (
	defaultTimeout = 5 * time.Second
	emptyTTL       = 60
)

var (
	errNoHealthy = errors.New("no healthy proxies")
)

// Ruledforward is a plugin that forwards or returns empty based on domain rules.
type Ruledforward struct {
	from         string
	groups       []*Group
	defaultGroup *Group // cached reference to default group if exists
	Next         plugin.Handler
}

// Group is one rule group: either forward to upstreams or return empty.
// Matcher is updated atomically (no lock in Matcher; holder uses atomic pointer swap).
type Group struct {
	Name    string
	Action  string // "forward" or "empty"
	matcher atomic.Pointer[Matcher]

	// forward-only
	Proxies  []*proxy.Proxy
	Policy   Policy
	Maxfails uint32
	Opts     proxy.Options

	// for refresh: static rules (inline + geosite) + URL list
	GeositeNames []string
	InlineRules  []Rule
	AdguardPaths []string
	AdguardURLs  []string
	BootstrapDNS string // optional; used to resolve adguard_rules URL host to avoid DNS loop
	RefreshCron  string
	StopRefresh  chan struct{}
}

// Matcher returns the current matcher (atomic load). Returns nil if not yet set.
func (g *Group) Matcher() Matcher {
	p := g.matcher.Load()
	if p == nil {
		return nil
	}
	return *p
}

// SetMatcher atomically stores the matcher. Used by Update (refresh) and tests.
func (g *Group) SetMatcher(m Matcher) {
	g.matcher.Store(&m)
}

const (
	UpdateMatcherGeosite byte = 1 << iota
	UpdateMatcherInlinee
	UpdateMatcherAdguardLocal
	UpdateMatcherAdguardRemote

	UpdateMatcherLocal = UpdateMatcherGeosite | UpdateMatcherInlinee | UpdateMatcherAdguardLocal
	UpdateMatcherAll   = UpdateMatcherLocal | UpdateMatcherAdguardRemote
)

func (g *Group) updateMatcher(dlcMap map[string][]Rule, updateItems byte) error {
	bm := NewBloomedMatcher(2<<13, bloomFP)

	if updateItems&UpdateMatcherGeosite != 0 {
		for _, listName := range g.GeositeNames {
			if dlcMap != nil {
				rules := dlcMap[strings.ToUpper(listName)]
				for _, rule := range rules {
					bm.AddRule(rule)
				}
			}
		}
	}

	if updateItems&UpdateMatcherInlinee != 0 {
		for _, rule := range g.InlineRules {
			bm.AddRule(rule)
		}
	}

	if updateItems&UpdateMatcherAdguardLocal != 0 {
		for _, path := range g.AdguardPaths {
			log.Infof("Load Adguard Rule path: %s", path)
			rules, err := LoadAdguardFromFile(path)
			if err != nil {
				return fmt.Errorf("group %s adguard_rules %s: %w", g.Name, path, err)
			}
			for _, rule := range rules {
				bm.AddRule(rule)
			}
		}
	}

	if updateItems&UpdateMatcherAdguardRemote != 0 {
		for _, url := range g.AdguardURLs {
			log.Infof("Load Adguard Rule URL: %s", url)
			rules, err := LoadAdguardFromURL(url, adguardTimeout, g.BootstrapDNS)
			if err != nil {
				return fmt.Errorf("group %s adguard_rules %s: %w", g.Name, url, err)
			}
			for _, rule := range rules {
				bm.AddRule(rule)
			}
		}
	}

	bm.Build()
	g.SetMatcher(bm)
	return nil
}

func (g *Group) Update(dlcMap map[string][]Rule, updateItems byte) error {
	if err := g.updateMatcher(dlcMap, updateItems); err != nil {
		return err
	}

	return nil
}

// Name implements plugin.Handler.
func (r *Ruledforward) Name() string { return "ruledforward" }

// ServeDNS implements plugin.Handler.
func (r *Ruledforward) ServeDNS(ctx context.Context, w dns.ResponseWriter, req *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: req}
	qname := state.Name()

	if r.from != "." && !plugin.Name(r.from).Matches(qname) {
		return plugin.NextOrFailure(r.Name(), r.Next, ctx, w, req)
	}

	for _, g := range r.groups {
		// Skip default group in normal iteration, it will be handled if no match found
		if g.Name == "default" {
			continue
		}
		if m := g.Matcher(); m == nil || !m.Match(qname) {
			continue
		}

		switch g.Action {
		case "empty":
			requestsTotal.WithLabelValues(g.Name, "empty").Inc()
			m := new(dns.Msg)
			m.SetReply(req)
			m.Ns = soaForEmpty(qname)
			_ = w.WriteMsg(m)
			return 0, nil
		case "forward":
			requestsTotal.WithLabelValues(g.Name, "forward").Inc()
			return r.forwardGroup(ctx, w, req, state, g)
		default:
			continue
		}
	}

	// If no group matched, use default group if it exists
	if r.defaultGroup != nil {
		switch r.defaultGroup.Action {
		case "empty":
			requestsTotal.WithLabelValues(r.defaultGroup.Name, "empty").Inc()
			m := new(dns.Msg)
			m.SetReply(req)
			m.Ns = soaForEmpty(qname)
			_ = w.WriteMsg(m)
			return 0, nil
		case "forward":
			requestsTotal.WithLabelValues(r.defaultGroup.Name, "forward").Inc()
			return r.forwardGroup(ctx, w, req, state, r.defaultGroup)
		}
	}

	noMatchTotal.Inc()
	return plugin.NextOrFailure(r.Name(), r.Next, ctx, w, req)
}

func soaForEmpty(origin string) []dns.RR {
	hdr := dns.RR_Header{Name: origin, Ttl: emptyTTL, Class: dns.ClassINET, Rrtype: dns.TypeSOA}
	return []dns.RR{&dns.SOA{Hdr: hdr, Ns: ".", Mbox: ".", Serial: 0, Refresh: 0, Retry: 0, Expire: 0, Minttl: emptyTTL}}
}

func (r *Ruledforward) forwardGroup(ctx context.Context, w dns.ResponseWriter, req *dns.Msg, state request.Request, g *Group) (int, error) {
	if len(g.Proxies) == 0 {
		return dns.RcodeServerFailure, errNoHealthy
	}
	list := g.Policy.List(g.Proxies)
	deadline := time.Now().Add(defaultTimeout)
	i := 0
	fails := 0
	var upstreamErr error

	for time.Now().Before(deadline) && ctx.Err() == nil {
		if i >= len(list) {
			i = 0
			fails = 0
		}
		pr := list[i]
		i++
		if pr.Down(g.Maxfails) {
			fails++
			if fails < len(g.Proxies) {
				continue
			}
			pr = list[0]
		}

		opts := g.Opts
		var ret *dns.Msg
		var err error
		for {
			ret, err = pr.Connect(ctx, state, opts)
			if errors.Is(err, proxy.ErrCachedClosed) {
				continue
			}
			if ret != nil && ret.Truncated && !opts.ForceTCP && opts.PreferUDP {
				opts.ForceTCP = true
				continue
			}
			break
		}
		upstreamErr = err

		if err != nil {
			if g.Maxfails != 0 {
				pr.Healthcheck()
			}
			if fails < len(g.Proxies) {
				continue
			}
			break
		}

		if !state.Match(ret) {
			debug.Hexdumpf(ret, "Wrong reply for id: %d, %s %d", ret.Id, state.QName(), state.QType())
			formerr := new(dns.Msg)
			formerr.SetRcode(state.Req, dns.RcodeFormatError)
			_ = w.WriteMsg(formerr)
			return 0, nil
		}

		_ = w.WriteMsg(ret)
		return 0, nil
	}

	forwardUpstreamFailTotal.WithLabelValues(g.Name).Inc()
	if upstreamErr != nil {
		return dns.RcodeServerFailure, upstreamErr
	}
	return dns.RcodeServerFailure, errNoHealthy
}
