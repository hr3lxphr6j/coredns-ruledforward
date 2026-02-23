package ruledforward

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAdguardRules(t *testing.T) {
	body := `
# comment
! comment
||example.com^
||sub.block.org^
@@||whitelist.com^
plain.com
/regex\.test/
1.2.3.4 host.with.ip
`
	rules, err := ParseAdguardRules(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) < 5 {
		t.Errorf("expected at least 5 rules, got %d", len(rules))
	}
	// domain: example.com, sub.block.org; full: plain.com, host.with.ip; regex: one
	var hasDomain, hasFull, hasRegex bool
	for _, r := range rules {
		if r.Type == RuleDomain && strings.Contains(r.Value, "example.com") {
			hasDomain = true
		}
		if r.Type == RuleFull {
			hasFull = true
		}
		if r.Type == RuleRegex {
			hasRegex = true
		}
	}
	if !hasDomain {
		t.Error("expected domain rule for example.com")
	}
	if !hasFull {
		t.Error("expected at least one full rule")
	}
	if !hasRegex {
		t.Error("expected regex rule")
	}
}

func TestIsURL(t *testing.T) {
	if !IsURL("https://example.com/list.txt") {
		t.Error("https should be URL")
	}
	if !IsURL("http://example.com/list.txt") {
		t.Error("http should be URL")
	}
	if IsURL("/path/to/file") {
		t.Error("path should not be URL")
	}
}

func TestLoadAdguardFromFile(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "rules.txt")
	content := "||file.example.com^\n# comment\n"
	if err := os.WriteFile(fpath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	rules, err := LoadAdguardFromFile(fpath)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if rules[0].Type != RuleDomain || !strings.HasSuffix(rules[0].Value, "file.example.com.") {
		t.Errorf("rule = %+v", rules[0])
	}
	_, err = LoadAdguardFromFile(filepath.Join(dir, "nonexistent"))
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadAdguardFromURL(t *testing.T) {
	content := "||url.example.com^\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(content))
	}))
	defer srv.Close()
	rules, err := LoadAdguardFromURL(srv.URL, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if rules[0].Type != RuleDomain || !strings.HasSuffix(rules[0].Value, "url.example.com.") {
		t.Errorf("rule = %+v", rules[0])
	}
	// non-200 status
	srvBad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srvBad.Close()
	_, err = LoadAdguardFromURL(srvBad.URL, 0, "")
	if err == nil {
		t.Error("expected error for 404")
	}
}
