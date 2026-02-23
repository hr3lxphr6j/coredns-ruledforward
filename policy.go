package ruledforward

import (
	"sync/atomic"
	"time"

	"github.com/coredns/coredns/plugin/pkg/proxy"
	"github.com/coredns/coredns/plugin/pkg/rand"
)

// Policy defines a policy for selecting upstreams (same as forward plugin).
type Policy interface {
	List([]*proxy.Proxy) []*proxy.Proxy
	String() string
}

type random struct{}

func (r *random) String() string { return "random" }

func (r *random) List(p []*proxy.Proxy) []*proxy.Proxy {
	switch len(p) {
	case 1:
		return p
	case 2:
		if rn.Int()%2 == 0 {
			return []*proxy.Proxy{p[1], p[0]}
		}
		return p
	}
	perms := rn.Perm(len(p))
	rnd := make([]*proxy.Proxy, len(p))
	for i, p1 := range perms {
		rnd[i] = p[p1]
	}
	return rnd
}

type roundRobin struct {
	robin uint32
}

func (r *roundRobin) String() string { return "round_robin" }

func (r *roundRobin) List(p []*proxy.Proxy) []*proxy.Proxy {
	poolLen := uint32(len(p)) // #nosec G115 -- pool length is small
	i := atomic.AddUint32(&r.robin, 1) % poolLen
	robin := make([]*proxy.Proxy, 0, len(p))
	robin = append(robin, p[i])
	robin = append(robin, p[:i]...)
	robin = append(robin, p[i+1:]...)
	return robin
}

type sequential struct{}

func (r *sequential) String() string { return "sequential" }

func (r *sequential) List(p []*proxy.Proxy) []*proxy.Proxy {
	return p
}

var rn = rand.New(time.Now().UnixNano())
