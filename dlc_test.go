package ruledforward

import (
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/hr3lxphr6j/coredns-ruledforward/internal/dlcpb"
)

// 测试默认使用项目根目录的 dlc.dat；可通过环境变量 RULEDFORWARD_DLC_DAT 覆盖。
const envDlcDat = "RULEDFORWARD_DLC_DAT"

func getTestDlcDatPath(t *testing.T) string {
	t.Helper()
	if p := os.Getenv(envDlcDat); p != "" {
		return p
	}
	dir, err := os.Getwd()
	if err != nil {
		t.Skipf("getwd: %v", err)
	}
	for {
		p := filepath.Join(dir, "dlc.dat")
		if _, err := os.Stat(p); err == nil {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return p
		}
		dir = parent
	}
}

func TestLoadDLCWire_Empty(t *testing.T) {
	_, err := loadDLCWire(nil)
	if err != errInvalidDLC {
		t.Errorf("loadDLCWire(nil) err = %v, want errInvalidDLC", err)
	}
	_, err = loadDLCWire([]byte{})
	if err != errInvalidDLC {
		t.Errorf("loadDLCWire([]) err = %v, want errInvalidDLC", err)
	}
}

func TestLoadDLCWire_Invalid(t *testing.T) {
	_, err := loadDLCWire([]byte{0xff, 0xff})
	if err == nil {
		t.Error("loadDLCWire(invalid) expected error")
	}
}

func mustMarshal(t *testing.T, m proto.Message) []byte {
	t.Helper()
	b, err := proto.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestLoadDLCWire_MinimalValid(t *testing.T) {
	list := &dlcpb.GeoSiteList{
		Entry: []*dlcpb.GeoSite{
			{
				CountryCode: "test",
				Domain: []*dlcpb.Domain{
					{Type: dlcpb.Domain_RootDomain, Value: "example.com"},
				},
			},
		},
	}
	m, err := loadDLCWire(mustMarshal(t, list))
	if err != nil {
		t.Fatalf("loadDLCWire(minimal): %v", err)
	}
	if len(m) == 0 {
		t.Fatal("expected at least one list")
	}
	rules, ok := m["TEST"]
	if !ok {
		t.Fatalf("expected key TEST, got keys: %v", mapKeys(m))
	}
	if len(rules) != 1 {
		t.Fatalf("len(rules) = %d, want 1", len(rules))
	}
	if rules[0].Type != RuleDomain || rules[0].Value != "example.com" {
		t.Errorf("rule = %+v, want Type=RuleDomain Value=example.com", rules[0])
	}
}

func mapKeys(m map[string][]Rule) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestLoadDLC_File(t *testing.T) {
	path := getTestDlcDatPath(t)
	if _, err := os.Stat(path); err != nil {
		t.Skipf("dlc.dat not found at %s (set %s to override): %v", path, envDlcDat, err)
	}
	m, err := LoadDLC(path)
	if err != nil {
		t.Fatalf("LoadDLC(%s): %v", path, err)
	}
	if len(m) == 0 {
		t.Fatal("expected at least one list from dlc.dat")
	}
	seen := 0
	for _, k := range []string{"CN", "GOOGLE", "CATEGORY-ADS-ALL", "TEST"} {
		if _, ok := m[k]; ok {
			seen++
		}
	}
	if seen == 0 {
		t.Logf("no common list found; sample keys: %v", mapKeys(m))
	}
}

func TestLoadDLC_NoFile(t *testing.T) {
	_, err := LoadDLC("/nonexistent/dlc.dat")
	if err == nil {
		t.Error("LoadDLC(nonexistent) expected error")
	}
}

func TestLoadDLCWire_EmptyListInvalid(t *testing.T) {
	list := &dlcpb.GeoSiteList{Entry: []*dlcpb.GeoSite{{CountryCode: "", Code: ""}}}
	b := mustMarshal(t, list)
	_, err := loadDLCWire(b)
	if err != errInvalidDLC {
		t.Errorf("loadDLCWire(empty list) err = %v, want errInvalidDLC", err)
	}
}

func TestLoadDLCWire_FullDomain(t *testing.T) {
	list := &dlcpb.GeoSiteList{
		Entry: []*dlcpb.GeoSite{
			{
				CountryCode: "test",
				Domain:      []*dlcpb.Domain{{Type: dlcpb.Domain_Full, Value: "full.example.com"}},
			},
		},
	}
	m, err := loadDLCWire(mustMarshal(t, list))
	if err != nil {
		t.Fatal(err)
	}
	rules := m["TEST"]
	if len(rules) != 1 {
		t.Fatalf("len(rules) = %d", len(rules))
	}
	if rules[0].Type != RuleFull || rules[0].Value != "full.example.com" {
		t.Errorf("rule = %+v", rules[0])
	}
}

func TestLoadDLCWire_Keyword(t *testing.T) {
	list := &dlcpb.GeoSiteList{
		Entry: []*dlcpb.GeoSite{
			{
				CountryCode: "test",
				Domain:      []*dlcpb.Domain{{Type: dlcpb.Domain_Plain, Value: "kw"}},
			},
		},
	}
	m, err := loadDLCWire(mustMarshal(t, list))
	if err != nil {
		t.Fatal(err)
	}
	rules := m["TEST"]
	if len(rules) != 1 {
		t.Fatalf("len(rules) = %d", len(rules))
	}
	if rules[0].Type != RuleKeyword || rules[0].Value != "kw" {
		t.Errorf("rule = %+v", rules[0])
	}
}

func TestLoadDLCWire_Regex(t *testing.T) {
	list := &dlcpb.GeoSiteList{
		Entry: []*dlcpb.GeoSite{
			{
				CountryCode: "test",
				Domain:      []*dlcpb.Domain{{Type: dlcpb.Domain_Regex, Value: "^a.+$"}},
			},
		},
	}
	m, err := loadDLCWire(mustMarshal(t, list))
	if err != nil {
		t.Fatal(err)
	}
	rules := m["TEST"]
	if len(rules) != 1 {
		t.Fatalf("len(rules) = %d", len(rules))
	}
	if rules[0].Type != RuleRegex || rules[0].Value != "^a.+$" {
		t.Errorf("rule = %+v", rules[0])
	}
}

func TestLoadDLCWire_Attribute(t *testing.T) {
	list := &dlcpb.GeoSiteList{
		Entry: []*dlcpb.GeoSite{
			{
				CountryCode: "test",
				Domain: []*dlcpb.Domain{
					{
						Type:      dlcpb.Domain_RootDomain,
						Value:     "x.com",
						Attribute: []*dlcpb.Domain_Attribute{{Key: "ads"}},
					},
				},
			},
		},
	}
	m, err := loadDLCWire(mustMarshal(t, list))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m["TEST"]; !ok {
		t.Error("expected key TEST")
	}
	if _, ok := m["TEST@ADS"]; !ok {
		t.Errorf("expected key TEST@ADS, got %v", mapKeys(m))
	}
	if len(m["TEST@ADS"]) != 1 {
		t.Errorf("TEST@ADS rules = %d", len(m["TEST@ADS"]))
	}
	if m["TEST@ADS"][0].Value != "x.com" {
		t.Errorf("TEST@ADS[0].Value = %q", m["TEST@ADS"][0].Value)
	}
}
