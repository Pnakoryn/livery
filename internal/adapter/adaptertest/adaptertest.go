// Package adaptertest has checks every adapter's tests run.
package adaptertest

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/content"
)

// MCPServers are servers using every key the shared repository accepts for an MCP server.
func MCPServers() []content.Item {
	return []content.Item{
		{Kind: content.MCP, Name: "local", Layer: "team", Origin: "mcp/local.json",
			Server: []byte(`{"command":"run","args":["a"],"env":{"K":"${K}"}}`)},
		{Kind: content.MCP, Name: "remote", Layer: "team", Origin: "mcp/remote.json",
			Server: []byte(`{"url":"https://x.example/mcp","headers":{"H":"${H}"}}`)},
	}
}

// EntryKeysCover fails when a rendered non-list entry has no EntryKeys for its object, or
// has a key its EntryKeys does not list.
func EntryKeysCover(t *testing.T, a adapter.Adapter, env adapter.Env) {
	t.Helper()
	plan, err := a.Render(MCPServers(), env)
	if err != nil {
		t.Fatal(err)
	}
	keys := a.EntryKeys(env)
	n := 0
	for _, e := range plan.Entries {
		if e.List {
			continue
		}
		n++
		i := slices.IndexFunc(keys, func(k adapter.EntryKeys) bool { return k.Path == e.Path && k.Key == e.Key })
		if i < 0 {
			t.Errorf("%s: entry %s under %q in %s: no EntryKeys for that object", a.Name(), e.Name, e.Key, e.Path)
			continue
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(e.Value, &obj); err != nil {
			t.Fatalf("%s: entry %s: %v", a.Name(), e.Name, err)
		}
		for k := range obj {
			if !slices.Contains(keys[i].Keys, k) {
				t.Errorf("%s: entry %s writes key %q, which EntryKeys does not list (%v)", a.Name(), e.Name, k, keys[i].Keys)
			}
		}
	}
	if n == 0 {
		t.Errorf("%s: no MCP entry rendered", a.Name())
	}
}

// SameJSON reports whether a and b decode to the same value. Key order and spacing of an entry
// livery generates are not promised, so tests of generated entries compare with this.
func SameJSON(a []byte, b string) bool {
	if len(a) == 0 || b == "" {
		return len(a) == 0 && b == ""
	}
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal([]byte(b), &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}
