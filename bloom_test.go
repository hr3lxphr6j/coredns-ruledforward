package ruledforward

import (
	"testing"
)

func TestBloomMaybeMatch(t *testing.T) {
	bf := NewBloomFilter(1000, 0.01)
	bf.Add("example.com.", "full.match.org.")

	if !bf.MaybeMatch("full.match.org.") {
		t.Error("expected MaybeMatch(full) true")
	}
	if !bf.MaybeMatch("sub.example.com.") {
		t.Error("expected MaybeMatch(sub.example.com) true (suffix)")
	}
	if !bf.MaybeMatch("example.com.") {
		t.Error("expected MaybeMatch(example.com) true")
	}
	if bf.MaybeMatch("other.org.") {
		t.Error("expected MaybeMatch(other.org) false")
	}
}
