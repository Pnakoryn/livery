package apply

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/lock"
)

// entryKeysCase is one row: the entry's value on disk, the wanted value ("" when the entry is
// no longer wanted), whether the plan has EntryKeys, and the op Compute decides on.
type entryKeysCase struct {
	name     string
	disk     string
	want     string
	noKeys   bool
	op       Op
	afterRun string // the entry after Execute; "" when it is gone, "-" when not checked
}

func TestEntryKeysOwnOnlyLiveryKeys(t *testing.T) {
	cases := []entryKeysCase{
		{name: "a extra key", disk: `{"command":"a","disabled":true}`, want: `{"command":"a"}`, op: Unchanged, afterRun: `{"command":"a","disabled":true}`},
		{name: "b update keeps extra", disk: `{"command":"a","disabled":true}`, want: `{"command":"b"}`, op: SetEntry, afterRun: `{"command":"b","disabled":true}`},
		{name: "c own key edited", disk: `{"command":"x","disabled":true}`, want: `{"command":"a"}`, op: Edited, afterRun: `{"command":"x","disabled":true}`},
		{name: "d removed", disk: `{"command":"a","disabled":true}`, op: UnsetEntry, afterRun: ""},
		{name: "e removed, own key edited", disk: `{"command":"x","disabled":true}`, op: LeftEdited, afterRun: `{"command":"x","disabled":true}`},
		{name: "f no EntryKeys", disk: `{"command":"a","disabled":true}`, want: `{"command":"a"}`, noKeys: true, op: Edited, afterRun: `{"command":"a","disabled":true}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			path := filepath.Join(home, ".h", "settings", "mcp.json")
			keys := []adapter.EntryKeys{{Path: path, Key: "mcpServers", Keys: []string{"command", "args", "env", "url", "headers"}}}
			if c.noKeys {
				keys = nil
			}
			plan := func(value string) []adapter.Plan {
				p := adapter.Plan{Root: filepath.Join(home, ".h"), EntryKeys: keys}
				if value != "" {
					p.Entries = []adapter.Entry{{Path: path, Key: "mcpServers", Name: "s", Value: json.RawMessage(value), Origin: "team: mcp/s.json"}}
				}
				return []adapter.Plan{p}
			}
			// Install {"command":"a"}: the lock records it.
			first := run(t, plan(`{"command":"a"}`), lock.Lock{}, home)
			put(t, path, `{"mcpServers": {"s": `+c.disk+`}, "other": 1}`)

			res, err := compute(t, plan(c.want), first.Lock, home)
			if err != nil {
				t.Fatal(err)
			}
			a := entryAction(t, res, "s")
			if a.Op != c.op {
				t.Fatalf("op %q, want %q", a.Op, c.op)
			}
			if c.op == SetEntry && string(a.New) != c.afterRun {
				t.Errorf("Action.New %s, want %s", a.New, c.afterRun)
			}
			if _, err := Execute(res); err != nil && c.op != Edited {
				t.Fatal(err)
			}
			got := diskEntry(t, path, "s")
			if got != c.afterRun {
				t.Errorf("after Execute: %q, want %q", got, c.afterRun)
			}
			if c.op == SetEntry || c.op == Unchanged {
				want, _ := lock.SumJSON(json.RawMessage(c.want))
				if sum := mergedSum(res.Lock, "s"); sum != want {
					t.Errorf("lock sum %q, want the sum of the wanted value %q", sum, want)
				}
			}
		})
	}
}

// An entry livery does not own, with livery's keys as wanted and an extra key, is adopted:
// nothing is written.
func TestEntryKeysAdoptWithExtraKey(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".h", "settings", "mcp.json")
	disk := `{"mcpServers": {"s": {"disabled": true, "command": "a"}}}`
	put(t, path, disk)
	plans := []adapter.Plan{{
		Root:      filepath.Join(home, ".h"),
		Entries:   []adapter.Entry{{Path: path, Key: "mcpServers", Name: "s", Value: json.RawMessage(`{"command":"a"}`)}},
		EntryKeys: []adapter.EntryKeys{{Path: path, Key: "mcpServers", Keys: []string{"command"}}},
	}}
	res := run(t, plans, lock.Lock{}, home)
	if a := entryAction(t, res, "s"); a.Op != Adopt {
		t.Fatalf("op %q", a.Op)
	}
	if data, _ := os.ReadFile(path); string(data) != disk {
		t.Errorf("file changed: %s", data)
	}
}

// The partial lock of a run that stopped records the wanted value's sum for a SetEntry that
// kept extra keys.
func TestEntryKeysPartialLockRecordsWantedSum(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".h", "settings", "mcp.json")
	keys := []adapter.EntryKeys{{Path: path, Key: "mcpServers", Keys: []string{"command"}}}
	plan := func(v string) []adapter.Plan {
		return []adapter.Plan{{Root: filepath.Join(home, ".h"), EntryKeys: keys,
			Entries: []adapter.Entry{{Path: path, Key: "mcpServers", Name: "s", Value: json.RawMessage(v)}}}}
	}
	first := run(t, plan(`{"command":"a"}`), lock.Lock{}, home)
	put(t, path, `{"mcpServers": {"s": {"command": "a", "disabled": true}}}`)
	res, err := compute(t, plan(`{"command":"b"}`), first.Lock, home)
	if err != nil {
		t.Fatal(err)
	}
	l := partialLock(res, res.Actions)
	want, _ := lock.SumJSON(json.RawMessage(`{"command":"b"}`))
	if got := mergedSum(l, "s"); got != want {
		t.Errorf("partial lock sum %q, want %q", got, want)
	}
}

func entryAction(t *testing.T, r Result, name string) Action {
	t.Helper()
	for _, a := range r.Actions {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("no action for %q: %+v", name, r.Actions)
	return Action{}
}

func mergedSum(l lock.Lock, name string) string {
	for _, m := range l.Merged {
		if s, ok := m.Entries[name]; ok {
			return s
		}
	}
	return ""
}

// diskEntry is the compact JSON of the entry name under mcpServers in path, "" when absent.
func diskEntry(t *testing.T, path, name string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	d, err := ParseDoc(path, data)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := d.Entries("mcpServers")
	if err != nil {
		t.Fatal(err)
	}
	v, ok := entries[name]
	if !ok {
		return ""
	}
	return strings.Join(strings.Fields(string(v)), "")
}
