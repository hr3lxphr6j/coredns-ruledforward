package ruledforward

import (
	"fmt"
	"iter"
	"testing"

	"github.com/coredns/coredns/plugin/pkg/proxy"
	"github.com/coredns/coredns/plugin/pkg/transport"
)

func mustProxy(addr string) *proxy.Proxy {
	p := proxy.NewProxy("ruledforward", addr, transport.DNS)
	return p
}

func collectN(seq iter.Seq[*proxy.Proxy], n int) []*proxy.Proxy {
	var out []*proxy.Proxy
	for pr := range seq {
		out = append(out, pr)
		if len(out) >= n {
			return out
		}
	}
	return out
}

func TestPolicyRandom(t *testing.T) {
	r := &random{}
	if s := r.String(); s != "random" {
		t.Errorf("String() = %q, want %q", s, "random")
	}
	one := []*proxy.Proxy{mustProxy("127.0.0.1:0")}
	list := collectN(r.List(one), 1)
	if len(list) != 1 || list[0] != one[0] {
		t.Errorf("List(one) = %v", list)
	}
	two := []*proxy.Proxy{mustProxy("127.0.0.1:0"), mustProxy("127.0.0.2:0")}
	list = collectN(r.List(two), 2)
	if len(list) != 2 {
		t.Errorf("List(two) len = %d, want 2", len(list))
	}
	three := []*proxy.Proxy{mustProxy("127.0.0.1:0"), mustProxy("127.0.0.2:0"), mustProxy("127.0.0.3:0")}
	list = collectN(r.List(three), 3)
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
	list := collectN(r.List(one), 1)
	if len(list) != 1 {
		t.Errorf("List(one) len = %d", len(list))
	}
	two := []*proxy.Proxy{mustProxy("127.0.0.1:0"), mustProxy("127.0.0.2:0")}
	for range 4 {
		list = collectN(r.List(two), 2)
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
	list := collectN(r.List(p), 2)
	if len(list) != 2 || list[0] != p[0] || list[1] != p[1] {
		t.Errorf("List() = %v, want same order as input", list)
	}
}

// consumeN drains n items from seq without allocating (for benchmarks).
func consumeN(seq iter.Seq[*proxy.Proxy], n int) {
	count := 0
	for range seq {
		count++
		if count >= n {
			return
		}
	}
}

func makeProxies(n int) []*proxy.Proxy {
	p := make([]*proxy.Proxy, n)
	for i := range p {
		p[i] = mustProxy("127.0.0.1:0")
	}
	return p
}

func BenchmarkPolicySequential_List(b *testing.B) {
	for _, n := range []int{1, 2, 5, 10} {
		b.Run(fmt.Sprintf("%d", n), func(b *testing.B) {
			r := &sequential{}
			proxies := makeProxies(n)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				consumeN(r.List(proxies), n)
			}
		})
	}
}

func BenchmarkPolicyRoundRobin_List(b *testing.B) {
	for _, n := range []int{1, 2, 5, 10} {
		b.Run(fmt.Sprintf("%d", n), func(b *testing.B) {
			r := &roundRobin{}
			proxies := makeProxies(n)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				consumeN(r.List(proxies), n)
			}
		})
	}
}

func BenchmarkPolicyRandom_List(b *testing.B) {
	for _, n := range []int{1, 2, 5, 10} {
		b.Run(fmt.Sprintf("%d", n), func(b *testing.B) {
			r := &random{}
			proxies := makeProxies(n)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				consumeN(r.List(proxies), n)
			}
		})
	}
}
