//go:build e2e

package e2e

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// MCP server names may hold capitals, dots and underscores; the name is the JSON key unchanged in
// every harness, never split at the dot.
func TestJourneyMCPWideNames(t *testing.T) {
	cases := []struct {
		harness string
		file    string
		keys    []string // path of the object that holds the servers
	}{
		{"kiro", ".kiro/settings/mcp.json", []string{"mcpServers"}},
		{"claude", ".claude.json", []string{"mcpServers"}},
		{"opencode", ".xdg/opencode/opencode.json", []string{"mcp", "servers"}},
	}
	for _, c := range cases {
		t.Run(c.harness, func(t *testing.T) {
			j := newJourney(t)
			j.env = []string{"XDG_CONFIG_HOME=" + j.path(".xdg")}
			writeFile(t, filepath.Join(j.src, "base/mcp/awslabs.core-mcp-server.json"), `{"command":"aws-mcp"}`+"\n")
			writeFile(t, filepath.Join(j.src, "base/mcp/My_Server.json"), `{"command":"my-mcp"}`+"\n")
			commit(t, j.src, "v0.2.0", func() {})
			j.expect(0, "check", j.src)

			servers := func() map[string]json.RawMessage {
				t.Helper()
				var cur map[string]json.RawMessage
				if err := json.Unmarshal([]byte(readFile(t, j.path(c.file))), &cur); err != nil {
					t.Fatal(err)
				}
				for _, k := range c.keys {
					var next map[string]json.RawMessage
					if err := json.Unmarshal(cur[k], &next); err != nil {
						t.Fatalf("%s: %v in %s", k, err, readFile(t, j.path(c.file)))
					}
					cur = next
				}
				return cur
			}

			j.expect(0, "setup", "--source", j.src, "--harness", c.harness)
			s := servers()
			for _, n := range []string{"awslabs.core-mcp-server", "My_Server"} {
				if s[n] == nil {
					t.Errorf("no server %q: %s", n, readFile(t, j.path(c.file)))
				}
			}
			if s["awslabs"] != nil {
				t.Errorf("name was split at the dot: %s", readFile(t, j.path(c.file)))
			}
			j.expect(0, "doctor")
			out, _ := j.expect(0, "update")
			j.has(out, "up to date")

			// A server the user adds survives uninstall.
			var doc map[string]any
			if err := json.Unmarshal([]byte(readFile(t, j.path(c.file))), &doc); err != nil {
				t.Fatal(err)
			}
			var m any = doc
			for _, k := range c.keys {
				m = m.(map[string]any)[k]
			}
			m.(map[string]any)["mine"] = map[string]any{"command": "x"}
			data, _ := json.Marshal(doc)
			writeFile(t, j.path(c.file), string(data))

			j.expect(0, "uninstall")
			s = servers()
			if s["mine"] == nil || s["My_Server"] != nil || s["awslabs.core-mcp-server"] != nil {
				t.Errorf("after uninstall: %s", readFile(t, j.path(c.file)))
			}
		})
	}
}
