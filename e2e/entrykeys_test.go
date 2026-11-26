//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Keys Kiro adds to a team MCP server are not an edit: update and doctor pass, a new release
// keeps them, uninstall removes the whole entry.
func TestJourneyKiroMCPExtraKeys(t *testing.T) {
	j := newJourney(t)
	if err := os.Remove(filepath.Join(j.src, "base/mcp/docs.json")); err != nil {
		t.Fatal(err)
	}
	commit(t, j.src, "v0.1.1", func() {})
	j.expect(0, "setup", "--source", j.src, "--harness", "kiro")
	mcp := j.path(".kiro/settings/mcp.json")

	servers := func() map[string]map[string]json.RawMessage {
		t.Helper()
		var doc struct {
			MCPServers map[string]map[string]json.RawMessage `json:"mcpServers"`
		}
		if err := json.Unmarshal([]byte(readFile(t, mcp)), &doc); err != nil {
			t.Fatal(err)
		}
		return doc.MCPServers
	}
	s := servers()
	if len(s) != 1 || s["github"] == nil {
		t.Fatalf("servers: %v", s)
	}
	writeFile(t, mcp, `{
  "mcpServers": {
    "github": {
      "command": "github-mcp",
      "args": ["stdio"],
      "env": {"GITHUB_TOKEN": "${GITHUB_TOKEN}"},
      "disabled": true,
      "autoApprove": ["x"]
    }
  }
}
`)
	out, _ := j.expect(0, "update")
	j.has(out, "up to date")
	j.expect(0, "doctor")

	commit(t, j.src, "v0.2.0", func() {
		writeFile(t, filepath.Join(j.src, "base/mcp/github.json"),
			`{"command": "github-mcp-2", "args": ["stdio"], "env": {"GITHUB_TOKEN": "${GITHUB_TOKEN}"}}`+"\n")
	})
	out, _ = j.expect(0, "update")
	j.has(out, "set     ~/.kiro/settings/mcp.json mcpServers.github")
	g := servers()["github"]
	if string(g["command"]) != `"github-mcp-2"` || string(g["disabled"]) != "true" || compact(t, g["autoApprove"]) != `["x"]` {
		t.Errorf("github after update: %s", readFile(t, mcp))
	}
	out, _ = j.expect(0, "update")
	j.has(out, "up to date")
	j.expect(0, "doctor")

	j.expect(0, "uninstall")
	if exists(mcp) {
		if s := servers(); s["github"] != nil {
			t.Errorf("uninstall left the entry: %s", readFile(t, mcp))
		}
	}
}
