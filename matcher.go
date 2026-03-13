// Package ruledforward implements rule-based forwarding with domain-list-community and AdGuard rules.
package ruledforward

import (
	"iter"
	"regexp"
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

func (r RuleType) String() string {
	switch r {
	case RuleDomain:
		return "domain"
	case RuleFull:
		return "full"
	case RuleKeyword:
		return "keyword"
	case RuleRegex:
		return "regex"
	default:
		return "unknown"
	}
}

// Rule is a single matching rule.
type Rule struct {
	Type  RuleType
	Value string // normalized (lowercase, FQDN for domain/full)
}

type Matcher interface {
	AddRule(r Rule)

	Match(fqdn string) bool
}

// matcher holds rules and provides Match(qname).
// matcher has no internal lock; the holder (Group) uses atomic.Pointer + Store/Load for concurrent safety.
// domainTrie is built in Build() from domain slice for O(qname labels) domain matching instead of O(rules).
type matcher struct {
	full       map[string]struct{} // exact names
	domainTrie *domainTrieNode     // label trie for domain match (right-to-left)
	keyword    []string            // substring
	regex      []*regexp.Regexp    // compiled
}

// NewMatcher returns an empty matcher.
func NewMatcher() Matcher {
	return &matcher{
		full:    make(map[string]struct{}),
		keyword: nil,
		regex:   nil,
	}
}

// AddRule adds one rule to the matcher (call before any concurrent use, or during build).
func (m *matcher) AddRule(r Rule) {
	val := strings.ToLower(dns.Fqdn(r.Value))
	switch r.Type {
	case RuleFull:
		m.full[val] = struct{}{}
	case RuleDomain:
		m.insertDomainTrie(val)
	case RuleKeyword:
		m.keyword = append(m.keyword, strings.ToLower(r.Value))
	case RuleRegex:
		re, err := regexp.Compile(r.Value)
		if err != nil {
			return
		}
		m.regex = append(m.regex, re)
	}
}

// domainLabels yields labels from right to left (TLD first) via iterator-style API.
// Example: FQDN "a.b.example.com." -> yields "com", "example", "b", "a".
func domainLabels(fqdn string) iter.Seq[string] {
	return func(yield func(string) bool) {
		fqdn = strings.TrimSuffix(fqdn, ".")
		if fqdn == "" {
			return
		}
		start := len(fqdn)
		for {
			// Find the last dot at or before current start.
			lastDot := strings.LastIndexByte(fqdn[:start], '.')
			label := fqdn[lastDot+1 : start]
			if label == "" {
				return
			}
			if !yield(label) {
				return
			}
			if lastDot < 0 {
				return
			}
			start = lastDot
		}
	}
}

// insertDomainTrie inserts a single domain rule (FQDN) into the trie. Labels right-to-left.
func (m *matcher) insertDomainTrie(fqdn string) {
	if m.domainTrie == nil {
		m.domainTrie = &domainTrieNode{}
	}
	node := m.domainTrie
	for label := range domainLabels(fqdn) {
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
func (m *matcher) matchDomainTrie(fqdn string) bool {
	node := m.domainTrie
	for label := range domainLabels(fqdn) {
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

// Match returns true if qname matches any rule. Order: full -> domain (trie) -> keyword -> regex.
func (m *matcher) Match(fqdn string) bool {

	if _, ok := m.full[fqdn]; ok {
		return true
	}
	if m.matchDomainTrie(fqdn) {
		return true
	}
	for _, k := range m.keyword {
		if strings.Contains(fqdn, k) {
			return true
		}
	}
	for _, re := range m.regex {
		if re.MatchString(fqdn) {
			return true
		}
	}
	return false
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

func (m *bloomedMatcher) Match(fqdn string) bool {
	return m.bf.MaybeMatch(fqdn) && m.m.Match(fqdn)
}
