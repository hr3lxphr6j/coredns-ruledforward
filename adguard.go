package ruledforward

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/miekg/dns"
)

// ParseAdguardRules parses AdGuard-style filter content and returns rules.
// Supports: domains-only, ||domain^ (suffix), /regex/, # and ! comments, @@ exceptions (skipped).
func ParseAdguardRules(body string) ([]Rule, error) {
	var rules []Rule
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		if strings.HasPrefix(line, "@@") {
			continue
		}
		// ||domain^ -> domain suffix
		if after, ok := strings.CutPrefix(line, "||"); ok {
			rest := after
			rest = strings.TrimSuffix(rest, "^")
			rest = strings.TrimSpace(rest)
			if rest != "" {
				rules = append(rules, Rule{Type: RuleDomain, Value: strings.ToLower(dns.Fqdn(rest))})
			}
			continue
		}
		// /regex/
		if len(line) >= 2 && line[0] == '/' && line[len(line)-1] == '/' {
			re := line[1 : len(line)-1]
			rules = append(rules, Rule{Type: RuleRegex, Value: re})
			continue
		}
		// hosts: IP domain -> use domain part
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			if isIP(parts[0]) {
				domain := strings.ToLower(dns.Fqdn(parts[1]))
				rules = append(rules, Rule{Type: RuleFull, Value: domain})
				continue
			}
		}
		// plain domain -> full (exact) per AdGuard
		if len(parts) == 1 {
			domain := strings.ToLower(dns.Fqdn(strings.TrimSpace(parts[0])))
			if domain != "." {
				rules = append(rules, Rule{Type: RuleFull, Value: domain})
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return rules, nil
}

func isIP(s string) bool {
	return strings.Contains(s, ".") || strings.Contains(s, ":")
}

// LoadAdguardFromFile reads a local file and parses as AdGuard rules.
func LoadAdguardFromFile(path string) ([]Rule, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	return ParseAdguardRules(string(data))
}

// transportWithBootstrapDNS returns an http.Transport that resolves hostnames via
// the given bootstrap DNS server to avoid circular dependency when this plugin is the system DNS.
func transportWithBootstrapDNS(bootstrapDNS string) *http.Transport {
	bootstrapAddr := bootstrapDNS
	if bootstrapAddr != "" && !strings.Contains(bootstrapAddr, ":") {
		bootstrapAddr = net.JoinHostPort(bootstrapAddr, "53")
	}
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: 10 * time.Second}
			return d.DialContext(ctx, "udp", bootstrapAddr)
		},
	}
	dialer := &net.Dialer{
		Resolver:  resolver,
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return &http.Transport{
		DialContext: dialer.DialContext,
	}
}

// LoadAdguardFromURL fetches URL and parses body as AdGuard rules.
// If bootstrapDNS is non-empty, the URL host is resolved via that DNS server to avoid
// circular dependency when this plugin is the system DNS.
func LoadAdguardFromURL(rawURL string, timeout time.Duration, bootstrapDNS string) ([]Rule, error) {
	var transport *http.Transport
	if bootstrapDNS != "" {
		transport = transportWithBootstrapDNS(bootstrapDNS)
	} else {
		transport = &http.Transport{}
	}
	client := &http.Client{Timeout: timeout, Transport: transport}
	resp, err := client.Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("adguard_rules URL %s: status %d", rawURL, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return ParseAdguardRules(string(data))
}

// IsURL returns true if s looks like http(s) URL.
func IsURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}
