package ruledforward

import (
	"slices"
	"strings"

	"github.com/bits-and-blooms/bloom/v3"
	"github.com/miekg/dns"
)

// BloomFilter wraps a bloom filter for domain/full keys and provides
// MaybeMatch(qname) that checks qname and all parent suffixes (for domain rules).
// Add must only be called during build (single goroutine); MaybeMatch is safe for concurrent read.
type BloomFilter struct {
	bf *bloom.BloomFilter
}

// NewBloomFilter creates a Bloom filter with estimated n entries and false positive rate fp.
// fp is e.g. 0.01 for 1%.
func NewBloomFilter(n uint, fp float64) *BloomFilter {
	return &BloomFilter{
		bf: bloom.NewWithEstimates(n, fp),
	}
}

// Add adds keys (full domain or domain rule values) to the filter in one call to reduce allocation.
// Must only be used during initial build; not safe for concurrent use with MaybeMatch until done.
func (b *BloomFilter) Add(s ...string) {
	if len(s) == 0 {
		return
	}
	var buf []byte
	for _, str := range s {
		key := strings.ToLower(dns.Fqdn(str))
		if len(key) == 0 {
			continue
		}
		if cap(buf) < len(key) {
			buf = slices.Grow(buf, len(key))
		}
		buf = buf[:len(key)]
		copy(buf, key)
		b.bf.Add(buf)
	}
}

// MaybeMatch returns true if qname or any of its parent suffixes might be in the set.
// Used for pre-match: if false, definitely no match; if true, call full matcher.
// Safe for concurrent read.
func (b *BloomFilter) MaybeMatch(qname string) bool {
	q := strings.ToLower(dns.Fqdn(qname))
	// Check qname itself (full match)
	if b.bf.Test([]byte(q)) {
		return true
	}
	// Check each parent suffix (domain match)
	for {
		idx := strings.Index(q, ".")
		if idx == -1 {
			break
		}
		q = q[idx+1:]
		if q == "" {
			break
		}
		if b.bf.Test([]byte(q)) {
			return true
		}
	}
	return false
}
