package apply

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/lock"
)

func compactJSON(t *testing.T, data []byte) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("%v: %s", err, data)
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestSetKeyPathCreatesObjects(t *testing.T) {
	d := mustParse(t, `{"theme":"x"}`)
	if err := d.Set("mcp.servers", "docs", json.RawMessage(`{"cmd":"d"}`)); err != nil {
		t.Fatal(err)
	}
	got := string(d.Bytes())
	want := "{\n  \"theme\": \"x\",\n  \"mcp\": {\n    \"servers\": {\n      \"docs\": {\n        \"cmd\": \"d\"\n      }\n    }\n  }\n}"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	entries, err := d.Entries("mcp.servers")
	if err != nil || string(entries["docs"]) != `{"cmd":"d"}` {
		t.Errorf("entries %v, %v", entries, err)
	}
}

func TestSetKeyPathKeepsSiblingsAndOrder(t *testing.T) {
	d := mustParse(t, "{\"mcp\": {\"timeout\": 5, \"x\\u0041\": 1.50}, \"z\": true}")
	if err := d.Set("mcp.servers", "docs", json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	got := string(d.Bytes())
	assertOrder(t, got, `"mcp"`, `"timeout": 5`, `"x\u0041": 1.50`, `"servers"`, `"docs"`, `"z": true`)

	if err := d.Remove("mcp.servers", "docs"); err != nil {
		t.Fatal(err)
	}
	if got := compactJSON(t, d.Bytes()); got != `{"mcp":{"servers":{},"timeout":5,"xA":1.5},"z":true}` {
		t.Errorf("after remove %s", got)
	}
	// Removing under a missing path changes nothing.
	before := string(d.Bytes())
	if err := d.Remove("nope.servers", "docs"); err != nil || string(d.Bytes()) != before {
		t.Errorf("remove of a missing path: %v\n%s", err, d.Bytes())
	}
}

func TestKeyPathNotAnObject(t *testing.T) {
	d := mustParse(t, `{"mcp": 5}`)
	err := d.Set("mcp.servers", "docs", json.RawMessage(`{}`))
	assertFinding(t, err)
	if !strings.Contains(err.Error(), `"mcp"`) || !strings.Contains(err.Error(), "mcp.servers") {
		t.Errorf("message %v", err)
	}
	_, err = d.Entries("mcp.servers")
	assertFinding(t, err)
	assertFinding(t, d.Remove("mcp.servers", "docs"))
	assertFinding(t, d.AddItem("mcp.list", "a"))
}

func TestItems(t *testing.T) {
	d := mustParse(t, `{"a": 1}`)
	items, err := d.Items("instructions")
	if err != nil || items == nil || len(items) != 0 {
		t.Fatalf("missing key: %v, %v", items, err)
	}
	if err := d.AddItem("x.instructions", "~/x/*.md"); err != nil {
		t.Fatal(err)
	}
	if err := d.AddItem("x.instructions", "~/x/*.md"); err != nil {
		t.Fatal(err)
	}
	if err := d.AddItem("x.instructions", "b"); err != nil {
		t.Fatal(err)
	}
	if got := compactJSON(t, d.Bytes()); got != `{"a":1,"x":{"instructions":["~/x/*.md","b"]}}` {
		t.Errorf("after add %s", got)
	}
	if err := d.RemoveItem("x.instructions", "~/x/*.md"); err != nil {
		t.Fatal(err)
	}
	if err := d.RemoveItem("x.instructions", "b"); err != nil {
		t.Fatal(err)
	}
	if got := compactJSON(t, d.Bytes()); got != `{"a":1,"x":{"instructions":[]}}` {
		t.Errorf("after remove %s", got)
	}

	for _, bad := range []string{`{"l": [1, 2]}`, `{"l": "a"}`, `{"l": null}`, `{"l": {}}`, `{"l": ["a", null]}`} {
		d := mustParse(t, bad)
		_, err := d.Items("l")
		assertFinding(t, err)
		if !strings.Contains(err.Error(), `"l"`) {
			t.Errorf("%s: message %v", bad, err)
		}
		assertFinding(t, d.AddItem("l", "a"))
		assertFinding(t, d.RemoveItem("l", "a"))
	}
}

func lockWith(t *testing.T, l lock.Lock, key string) lock.Merged {
	t.Helper()
	for _, m := range l.Merged {
		if m.Key == key {
			return m
		}
	}
	t.Fatalf("lock has no %s: %+v", key, l.Merged)
	return lock.Merged{}
}

func pathAndListPlan(home, path string) []adapter.Plan {
	return entryPlan(home,
		adapter.Entry{Path: path, Key: "mcp.servers", Name: "docs", Value: json.RawMessage(`{"cmd":"d"}`)},
		adapter.Entry{Path: path, Key: "instructions", Name: "~/x/*.md", List: true},
	)
}

func TestKeyPathAndListLifecycle(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".h", "app.json")
	put(t, path, `{"theme": "x"}`)

	res := run(t, pathAndListPlan(home, path), lock.Lock{}, home)
	if got, want := report(res, home), "set     ~/.h/app.json instructions[~/x/*.md]\nset     ~/.h/app.json mcp.servers.docs\n"; got != want {
		t.Errorf("report %q, want %q", got, want)
	}
	data, _ := os.ReadFile(path)
	if got := compactJSON(t, data); got != `{"instructions":["~/x/*.md"],"mcp":{"servers":{"docs":{"cmd":"d"}}},"theme":"x"}` {
		t.Errorf("file %s", data)
	}
	if m := lockWith(t, res.Lock, "mcp.servers"); m.List || len(m.Entries) != 1 || m.Entries["docs"] == "" {
		t.Errorf("lock %+v", m)
	}
	m := lockWith(t, res.Lock, "instructions")
	if !m.List || !reflect.DeepEqual(m.Entries, map[string]string{"~/x/*.md": lock.Sum([]byte("~/x/*.md"))}) {
		t.Errorf("lock %+v", m)
	}
	// The lock file keeps List.
	lp := filepath.Join(home, "lock.json")
	if err := res.Lock.Write(lp); err != nil {
		t.Fatal(err)
	}
	l, _, err := lock.ReadIn(lp, lock.Scope{Home: filepath.Dir(lp)})
	if err != nil || !lockWith(t, l, "instructions").List {
		t.Fatalf("read lock %+v, %v", l, err)
	}

	res2, err := compute(t, pathAndListPlan(home, path), l, home)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Changes() || report(res2, home) != "up to date\n" {
		t.Errorf("second run: %s", report(res2, home))
	}

	// The user adds an element and a server by hand.
	put(t, path, `{"theme": "x", "instructions": ["mine.md", "~/x/*.md"], "mcp": {"servers": {"mine": {}, "docs": {"cmd":"d"}}}}`)
	res3 := run(t, pathAndListPlan(home, path), res2.Lock, home)
	if res3.Changes() {
		t.Errorf("third run: %s", report(res3, home))
	}
	data, _ = os.ReadFile(path)
	if got := compactJSON(t, data); got != `{"instructions":["mine.md","~/x/*.md"],"mcp":{"servers":{"docs":{"cmd":"d"},"mine":{}}},"theme":"x"}` {
		t.Errorf("file %s", data)
	}

	// Uninstall removes only livery's two.
	res4 := run(t, entryPlan(home), res3.Lock, home)
	if got, want := report(res4, home), "unset   ~/.h/app.json instructions[~/x/*.md]\nunset   ~/.h/app.json mcp.servers.docs\n"; got != want {
		t.Errorf("report %q, want %q", got, want)
	}
	data, _ = os.ReadFile(path)
	if got := compactJSON(t, data); got != `{"instructions":["mine.md"],"mcp":{"servers":{"mine":{}}},"theme":"x"}` {
		t.Errorf("file %s", data)
	}
	if len(res4.Lock.Merged) != 0 {
		t.Errorf("lock %+v", res4.Lock.Merged)
	}
}

func TestListElementGoneIsAddedAgain(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".h", "app.json")
	res := run(t, pathAndListPlan(home, path), lock.Lock{}, home)
	put(t, path, `{"instructions": [], "mcp": {"servers": {"docs": {"cmd":"d"}}}}`)
	res = run(t, pathAndListPlan(home, path), res.Lock, home)
	if got := report(res, home); got != "set     ~/.h/app.json instructions[~/x/*.md]\n" {
		t.Errorf("report %q", got)
	}
	if !lockWith(t, res.Lock, "instructions").List {
		t.Error("element not recorded")
	}
}

func TestListElementOfTheUserIsAdopted(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".h", "app.json")
	const content = `{"instructions": ["~/x/*.md"]}`
	put(t, path, content)
	plan := entryPlan(home, adapter.Entry{Path: path, Key: "instructions", Name: "~/x/*.md", List: true})
	res := run(t, plan, lock.Lock{}, home)
	if got := report(res, home); got != "adopt   ~/.h/app.json instructions[~/x/*.md]\n" {
		t.Errorf("report %q", got)
	}
	m := lockWith(t, res.Lock, "instructions")
	if !m.List || !reflect.DeepEqual(m.Entries, map[string]string{"~/x/*.md": lock.Sum([]byte("~/x/*.md"))}) {
		t.Errorf("lock %+v", m)
	}
	data, _ := os.ReadFile(path)
	if string(data) != content {
		t.Errorf("file changed: %s", data)
	}
	res2 := run(t, plan, res.Lock, home)
	if res2.Changes() || report(res2, home) != "up to date\n" {
		t.Errorf("second run: %s", report(res2, home))
	}
	res3 := run(t, entryPlan(home), res2.Lock, home)
	if got := report(res3, home); got != "unset   ~/.h/app.json instructions[~/x/*.md]\n" {
		t.Errorf("uninstall report %q", got)
	}
	gone(t, path)
}

func TestKeyPathAndListNewFileRemovedOnUninstall(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".h")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "conf", "app.json")
	res := run(t, pathAndListPlan(home, path), lock.Lock{}, home)
	res = run(t, entryPlan(home), res.Lock, home)
	if len(res.Lock.Merged) != 0 {
		t.Errorf("lock %+v", res.Lock.Merged)
	}
	gone(t, path)
	gone(t, filepath.Dir(path))

	// Something else left in the file keeps it.
	res = run(t, pathAndListPlan(home, path), lock.Lock{}, home)
	put(t, path, `{"instructions": ["~/x/*.md"], "mcp": {"servers": {"docs": {"cmd":"d"}}, "timeout": 1}}`)
	run(t, entryPlan(home), res.Lock, home)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := compactJSON(t, data); got != `{"instructions":[],"mcp":{"servers":{},"timeout":1}}` {
		t.Errorf("file %s", data)
	}
}

func TestOnlyEmptyKeyPaths(t *testing.T) {
	keys := map[string]bool{"mcp.servers": false, "instructions": true}
	for data, want := range map[string]bool{
		`{}`:                              true,
		`{"mcp": {}, "instructions": []}`: true,
		`{"mcp": {"servers": {}}, "instructions": {}}`:         true,
		`{"mcp": {"servers": {}, "x": 1}}`:                     false,
		`{"mcp": {"servers": {"a": 1}}}`:                       false,
		`{"mcp": []}`:                                          false,
		`{"instructions": ["a"]}`:                              false,
		`{"theme": "x"}`:                                       false,
		`{"mcp": {"servers": {}}, "instructions": [], "a":{}}`: false,
	} {
		if got := mustParse(t, data).onlyEmpty(keys); got != want {
			t.Errorf("%s: onlyEmpty %v, want %v", data, got, want)
		}
	}
}

func TestListListedTwiceIsUsage(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".h", "app.json")
	e := adapter.Entry{Path: path, Key: "instructions", Name: "a", List: true, Origin: "team: a"}
	if _, err := compute(t, entryPlan(home, e, e), lock.Lock{}, home); err == nil || !strings.Contains(err.Error(), "wanted twice") {
		t.Errorf("err %v", err)
	}
}

func TestListKeyNotAListIsFinding(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".h", "app.json")
	put(t, path, `{"instructions": [1, 2]}`)
	plan := entryPlan(home, adapter.Entry{Path: path, Key: "instructions", Name: "a", List: true})
	_, err := compute(t, plan, lock.Lock{}, home)
	if err == nil || !strings.Contains(err.Error(), `"instructions"`) || !strings.Contains(err.Error(), "app.json") {
		t.Errorf("err %v", err)
	}
}

func TestListElementChangedWhileRunning(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".h", "app.json")
	put(t, path, `{"instructions": []}`)
	plan := entryPlan(home, adapter.Entry{Path: path, Key: "instructions", Name: "a", List: true})
	res, err := compute(t, plan, lock.Lock{}, home)
	if err != nil {
		t.Fatal(err)
	}
	executeChanged(t, res, home, func() { put(t, path, `{"instructions": ["a"]}`) }, "app.json instructions[a]")

	// The owned element vanishes between Compute and the write of an uninstall.
	put(t, path, `{"instructions": ["a"]}`)
	old := lock.Lock{Source: "s", Merged: []lock.Merged{{Path: "~/.h/app.json", Key: "instructions", List: true, Entries: map[string]string{"a": lock.Sum([]byte("a"))}}}}
	res, err = compute(t, entryPlan(home), old, home)
	if err != nil {
		t.Fatal(err)
	}
	hookOnce(t, path, func() { put(t, path, `{"instructions": []}`) })
	_, err = Execute(res)
	wantChanged(t, err, path+" instructions[a]")
}

func TestPartialLockList(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".h", "app.json")
	r := Result{Home: home, Lock: lock.Lock{Livery: "x"}, Actions: []Action{
		{Op: SetEntry, Path: path, Key: "instructions", Name: "a", List: true, New: listValue("a")},
		{Op: SetEntry, Path: path, Key: "mcp.servers", Name: "d", New: []byte(`{}`)},
	}}
	l := partialLock(r, r.Actions)
	if m := lockWith(t, l, "instructions"); !m.List || m.Entries["a"] != lock.Sum([]byte("a")) {
		t.Errorf("list %+v", m)
	}
	if m := lockWith(t, l, "mcp.servers"); m.List || m.Entries["d"] == "" {
		t.Errorf("entry %+v", m)
	}
}
