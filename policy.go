package ruledforward

import (
	"iter"
	"sync/atomic"
	"time"

	"github.com/coredns/coredns/plugin/pkg/proxy"
	"github.com/coredns/coredns/plugin/pkg/rand"
)

const maxProxiesForRandom = 15

// Policy defines a policy for selecting upstreams (same as forward plugin).
// List returns an iterator that yields proxies in policy order, repeating indefinitely.
// Implementations avoid per-call allocation where possible.
type Policy interface {
	List([]*proxy.Proxy) iter.Seq[*proxy.Proxy]
	String() string
}

// random uses double-buffered permutation and an atomic index so no lock is needed.
// Each group has fixed n proxies, so both buffers are indexed by the same n.
type random struct {
	buf [2][maxProxiesForRandom]int
	cur atomic.Uint32 // 0 or 1, which buffer is current
}

func (r *random) String() string { return "random" }

func (r *random) List(p []*proxy.Proxy) iter.Seq[*proxy.Proxy] {
	return func(yield func(*proxy.Proxy) bool) {
		n := len(p)
		if n == 0 {
			return
		}
		if n == 1 {
			for {
				if !yield(p[0]) {
					return
				}
			}
		}
		for {
			next := 1 - (r.cur.Load() % 2)
			perm := r.buf[next][:n]
			for i := range perm {
				perm[i] = i
			}
			// Fisher-Yates shuffle
			for i := n - 1; i > 0; i-- {
				j := rn.Int() % (i + 1)
				perm[i], perm[j] = perm[j], perm[i]
			}
			r.cur.Store(next)
			for _, i := range perm {
				if !yield(p[i]) {
					return
				}
			}
		}
	}
}

type roundRobin struct {
	robin uint32
}

func (r *roundRobin) String() string { return "round_robin" }

func (r *roundRobin) List(p []*proxy.Proxy) iter.Seq[*proxy.Proxy] {
	return func(yield func(*proxy.Proxy) bool) {
		n := len(p)
		if n == 0 {
			return
		}
		poolLen := uint32(n) // #nosec G115 -- pool length is small
		start := atomic.AddUint32(&r.robin, 1) % poolLen
		for {
			for j := range n {
				if !yield(p[(int(start)+j)%n]) {
					return
				}
			}
		}
	}
}

type sequential struct{}

func (r *sequential) String() string { return "sequential" }

func (r *sequential) List(p []*proxy.Proxy) iter.Seq[*proxy.Proxy] {
	return func(yield func(*proxy.Proxy) bool) {
		for {
			for _, pr := range p {
				if !yield(pr) {
					return
				}
			}
		}
	}
}

var rn = rand.New(time.Now().UnixNano())
