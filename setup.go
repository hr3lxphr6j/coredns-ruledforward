package ruledforward

import (
	"crypto/tls"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	"github.com/coredns/coredns/plugin/pkg/parse"
	"github.com/coredns/coredns/plugin/pkg/proxy"
	pkgtls "github.com/coredns/coredns/plugin/pkg/tls"
	"github.com/coredns/coredns/plugin/pkg/transport"
	"github.com/hashicorp/cronexpr"

	"github.com/miekg/dns"
)

var (
	log = clog.NewWithPlugin("ruledforward")

	dlcMap map[string][]Rule
)

const (
	hcInterval     = 500 * time.Millisecond
	defaultExpire  = 10 * time.Second
	maxProxies     = 15
	bloomFP        = 0.01
	adguardTimeout = 30 * time.Second
)

func init() {
	plugin.Register("ruledforward", setup)
}

func setup(c *caddy.Controller) error {
	r, err := parseRuledforward(c)
	if err != nil {
		return plugin.Error("ruledforward", err)
	}

	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		r.Next = next
		return r
	})

	c.OnStartup(r.OnStartup)
	c.OnShutdown(r.OnShutdown)

	return nil
}

func parseRuledforward(c *caddy.Controller) (*Ruledforward, error) {
	r := &Ruledforward{from: "."}
	var dlcfile string

	if !c.Next() {
		return r, c.ArgErr()
	}
	args := c.RemainingArgs()
	if len(args) > 0 {
		zones := plugin.Host(args[0]).NormalizeExact()
		if len(zones) == 0 {
			return r, fmt.Errorf("unable to normalize zone '%s'", args[0])
		}
		r.from = zones[0]
	}

	for c.NextBlock() {
		switch c.Val() {
		case "dlcfile":
			if !c.NextArg() {
				return r, c.ArgErr()
			}
			dlcfile = c.Val()
			if dlcfile != "" && filepath.IsAbs(dlcfile) == false && dnsserver.GetConfig(c).Root != "" {
				dlcfile = filepath.Join(dnsserver.GetConfig(c).Root, dlcfile)
			}
		case "group":
			// Get group name
			if !c.NextArg() {
				return r, c.ArgErr()
			}
			groupName := c.Val()
			gb := &groupBuild{
				Action:   "forward",
				maxfails: 2,
				expire:   defaultExpire,
				opts:     proxy.Options{HCRecursionDesired: true, HCDomain: "."},
			}
			gb.Name = groupName
			// Parse group block contents
			// The outer loop's NextBlock() has already positioned us inside the group block
			// We need to parse directives until we hit the closing brace
			for c.Next() && c.Val() != "}" {
				if err := parseGroupDirective(c, gb); err != nil {
					return r, err
				}
			}
			// Build the group
			g, err := buildGroup(gb)
			if err != nil {
				return r, err
			}
			r.groups = append(r.groups, g)
		default:
			return r, c.Errf("unknown directive '%s'", c.Val())
		}
	}

	if dlcfile != "" {
		var err error
		dlcMap, err = LoadDLC(dlcfile)
		if err != nil {
			return r, fmt.Errorf("loading dlcfile %s: %w", dlcfile, err)
		}
	}

	for _, g := range r.groups {
		if err := g.Update(dlcMap); err != nil {
			return r, fmt.Errorf("updating group %s: %w", g.Name, err)
		}
	}

	// Validate that there is at most one default group and cache reference
	defaultCount := 0
	for _, g := range r.groups {
		if g.Name == "default" {
			defaultCount++
			r.defaultGroup = g
		}
	}
	if defaultCount > 1 {
		return r, fmt.Errorf("at most one 'default' group is allowed, found %d", defaultCount)
	}

	return r, nil
}

// groupBuild holds raw config for a group until we build it.
type groupBuild struct {
	Name           string
	Action         string
	geositeNames   []string
	inlineRules    []Rule
	adguardRules   []Rule
	adguardPaths   []string
	adguardURLs    []string
	bootstrapDNS   string
	refreshCron    string
	toHosts        []string
	policy         string
	maxfails       uint32
	expire         time.Duration
	tlsConfig      *tls.Config
	tlsServerName  string
	opts           proxy.Options
}

func parseGroupDirective(c *caddy.Controller, gb *groupBuild) error {
	switch c.Val() {
	case "action":
		if !c.NextArg() {
			return c.ArgErr()
		}
		gb.Action = strings.ToLower(c.Val())
		if gb.Action != "forward" && gb.Action != "empty" {
			return c.Errf("action must be 'forward' or 'empty'")
		}
	case "geosite":
		gb.geositeNames = c.RemainingArgs()
		if len(gb.geositeNames) == 0 {
			return c.ArgErr()
		}
	case "adguard_rules":
		paths := c.RemainingArgs()
		if len(paths) == 0 {
			return c.ArgErr()
		}
		for _, p := range paths {
			if IsURL(p) {
				gb.adguardURLs = append(gb.adguardURLs, p)
			} else {
				gb.adguardPaths = append(gb.adguardPaths, p)
			}
		}
	case "bootstrap_dns":
		if !c.NextArg() {
			return c.ArgErr()
		}
		gb.bootstrapDNS = c.Val()
	case "refresh":
		if !c.NextArg() {
			return c.ArgErr()
		}
		gb.refreshCron = c.Val()
		if _, err := cronexpr.Parse(gb.refreshCron); err != nil {
			return c.Errf("invalid refresh cron: %v", err)
		}
	case "to":
		gb.toHosts = c.RemainingArgs()
		if len(gb.toHosts) == 0 {
			return c.ArgErr()
		}
	case "policy":
		if !c.NextArg() {
			return c.ArgErr()
		}
		gb.policy = strings.ToLower(c.Val())
	case "max_fails":
		if !c.NextArg() {
			return c.ArgErr()
		}
		n, err := strconv.ParseUint(c.Val(), 10, 32)
		if err != nil {
			return err
		}
		gb.maxfails = uint32(n)
	case "tls":
		args := c.RemainingArgs()
		config := dnsserver.GetConfig(c)
		for i := range args {
			if !filepath.IsAbs(args[i]) && config.Root != "" {
				args[i] = filepath.Join(config.Root, args[i])
			}
		}
		tlsConfig, err := pkgtls.NewTLSConfigFromArgs(args...)
		if err != nil {
			return err
		}
		gb.tlsConfig = tlsConfig
	case "tls_servername":
		if !c.NextArg() {
			return c.ArgErr()
		}
		gb.tlsServerName = c.Val()
	case "expire":
		if !c.NextArg() {
			return c.ArgErr()
		}
		dur, err := time.ParseDuration(c.Val())
		if err != nil {
			return err
		}
		gb.expire = dur
	case "force_tcp":
		gb.opts.ForceTCP = true
	case "prefer_udp":
		gb.opts.PreferUDP = true
	default:
		directive := c.Val()
		// Ignore block delimiters
		if directive == "{" || directive == "}" {
			return nil
		}
		if strings.HasPrefix(directive, "include:") {
			return c.Errf("include: is not supported in group rules")
		}
		rule, err := parseInlineRule(directive, c)
		if err != nil {
			return err
		}
		if rule != nil {
			gb.inlineRules = append(gb.inlineRules, *rule)
		}
	}
	return nil
}

func buildGroup(gb *groupBuild) (*Group, error) {
	if gb.Action == "empty" && len(gb.toHosts) > 0 {
		return nil, fmt.Errorf("group %s: action empty cannot have 'to'", gb.Name)
	}
	if gb.Action == "forward" && len(gb.toHosts) == 0 {
		return nil, fmt.Errorf("group %s: action forward requires 'to'", gb.Name)
	}

	g := &Group{
		Name:     gb.Name,
		Action:   gb.Action,
		Maxfails: gb.maxfails,
		Opts:     gb.opts,
	}

	if gb.Action == "forward" {
		toHosts, err := parse.HostPortOrFile(gb.toHosts...)
		if err != nil {
			return nil, err
		}
		if len(toHosts) > maxProxies {
			return nil, fmt.Errorf("group %s: more than %d upstreams: %d", gb.Name, maxProxies, len(toHosts))
		}
		allowedTrans := map[string]bool{"dns": true, "tls": true}
		for _, hostWithZone := range toHosts {
			trans, h := parse.Transport(hostWithZone)
			if !allowedTrans[trans] {
				return nil, fmt.Errorf("group %s: unsupported protocol %s", gb.Name, trans)
			}
			p := proxy.NewProxy("ruledforward", h, trans)
			if trans == transport.TLS {
				tcfg := gb.tlsConfig
				if tcfg == nil {
					tcfg = &tls.Config{}
				}
				if gb.tlsServerName != "" {
					tcfg = tcfg.Clone()
					tcfg.ServerName = gb.tlsServerName
				}
				p.SetTLSConfig(tcfg)
			}
			p.SetExpire(gb.expire)
			p.GetHealthchecker().SetRecursionDesired(gb.opts.HCRecursionDesired)
			p.GetHealthchecker().SetDomain(gb.opts.HCDomain)
			g.Proxies = append(g.Proxies, p)
		}
		switch gb.policy {
		case "random":
			g.Policy = &random{}
		case "round_robin":
			g.Policy = &roundRobin{}
		case "sequential", "":
			g.Policy = &sequential{}
		default:
			return nil, fmt.Errorf("unknown policy '%s'", gb.policy)
		}
	}

	g.GeositeNames = gb.geositeNames
	g.InlineRules = gb.inlineRules
	g.AdguardPaths = gb.adguardPaths
	g.AdguardURLs = gb.adguardURLs
	g.BootstrapDNS = gb.bootstrapDNS
	g.RefreshCron = gb.refreshCron

	return g, nil
}

func parseInlineRule(directive string, c *caddy.Controller) (*Rule, error) {
	lower := strings.ToLower(directive)
	if strings.HasPrefix(lower, "domain:") {
		val := strings.TrimSpace(directive[7:])
		if val == "" && c.NextArg() {
			val = c.Val()
		}
		if val == "" {
			return nil, c.ArgErr()
		}
		return &Rule{Type: RuleDomain, Value: strings.ToLower(dns.Fqdn(val))}, nil
	}
	if strings.HasPrefix(lower, "full:") {
		val := strings.TrimSpace(directive[5:])
		if val == "" && c.NextArg() {
			val = c.Val()
		}
		if val == "" {
			return nil, c.ArgErr()
		}
		return &Rule{Type: RuleFull, Value: strings.ToLower(dns.Fqdn(val))}, nil
	}
	if strings.HasPrefix(lower, "keyword:") {
		val := strings.TrimSpace(directive[8:])
		if val == "" && c.NextArg() {
			val = c.Val()
		}
		if val == "" {
			return nil, c.ArgErr()
		}
		return &Rule{Type: RuleKeyword, Value: strings.ToLower(val)}, nil
	}
	if strings.HasPrefix(lower, "regex:") {
		val := strings.TrimSpace(directive[6:])
		if val == "" && c.NextArg() {
			val = c.Val()
		}
		if val == "" {
			return nil, c.ArgErr()
		}
		return &Rule{Type: RuleRegex, Value: val}, nil
	}
	if _, ok := dns.IsDomainName(directive); ok && !strings.Contains(directive, " ") {
		return &Rule{Type: RuleDomain, Value: strings.ToLower(dns.Fqdn(directive))}, nil
	}
	return nil, nil
}

// OnStartup starts proxies and refresh goroutines.
func (r *Ruledforward) OnStartup() error {
	for _, g := range r.groups {
		for _, p := range g.Proxies {
			p.Start(hcInterval)
		}
		if g.RefreshCron != "" && len(g.AdguardURLs) > 0 {
			go r.runRefresh(g)
		}
	}
	return nil
}

// OnShutdown stops proxies and refresh goroutines.
func (r *Ruledforward) OnShutdown() error {
	for _, g := range r.groups {
		for _, p := range g.Proxies {
			p.Stop()
		}
		if g.StopRefresh != nil {
			close(g.StopRefresh)
		}
	}
	return nil
}

func (r *Ruledforward) runRefresh(g *Group) {
	expr, err := cronexpr.Parse(g.RefreshCron)
	if err != nil {
		return
	}
	g.StopRefresh = make(chan struct{})
	for {
		next := expr.Next(time.Now())
		timer := time.NewTimer(time.Until(next))
		select {
		case <-g.StopRefresh:
			timer.Stop()
			return
		case <-timer.C:
			if err := g.Update(dlcMap); err != nil {
				log.Errorf("refresh failed for group '%s': %v", g.Name, err)
			}
		}
	}
}
