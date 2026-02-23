// Package ruledforward implements rule-based forwarding with domain-list-community and AdGuard rules.
package ruledforward

import (
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/miekg/dns"
)

// domainTrieNode is a label-based trie for domain suffix matching (reference: v2fly/v2ray-core common/strmatcher DomainMatcherGroup).
// Labels are traversed right-to-left (TLD to subdomain). match is true when this node is the end of a rule domain.
type domainTrieNode struct {
	children map[string]*domainTrieNode
	match    bool
}

// RuleType is the type of domain rule.
type RuleType int

const (
	// RuleDomain matches qname as subdomain (or equal) of value.
	RuleDomain RuleType = iota
	// RuleFull matches qname exactly.
	RuleFull
	// RuleKeyword matches if value is substring of qname.
	RuleKeyword
	// RuleRegex matches qname against value as regex.
	RuleRegex
)

// Rule is a single matching rule.
type Rule struct {
	Type  RuleType
	Value string // normalized (lowercase, FQDN for domain/full)
}

type Matcher interface {
	AddRule(r Rule)
	Build()

	Match(qname string) bool
}

// matcher holds rules and provides Match(qname).
// matcher has no internal lock; the holder (Group) uses atomic.Pointer + Store/Load for concurrent safety.
// domainTrie is built in Build() from domain slice for O(qname labels) domain matching instead of O(rules).
type matcher struct {
	full       map[string]struct{}   // exact names
	domain     []string             // suffix rules, kept for keysForBloom
	domainTrie *domainTrieNode      // label trie for domain match (right-to-left)
	keyword    []string             // substring
	regex      []*regexp.Regexp     // compiled
}

// NewMatcher returns an empty matcher.
func NewMatcher() Matcher {
	return &matcher{
		full:    make(map[string]struct{}),
		domain:  nil,
		keyword: nil,
		regex:   nil,
	}
}

// AddRule adds one rule to the matcher (call before any concurrent use, or during build).
func (m *matcher) AddRule(r Rule) {
	val := strings.ToLower(dns.Fqdn(r.Value))
	if r.Type == RuleFull {
		m.full[val] = struct{}{}
		return
	}
	if r.Type == RuleDomain {
		m.domain = append(m.domain, val)
		return
	}
	if r.Type == RuleKeyword {
		m.keyword = append(m.keyword, strings.ToLower(r.Value))
		return
	}
	if r.Type == RuleRegex {
		re, err := regexp.Compile(r.Value)
		if err != nil {
			return
		}
		m.regex = append(m.regex, re)
	}
}

// domainLabels returns labels from right to left (TLD first). FQDN "a.b.example.com." -> ["com", "example", "b", "a"].
func domainLabels(fqdn string) []string {
	fqdn = strings.TrimSuffix(fqdn, ".")
	if fqdn == "" {
		return nil
	}
	parts := strings.Split(fqdn, ".")
	out := make([]string, 0, len(parts))
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" {
			out = append(out, parts[i])
		}
	}
	return out
}

// insertDomainTrie inserts a single domain rule (FQDN) into the trie. Labels right-to-left.
func (m *matcher) insertDomainTrie(fqdn string) {
	labels := domainLabels(fqdn)
	if len(labels) == 0 {
		return
	}
	if m.domainTrie == nil {
		m.domainTrie = &domainTrieNode{}
	}
	node := m.domainTrie
	for _, label := range labels {
		if node.children == nil {
			node.children = make(map[string]*domainTrieNode)
		}
		next := node.children[label]
		if next == nil {
			next = &domainTrieNode{}
			node.children[label] = next
		}
		node = next
	}
	node.match = true
}

// matchDomainTrie returns true if qname (already normalized FQDN, lower) matches any domain rule in the trie.
func (m *matcher) matchDomainTrie(qname string) bool {
	labels := domainLabels(qname)
	if len(labels) == 0 || m.domainTrie == nil {
		return false
	}
	node := m.domainTrie
	for _, label := range labels {
		if node == nil {
			return false
		}
		if node.match {
			return true
		}
		if node.children == nil {
			return false
		}
		node = node.children[label]
	}
	return node != nil && node.match
}

// Build finalizes the matcher: builds domain trie from domain rules and sorts domain slice for keysForBloom.
// Call after adding all rules.
func (m *matcher) Build() {
	// Build label trie for O(qname labels) domain matching (reference: v2ray DomainMatcherGroup)
	seen := make(map[string]struct{})
	for _, d := range m.domain {
		if _, ok := seen[d]; ok {
			continue
		}
		seen[d] = struct{}{}
		m.insertDomainTrie(d)
	}
	// Keep domain slice sorted for keysForBloom (longest first)
	slices.SortFunc(m.domain, func(a, b string) int {
		return len(b) - len(a)
	})
}

// Match returns true if qname matches any rule. Order: full -> domain (trie) -> keyword -> regex.
func (m *matcher) Match(qname string) bool {
	q := strings.ToLower(dns.Fqdn(qname))

	if _, ok := m.full[q]; ok {
		return true
	}
	if m.matchDomainTrie(q) {
		return true
	}
	for _, k := range m.keyword {
		if strings.Contains(q, k) {
			return true
		}
	}
	for _, re := range m.regex {
		if re.MatchString(q) {
			return true
		}
	}
	return false
}

// keysForBloom returns domain and full values that can be added to a bloom filter
// (for domain and full rules only).
func (m *matcher) keysForBloom() (full []string, domain []string) {
	return slices.Collect(maps.Keys(m.full)), slices.Clone(m.domain)
}

func NewBloomedMatcher(n uint, fp float64) Matcher {
	return &bloomedMatcher{
		m:  matcher{full: make(map[string]struct{})},
		bf: NewBloomFilter(n, fp),
	}
}

type bloomedMatcher struct {
	m  matcher
	bf *BloomFilter
}

func (m *bloomedMatcher) AddRule(r Rule) {
	m.m.AddRule(r)
	switch r.Type {
	case RuleDomain, RuleFull:
		m.bf.Add(r.Value)
	default:
		// do nothing
	}
}

func (m *bloomedMatcher) Build() {
	m.m.Build()
}

func (m *bloomedMatcher) Match(qname string) bool {
	return m.bf.MaybeMatch(qname) && m.m.Match(qname)
}
