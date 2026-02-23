package ruledforward

import (
	"testing"

	"github.com/coredns/coredns/plugin/pkg/proxy"
	"github.com/coredns/coredns/plugin/pkg/transport"
)

func mustProxy(addr string) *proxy.Proxy {
	p := proxy.NewProxy("ruledforward", addr, transport.DNS)
	return p
}

func TestPolicyRandom(t *testing.T) {
	r := &random{}
	if s := r.String(); s != "random" {
		t.Errorf("String() = %q, want %q", s, "random")
	}
	// List with 0 proxies - falls through to default branch, returns empty slice
	list := r.List(nil)
	if list != nil && len(list) != 0 {
		t.Errorf("List(nil) = %v, want empty or nil", list)
	}
	one := []*proxy.Proxy{mustProxy("127.0.0.1:0")}
	list = r.List(one)
	if len(list) != 1 || list[0] != one[0] {
		t.Errorf("List(one) = %v", list)
	}
	two := []*proxy.Proxy{mustProxy("127.0.0.1:0"), mustProxy("127.0.0.2:0")}
	list = r.List(two)
	if len(list) != 2 {
		t.Errorf("List(two) len = %d, want 2", len(list))
	}
	three := []*proxy.Proxy{mustProxy("127.0.0.1:0"), mustProxy("127.0.0.2:0"), mustProxy("127.0.0.3:0")}
	list = r.List(three)
	if len(list) != 3 {
		t.Errorf("List(three) len = %d, want 3", len(list))
	}
}

func TestPolicyRoundRobin(t *testing.T) {
	r := &roundRobin{}
	if s := r.String(); s != "round_robin" {
		t.Errorf("String() = %q, want %q", s, "round_robin")
	}
	one := []*proxy.Proxy{mustProxy("127.0.0.1:0")}
	list := r.List(one)
	if len(list) != 1 {
		t.Errorf("List(one) len = %d", len(list))
	}
	two := []*proxy.Proxy{mustProxy("127.0.0.1:0"), mustProxy("127.0.0.2:0")}
	for range 4 {
		list = r.List(two)
		if len(list) != 2 {
			t.Errorf("List(two) len = %d", len(list))
		}
	}
}

func TestPolicySequential(t *testing.T) {
	r := &sequential{}
	if s := r.String(); s != "sequential" {
		t.Errorf("String() = %q, want %q", s, "sequential")
	}
	p := []*proxy.Proxy{mustProxy("127.0.0.1:0"), mustProxy("127.0.0.2:0")}
	list := r.List(p)
	if len(list) != 2 || list[0] != p[0] || list[1] != p[1] {
		t.Errorf("List() = %v, want same order as input", list)
	}
}
