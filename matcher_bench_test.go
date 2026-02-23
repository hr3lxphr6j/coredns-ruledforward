package ruledforward

import (
	"fmt"
	"testing"
)

// BenchmarkMatcherMatch_DomainTrie_1e4_Hit benchmarks Match with 10k domain rules, qname hits a rule.
func BenchmarkMatcherMatch_DomainTrie_1e4_Hit(b *testing.B) {
	m := NewMatcher()
	for i := 0; i < 10_000; i++ {
		m.AddRule(Rule{Type: RuleDomain, Value: fmt.Sprintf("sub%d.example.com.", i)})
	}
	m.Build()
	qname := "a.sub5000.example.com."
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.Match(qname)
	}
}

// BenchmarkMatcherMatch_DomainTrie_1e4_Miss benchmarks Match with 10k domain rules, qname misses.
func BenchmarkMatcherMatch_DomainTrie_1e4_Miss(b *testing.B) {
	m := NewMatcher()
	for i := 0; i < 10_000; i++ {
		m.AddRule(Rule{Type: RuleDomain, Value: fmt.Sprintf("sub%d.example.com.", i)})
	}
	m.Build()
	qname := "other.zone.org."
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.Match(qname)
	}
}

// BenchmarkMatcherMatch_DomainTrie_1e5_Hit benchmarks Match with 100k domain rules, qname hits.
func BenchmarkMatcherMatch_DomainTrie_1e5_Hit(b *testing.B) {
	m := NewMatcher()
	for i := 0; i < 100_000; i++ {
		m.AddRule(Rule{Type: RuleDomain, Value: fmt.Sprintf("sub%d.example.com.", i)})
	}
	m.Build()
	qname := "a.sub50000.example.com."
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.Match(qname)
	}
}

// BenchmarkMatcherMatch_Full benchmarks Match when only full rules exist (map lookup).
func BenchmarkMatcherMatch_Full(b *testing.B) {
	m := NewMatcher()
	for i := 0; i < 1000; i++ {
		m.AddRule(Rule{Type: RuleFull, Value: fmt.Sprintf("exact%d.example.com.", i)})
	}
	m.Build()
	qname := "exact500.example.com."
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.Match(qname)
	}
}
