package ruledforward

import (
	"context"
	"errors"
	"testing"

	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/pkg/proxy"
	"github.com/coredns/coredns/plugin/pkg/transport"
	"github.com/coredns/coredns/plugin/test"
	"github.com/coredns/coredns/request"

	"github.com/miekg/dns"
)

func TestRuledforwardServeDNS(t *testing.T) {
	r := &Ruledforward{from: "."}
	m := NewBloomedMatcher(1000, 0.01)
	m.AddRule(Rule{Type: RuleDomain, Value: "blocked.example.com."})
	m.Build()
	g := &Group{Name: "block", Action: "empty"}
	g.SetMatcher(m)
	r.groups = []*Group{g}
	nextCalled := false
	r.Next = test.HandlerFunc(func(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
		nextCalled = true
		return dns.RcodeSuccess, nil
	})

	// Query that matches: expect empty response (NODATA with SOA)
	req := new(dns.Msg)
	req.SetQuestion("blocked.example.com.", dns.TypeA)
	rec := dnstest.NewRecorder(&test.ResponseWriter{})
	_, err := r.ServeDNS(context.Background(), rec, req)
	if err != nil {
		t.Fatal(err)
	}
	if nextCalled {
		t.Error("expected Next not to be called when group matches")
	}
	if rec.Rcode != dns.RcodeSuccess {
		t.Errorf("expected RcodeSuccess, got %d", rec.Rcode)
	}
	if rec.Msg == nil {
		t.Fatal("expected non-nil response")
	}
	if len(rec.Msg.Answer) != 0 {
		t.Errorf("expected empty Answer for action empty, got %d", len(rec.Msg.Answer))
	}
	if len(rec.Msg.Ns) == 0 {
		t.Error("expected SOA in Ns for NODATA")
	}

	// Query that does not match: expect Next to be called
	nextCalled = false
	req2 := new(dns.Msg)
	req2.SetQuestion("other.example.org.", dns.TypeA)
	rec2 := dnstest.NewRecorder(&test.ResponseWriter{})
	_, _ = r.ServeDNS(context.Background(), rec2, req2)
	if !nextCalled {
		t.Error("expected Next to be called when no group matches")
	}
}

func TestRuledforwardZoneMatch(t *testing.T) {
	r := &Ruledforward{from: "example.org."}
	r.groups = []*Group{} // no groups
	nextCalled := false
	r.Next = test.HandlerFunc(func(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
		nextCalled = true
		return dns.RcodeSuccess, nil
	})

	req := new(dns.Msg)
	req.SetQuestion("other.example.com.", dns.TypeA) // not in example.org.
	rec := dnstest.NewRecorder(&test.ResponseWriter{})
	_, _ = r.ServeDNS(context.Background(), rec, req)
	if !nextCalled {
		t.Error("expected Next when qname not in from zone")
	}
}

func TestSoaForEmpty(t *testing.T) {
	ns := soaForEmpty("example.com.")
	if len(ns) != 1 {
		t.Fatalf("expected 1 SOA RR, got %d", len(ns))
	}
	if ns[0].Header().Rrtype != dns.TypeSOA {
		t.Errorf("expected SOA type, got %d", ns[0].Header().Rrtype)
	}
}

// Test state.Match for request package
func TestRequestMatch(t *testing.T) {
	req := new(dns.Msg)
	req.SetQuestion("example.com.", dns.TypeA)
	resp := new(dns.Msg)
	resp.SetReply(req)
	resp.Answer = append(resp.Answer, &dns.A{
		Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET},
		A:   nil,
	})
	state := request.Request{W: &test.ResponseWriter{}, Req: req}
	if !state.Match(resp) {
		t.Error("state.Matcher should succeed for matching reply")
	}
}

func TestDefaultGroupMatchesAll(t *testing.T) {
	r := &Ruledforward{from: "."}

	// Create a blocking group that matches specific domain
	m := NewBloomedMatcher(1000, 0.01)
	m.AddRule(Rule{Type: RuleDomain, Value: "ads.example.com."})
	m.Build()

	// Create default group (empty matcher - matches nothing by itself, used as default)
	defaultMatcher := NewMatcher()
	defaultMatcher.Build()
	defaultGroup := &Group{Name: "default", Action: "empty"}
	defaultGroup.SetMatcher(defaultMatcher)

	blockAds := &Group{Name: "block_ads", Action: "empty"}
	blockAds.SetMatcher(m)

	r.groups = []*Group{blockAds, defaultGroup}
	r.defaultGroup = defaultGroup

	nextCalled := false
	r.Next = test.HandlerFunc(func(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
		nextCalled = true
		return dns.RcodeSuccess, nil
	})

	// Query that matches block_ads: expect empty response from block_ads
	req := new(dns.Msg)
	req.SetQuestion("ads.example.com.", dns.TypeA)
	rec := dnstest.NewRecorder(&test.ResponseWriter{})
	_, err := r.ServeDNS(context.Background(), rec, req)
	if err != nil {
		t.Fatal(err)
	}
	if nextCalled {
		t.Error("expected Next not to be called when block_ads group matches")
	}
	if rec.Msg == nil {
		t.Fatal("expected non-nil response")
	}
	if len(rec.Msg.Ns) == 0 {
		t.Error("expected SOA in Ns for NODATA from block_ads")
	}

	// Query that does not match block_ads: expect default group to be used
	nextCalled = false
	req2 := new(dns.Msg)
	req2.SetQuestion("other.example.com.", dns.TypeA)
	rec2 := dnstest.NewRecorder(&test.ResponseWriter{})
	_, err = r.ServeDNS(context.Background(), rec2, req2)
	if err != nil {
		t.Fatal(err)
	}
	if nextCalled {
		t.Error("expected Next not to be called when default group matches")
	}
	if rec2.Msg == nil {
		t.Fatal("expected non-nil response from default group")
	}
	if len(rec2.Msg.Ns) == 0 {
		t.Error("expected SOA in Ns for NODATA from default group")
	}
}

func TestForwardGroupNoProxies(t *testing.T) {
	r := &Ruledforward{from: "."}
	g := &Group{Name: "empty", Action: "forward", Proxies: nil, Policy: &sequential{}}
	req := new(dns.Msg)
	req.SetQuestion("example.com.", dns.TypeA)
	rec := dnstest.NewRecorder(&test.ResponseWriter{})
	state := request.Request{W: rec, Req: req}
	code, err := r.forwardGroup(context.Background(), rec, req, state, g)
	if code != dns.RcodeServerFailure {
		t.Errorf("forwardGroup code = %d, want RcodeServerFailure", code)
	}
	if !errors.Is(err, errNoHealthy) {
		t.Errorf("forwardGroup err = %v, want errNoHealthy", err)
	}
}

func TestOnStartupOnShutdown(t *testing.T) {
	r := &Ruledforward{from: "."}
	p := proxy.NewProxy("ruledforward", "127.0.0.1:0", transport.DNS)
	g := &Group{Name: "g", Proxies: []*proxy.Proxy{p}}
	g.SetMatcher(NewMatcher()) // required for Group to be valid
	r.groups = []*Group{g}
	if err := r.OnStartup(); err != nil {
		t.Fatal(err)
	}
	if err := r.OnShutdown(); err != nil {
		t.Fatal(err)
	}
}
