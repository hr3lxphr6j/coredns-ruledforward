package ruledforward

import (
	"testing"
)

func TestMatcherMatch(t *testing.T) {
	m := NewMatcher()
	m.AddRule(Rule{Type: RuleFull, Value: "exact.example.com."})
	m.AddRule(Rule{Type: RuleDomain, Value: "example.com."})
	m.AddRule(Rule{Type: RuleKeyword, Value: "keyword"})
	m.Build()

	tests := []struct {
		qname  string
		expect bool
	}{
		{"exact.example.com.", true},
		{"sub.exact.example.com.", true}, // domain example.com matches
		{"a.example.com.", true},
		{"example.com.", true},
		{"other.com.", false},
		{"haskeyword.example.org.", true},
		{"no.match.here.", false},
	}
	for i, tc := range tests {
		got := m.Match(tc.qname)
		if got != tc.expect {
			t.Errorf("Test %d: Matcher(%q) = %v, want %v", i, tc.qname, got, tc.expect)
		}
	}
}

// TestGroupMatcherNil verifies Matcher() returns nil when not set.
func TestGroupMatcherNil(t *testing.T) {
	g := &Group{}
	if m := g.Matcher(); m != nil {
		t.Errorf("Matcher() = %v, want nil", m)
	}
}

// TestMatcherAtomicSwap verifies the holder (Group) can atomically swap matchers via SetMatcher (e.g. on refresh).
func TestMatcherAtomicSwap(t *testing.T) {
	m1 := NewMatcher()
	m1.AddRule(Rule{Type: RuleDomain, Value: "old.com."})
	m1.Build()

	m2 := NewMatcher()
	m2.AddRule(Rule{Type: RuleDomain, Value: "new.com."})
	m2.Build()

	g := &Group{}
	g.SetMatcher(m1)
	if m := g.Matcher(); m == nil || !m.Match("a.old.com.") {
		t.Fatal("group should match a.old.com before swap")
	}
	g.SetMatcher(m2)
	if m := g.Matcher(); m == nil || m.Match("a.old.com.") {
		t.Error("group should not match a.old.com after swap")
	}
	if m := g.Matcher(); m == nil || !m.Match("a.new.com.") {
		t.Error("group should match a.new.com after swap")
	}
}

// TestMatcherBuild triggers Build's sort path (multiple domain rules).
func TestMatcherBuild(t *testing.T) {
	m := NewMatcher()
	m.AddRule(Rule{Type: RuleDomain, Value: "short.com."})
	m.AddRule(Rule{Type: RuleDomain, Value: "long.sub.example.com."})
	m.AddRule(Rule{Type: RuleDomain, Value: "medium.example.com."})
	m.Build()
	// After Build, longest match first: long > medium > short
	if !m.Match("a.long.sub.example.com.") {
		t.Error("expected match long")
	}
	if !m.Match("b.medium.example.com.") {
		t.Error("expected match medium")
	}
	if !m.Match("c.short.com.") {
		t.Error("expected match short")
	}
}

// TestMatcherMatchRegex tests regex rule matching.
func TestMatcherMatchRegex(t *testing.T) {
	m := NewMatcher()
	m.AddRule(Rule{Type: RuleRegex, Value: `^.*\.ads\..*\.com\.$`})
	m.Build()
	if !m.Match("track.ads.example.com.") {
		t.Error("expected regex match")
	}
	if m.Match("ads.example.com.") {
		t.Error("expected no match (no .ads. in middle)")
	}
}

// TestBloomedMatcher verifies bloomedMatcher combines Bloom pre-filter with full Matcher.
func TestBloomedMatcher(t *testing.T) {
	m := NewBloomedMatcher(1000, 0.01)
	m.AddRule(Rule{Type: RuleDomain, Value: "example.com."})
	m.AddRule(Rule{Type: RuleFull, Value: "exact.test."})
	m.Build()
	if !m.Match("a.example.com.") {
		t.Error("expected domain match")
	}
	if !m.Match("exact.test.") {
		t.Error("expected full match")
	}
	if m.Match("other.org.") {
		t.Error("expected no match")
	}
}

// TestMatcherDomainTrieEdgeCases covers domain trie: empty, single label, and multi-level.
func TestMatcherDomainTrieEdgeCases(t *testing.T) {
	// Empty trie (no domain rules)
	m0 := NewMatcher()
	m0.Build()
	if m0.Match("any.example.com.") {
		t.Error("empty matcher should not match")
	}

	// Single-label rule "com." matches *\.com
	m1 := NewMatcher()
	m1.AddRule(Rule{Type: RuleDomain, Value: "com."})
	m1.Build()
	if !m1.Match("com.") {
		t.Error("com. should match com.")
	}
	if !m1.Match("a.com.") {
		t.Error("a.com. should match")
	}
	if m1.Match("example.org.") {
		t.Error("example.org. should not match")
	}

	// Multi-level: only sub.example.com. as rule
	m2 := NewMatcher()
	m2.AddRule(Rule{Type: RuleDomain, Value: "sub.example.com."})
	m2.Build()
	if !m2.Match("sub.example.com.") {
		t.Error("sub.example.com. should match")
	}
	if !m2.Match("a.sub.example.com.") {
		t.Error("a.sub.example.com. should match")
	}
	if m2.Match("example.com.") {
		t.Error("example.com. should not match (rule is sub.example.com.)")
	}
}
