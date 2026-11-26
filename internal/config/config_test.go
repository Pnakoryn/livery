package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/exit"
)

func isUsage(err error, contains ...string) bool {
	var e *exit.Error
	if !errors.As(err, &e) || e.Code != exit.Usage {
		return false
	}
	for _, s := range contains {
		if !strings.Contains(e.Message, s) {
			return false
		}
	}
	return true
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "config.json")
	want := Config{Source: "git@x:y.git", Channel: "stable", Teams: []string{"web"}, Harnesses: []string{"kiro"}}
	if err := want.Save(path); err != nil {
		t.Fatal(err)
	}
	got, found, err := Load(path, "livery setup")
	if err != nil || !found || !reflect.DeepEqual(got, want) {
		t.Fatalf("Load = %+v, %v, %v", got, found, err)
	}
	data, _ := os.ReadFile(path)
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for k := range top {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if !reflect.DeepEqual(keys, []string{"channel", "harnesses", "source", "teams"}) {
		t.Errorf("keys %v: %s", keys, data)
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0o644 {
		t.Errorf("mode %v", st.Mode().Perm())
	}
	st, _ = os.Stat(filepath.Dir(path))
	if st.Mode().Perm() != 0o755 {
		t.Errorf("dir mode %v", st.Mode().Perm())
	}
}

func TestSaveEmptyListsAreArrays(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := (Config{Source: "s", Channel: "next"}).Save(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	s := string(data)
	if !strings.Contains(s, `"teams": []`) || !strings.Contains(s, `"harnesses": []`) || !strings.HasSuffix(s, "\n") {
		t.Errorf("got %q", s)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "none.json")
	if _, found, err := Load(missing, "livery setup"); found || err != nil {
		t.Fatalf("missing: %v %v", found, err)
	}
	tests := []struct{ name, body, key string }{
		{"not json", "nope", ""},
		{"unknown key", `{"sourc": "x"}`, "sourc"},
		{"wrong type", `{"teams": "web"}`, "teams"},
		{"trailing", `{} {}`, ""},
		{"wrong case", `{"Source": "x"}`, `"source"`},
		{"duplicate key", `{"source": "x", "source": "y"}`, "twice"},
		{"unknown field", `{"name": "x"}`, "name"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "_")+".json")
			if err := os.WriteFile(p, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			_, _, err := Load(p, "livery setup")
			if !isUsage(err, p, tc.key) {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func TestLoadSourcesIsAnUnknownKey(t *testing.T) {
	dir := t.TempDir()
	for _, body := range []string{
		`{"sources":[{"name":"a","source":"x","channel":"stable","teams":[]}],"harnesses":["kiro"]}`,
		`{"sources": [], "Channel": "x", "bogus": 1}`,
		`{"harnesses":["kiro"],"sources":[{"nam":"x"}]}`,
	} {
		p := filepath.Join(dir, "c.json")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		_, found, err := Load(p, "livery setup --project")
		if found || !isUsage(err, p, "livery setup --project") {
			t.Errorf("%s: found=%v err=%v", body, found, err)
		}
	}
}

func TestValidate(t *testing.T) {
	ok := Config{Source: "s", Channel: "stable", Teams: []string{"web", "a-1"}, Harnesses: []string{"kiro"}}
	if err := ok.Validate("p", []string{"kiro"}, "livery setup"); err != nil {
		t.Fatal(err)
	}
	with := func(f func(*Config)) Config {
		c := ok
		f(&c)
		return c
	}
	tests := []struct {
		name string
		c    Config
		want []string
	}{
		{"empty source", with(func(c *Config) { c.Source = "" }), []string{"p", "source", "livery setup"}},
		{"bad channel", with(func(c *Config) { c.Channel = "nightly" }), []string{"channel", "stable", "next", "vX.Y.Z"}},
		{"no harnesses", with(func(c *Config) { c.Harnesses = []string{} }), []string{"harnesses", "kiro"}},
		{"unknown harness", with(func(c *Config) { c.Harnesses = []string{"cursor"} }), []string{"cursor", "kiro"}},
		{"dup harness", with(func(c *Config) { c.Harnesses = []string{"kiro", "kiro"} }), []string{"harnesses", "kiro"}},
		{"bad team", with(func(c *Config) { c.Teams = []string{"Web_Team"} }), []string{"teams", "Web_Team"}},
		{"dup team", with(func(c *Config) { c.Teams = []string{"web", "web"} }), []string{"teams", "web"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.c.Validate("p", []string{"kiro"}, "livery setup"); !isUsage(err, tc.want...) {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func TestSaveErrorNamesNextCommand(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	err := (Config{}).Save(filepath.Join(blocker, "sub", "config.json"))
	var e *exit.Error
	if !errors.As(err, &e) || !strings.Contains(e.Message, "run the command again") {
		t.Errorf("err = %v", err)
	}
}

func TestValidateEmptySourceNamesSetupCommand(t *testing.T) {
	err := Config{Channel: "stable", Harnesses: []string{"kiro"}}.Validate("p", []string{"kiro"}, "livery setup --project")
	if err == nil || !strings.Contains(err.Error(), "livery setup --project") {
		t.Errorf("got %v", err)
	}
}
