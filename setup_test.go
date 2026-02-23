package ruledforward

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
)

func TestParseRuledforward(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		shouldErr   bool
		expectedErr string
		validate    func(t *testing.T, r *Ruledforward)
	}{
		{
			name: "basic empty group with domain rule",
			input: `ruledforward . {
    group g1 {
        action empty
        domain: example.com
    }
}`,
			shouldErr: false,
			validate: func(t *testing.T, r *Ruledforward) {
				if r.from != "." {
					t.Errorf("from = %q, want %q", r.from, ".")
				}
				if len(r.groups) != 1 {
					t.Fatalf("len(groups) = %d, want 1", len(r.groups))
				}
				g := r.groups[0]
				if g.Name != "g1" {
					t.Errorf("group.Name = %q, want %q", g.Name, "g1")
				}
				if g.Action != "empty" {
					t.Errorf("group.Action = %q, want %q", g.Action, "empty")
				}
				if len(g.InlineRules) != 1 {
					t.Fatalf("len(group.InlineRules) = %d, want 1", len(g.InlineRules))
				}
				rule := g.InlineRules[0]
				if rule.Type != RuleDomain {
					t.Errorf("rule.Type = %v, want %v", rule.Type, RuleDomain)
				}
				if rule.Value != "example.com." {
					t.Errorf("rule.Value = %q, want %q", rule.Value, "example.com.")
				}
			},
		},
		{
			name: "forward group with to and policy",
			input: `ruledforward . {
    group g2 {
        action forward
        to 127.0.0.1
        policy sequential
    }
}`,
			shouldErr: false,
			validate: func(t *testing.T, r *Ruledforward) {
				if len(r.groups) != 1 {
					t.Fatalf("len(groups) = %d, want 1", len(r.groups))
				}
				g := r.groups[0]
				if g.Name != "g2" {
					t.Errorf("group.Name = %q, want %q", g.Name, "g2")
				}
				if g.Action != "forward" {
					t.Errorf("group.Action = %q, want %q", g.Action, "forward")
				}
				if len(g.Proxies) != 1 {
					t.Fatalf("len(group.Proxies) = %d, want 1", len(g.Proxies))
				}
				if g.Policy == nil {
					t.Error("group.Policy is nil")
				}
				if _, ok := g.Policy.(*sequential); !ok {
					t.Errorf("group.Policy type = %T, want *sequential", g.Policy)
				}
			},
		},
		{
			name: "multiple groups",
			input: `ruledforward . {
    group block_ads {
        action empty
        geosite category-ads-all
    }
    group default {
        action forward
        to 114.114.114.114
        policy round_robin
    }
}`,
			shouldErr: false,
			validate: func(t *testing.T, r *Ruledforward) {
				if len(r.groups) != 2 {
					t.Fatalf("len(groups) = %d, want 2", len(r.groups))
				}
				// Check first group
				g1 := r.groups[0]
				if g1.Name != "block_ads" {
					t.Errorf("group[0].Name = %q, want %q", g1.Name, "block_ads")
				}
				if g1.Action != "empty" {
					t.Errorf("group[0].Action = %q, want %q", g1.Action, "empty")
				}
				if len(g1.GeositeNames) != 1 || g1.GeositeNames[0] != "category-ads-all" {
					t.Errorf("group[0].GeositeNames = %v, want [category-ads-all]", g1.GeositeNames)
				}
				// Check second group
				g2 := r.groups[1]
				if g2.Name != "default" {
					t.Errorf("group[1].Name = %q, want %q", g2.Name, "default")
				}
				if g2.Action != "forward" {
					t.Errorf("group[1].Action = %q, want %q", g2.Action, "forward")
				}
				if len(g2.Proxies) != 1 {
					t.Fatalf("len(group[1].Proxies) = %d, want 1", len(g2.Proxies))
				}
				if _, ok := g2.Policy.(*roundRobin); !ok {
					t.Errorf("group[1].Policy type = %T, want *roundRobin", g2.Policy)
				}
			},
		},
		{
			name: "group with all inline rule types",
			input: `ruledforward . {
    group test {
        action empty
        domain: example.com
        full: www.example.com
        keyword: ads
        regex: ^.*\.ads\..*$
    }
}`,
			shouldErr: false,
			validate: func(t *testing.T, r *Ruledforward) {
				if len(r.groups) != 1 {
					t.Fatalf("len(groups) = %d, want 1", len(r.groups))
				}
				g := r.groups[0]
				if len(g.InlineRules) != 4 {
					t.Fatalf("len(group.InlineRules) = %d, want 4", len(g.InlineRules))
				}
				// Check domain rule
				if g.InlineRules[0].Type != RuleDomain || g.InlineRules[0].Value != "example.com." {
					t.Errorf("InlineRules[0] = %+v, want {Type: RuleDomain, Value: example.com.}", g.InlineRules[0])
				}
				// Check full rule
				if g.InlineRules[1].Type != RuleFull || g.InlineRules[1].Value != "www.example.com." {
					t.Errorf("InlineRules[1] = %+v, want {Type: RuleFull, Value: www.example.com.}", g.InlineRules[1])
				}
				// Check keyword rule
				if g.InlineRules[2].Type != RuleKeyword || g.InlineRules[2].Value != "ads" {
					t.Errorf("InlineRules[2] = %+v, want {Type: RuleKeyword, Value: ads}", g.InlineRules[2])
				}
				// Check regex rule
				if g.InlineRules[3].Type != RuleRegex || g.InlineRules[3].Value != "^.*\\.ads\\..*$" {
					t.Errorf("InlineRules[3] = %+v, want {Type: RuleRegex, Value: ^.*\\.ads\\..*$}", g.InlineRules[3])
				}
			},
		},
		{
			name: "group with geosite names",
			input: `ruledforward . {
    group test {
        action empty
        geosite cn google@ads
    }
}`,
			shouldErr: false,
			validate: func(t *testing.T, r *Ruledforward) {
				if len(r.groups) != 1 {
					t.Fatalf("len(groups) = %d, want 1", len(r.groups))
				}
				g := r.groups[0]
				expectedGeosites := []string{"cn", "google@ads"}
				if !reflect.DeepEqual(g.GeositeNames, expectedGeosites) {
					t.Errorf("group.GeositeNames = %v, want %v", g.GeositeNames, expectedGeosites)
				}
			},
		},
		{
			name: "group with custom from zone",
			input: `ruledforward example.com {
    group test {
        action empty
        domain: test.example.com
    }
}`,
			shouldErr: false,
			validate: func(t *testing.T, r *Ruledforward) {
				if r.from != "example.com." {
					t.Errorf("from = %q, want %q", r.from, "example.com.")
				}
			},
		},
		{
			name: "group with max_fails and expire",
			input: `ruledforward . {
    group test {
        action forward
        to 8.8.8.8
        max_fails 3
        expire 5s
    }
}`,
			shouldErr: false,
			validate: func(t *testing.T, r *Ruledforward) {
				if len(r.groups) != 1 {
					t.Fatalf("len(groups) = %d, want 1", len(r.groups))
				}
				g := r.groups[0]
				if g.Maxfails != 3 {
					t.Errorf("group.Maxfails = %d, want 3", g.Maxfails)
				}
				if len(g.Proxies) != 1 {
					t.Fatalf("len(group.Proxies) = %d, want 1", len(g.Proxies))
				}
			},
		},
		{
			name: "group with force_tcp and prefer_udp",
			input: `ruledforward . {
    group test {
        action forward
        to 8.8.8.8
        force_tcp
        prefer_udp
    }
}`,
			shouldErr: false,
			validate: func(t *testing.T, r *Ruledforward) {
				if len(r.groups) != 1 {
					t.Fatalf("len(groups) = %d, want 1", len(r.groups))
				}
				g := r.groups[0]
				if !g.Opts.ForceTCP {
					t.Error("group.Opts.ForceTCP = false, want true")
				}
				if !g.Opts.PreferUDP {
					t.Error("group.Opts.PreferUDP = false, want true")
				}
			},
		},
		{
			name: "group with policy random",
			input: `ruledforward . {
    group test {
        action forward
        to 8.8.8.8 1.1.1.1
        policy random
    }
}`,
			shouldErr: false,
			validate: func(t *testing.T, r *Ruledforward) {
				if len(r.groups) != 1 {
					t.Fatalf("len(groups) = %d, want 1", len(r.groups))
				}
				g := r.groups[0]
				if len(g.Proxies) != 2 {
					t.Fatalf("len(group.Proxies) = %d, want 2", len(g.Proxies))
				}
				if _, ok := g.Policy.(*random); !ok {
					t.Errorf("group.Policy type = %T, want *random", g.Policy)
				}
			},
		},
		{
			name: "error: include not supported",
			input: `ruledforward . {
    group bad {
        action empty
        include: other
    }
}`,
			shouldErr:   true,
			expectedErr: "include:",
		},
		{
			name: "error: empty action with to",
			input: `ruledforward . {
    group bad {
        action empty
        to 8.8.8.8
    }
}`,
			shouldErr:   true,
			expectedErr: "cannot have 'to'",
		},
		{
			name: "error: forward action without to",
			input: `ruledforward . {
    group forward_no_to {
        action forward
    }
}`,
			shouldErr:   true,
			expectedErr: "requires 'to'",
		},
		{
			name: "error: unknown policy",
			input: `ruledforward . {
    group bad {
        action forward
        to 8.8.8.8
        policy invalid_policy
    }
}`,
			shouldErr:   true,
			expectedErr: "unknown policy",
		},
		{
			name: "group with refresh",
			input: `ruledforward . {
    group cron {
        action empty
        domain: example.com
        refresh @daily
    }
}`,
			shouldErr: false,
			validate: func(t *testing.T, r *Ruledforward) {
				if len(r.groups) != 1 {
					t.Fatalf("len(groups) = %d, want 1", len(r.groups))
				}
				if r.groups[0].RefreshCron != "@daily" {
					t.Errorf("RefreshCron = %q", r.groups[0].RefreshCron)
				}
			},
		},
		{
			name: "ignore brace characters",
			input: `ruledforward . {
    group test {
        action empty
        domain: example.com
    }
}`,
			shouldErr: false,
			validate: func(t *testing.T, r *Ruledforward) {
				if len(r.groups) != 1 {
					t.Fatalf("len(groups) = %d, want 1", len(r.groups))
				}
				g := r.groups[0]
				// Verify that { was not parsed as a rule
				for _, rule := range g.InlineRules {
					if strings.Contains(rule.Value, "{") {
						t.Errorf("found rule with '{' in value: %+v", rule)
					}
				}
				// Should only have one rule (domain: example.com)
				if len(g.InlineRules) != 1 {
					t.Errorf("len(group.InlineRules) = %d, want 1 (found: %+v)", len(g.InlineRules), g.InlineRules)
				}
			},
		},
		{
			name: "error: multiple default groups",
			input: `ruledforward . {
    group default {
        action forward
        to 8.8.8.8
    }
    group default {
        action forward
        to 1.1.1.1
    }
}`,
			shouldErr:   true,
			expectedErr: "at most one 'default' group is allowed",
		},
		{
			name: "default group exists",
			input: `ruledforward . {
    group block_ads {
        action empty
        domain: ads.example.com
    }
    group default {
        action forward
        to 8.8.8.8
        policy round_robin
    }
}`,
			shouldErr: false,
			validate: func(t *testing.T, r *Ruledforward) {
				if len(r.groups) != 2 {
					t.Fatalf("len(groups) = %d, want 2", len(r.groups))
				}
				if r.defaultGroup == nil {
					t.Error("defaultGroup is nil, want non-nil")
				}
				if r.defaultGroup.Name != "default" {
					t.Errorf("defaultGroup.Name = %q, want %q", r.defaultGroup.Name, "default")
				}
				if r.defaultGroup.Action != "forward" {
					t.Errorf("defaultGroup.Action = %q, want %q", r.defaultGroup.Action, "forward")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := caddy.NewTestController("dns", tc.input)
			// Ensure dnsserver config exists so GetConfig doesn't panic
			dnsserver.NewServer("", []*dnsserver.Config{{Root: t.TempDir()}})
			r, err := parseRuledforward(c)
			if tc.shouldErr {
				if err == nil {
					t.Errorf("expected error for input %s", tc.input)
					return
				}
				if tc.expectedErr != "" && !strings.Contains(err.Error(), tc.expectedErr) {
					t.Errorf("expected error to contain %q, got %v", tc.expectedErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.validate != nil {
				tc.validate(t, r)
			}
		})
	}
}

func TestSetupWithDlcfile(t *testing.T) {
	// Create a minimal dlc.dat would require protobuf; skip if no file
	dir := t.TempDir()
	dlcPath := filepath.Join(dir, "dlc.dat")
	if err := os.WriteFile(dlcPath, []byte("invalid not protobuf"), 0644); err != nil {
		t.Fatal(err)
	}
	input := `ruledforward . {
    dlcfile ` + dlcPath + `
    group g1 {
        action empty
        geosite cn
    }
}`
	c := caddy.NewTestController("dns", input)
	dnsserver.NewServer("", []*dnsserver.Config{{Root: dir}})
	_, err := parseRuledforward(c)
	// Expect error because file is not valid protobuf
	if err == nil {
		t.Error("expected error when dlcfile is not valid protobuf")
	}
}
