package importer

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/content"
	"github.com/Pnakoryn/livery/internal/exit"
)

func mcpSpec() MCPSpec {
	return MCPSpec{
		Origin:      "origin.json",
		IsServerKey: func(k string) bool { return k == "command" || k == "args" || k == "url" },
	}
}

func writeMCP(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "f.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMCPServersValid(t *testing.T) {
	p := writeMCP(t, `{"mcpServers":{"a-b":{"command":"x","args":["1"],"extra":1}}}`)
	found, skipped, err := MCPServers(p, mcpSpec())
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].Name != "a-b" || found[0].Item.Origin != "origin.json" ||
		string(found[0].Item.Server) != `{"command":"x","args":["1"]}` {
		t.Fatalf("found = %+v", found)
	}
	if len(skipped) != 1 || !strings.Contains(skipped[0].Reason, "imported without extra") {
		t.Fatalf("skipped = %+v", skipped)
	}
}

func TestMCPServersBadName(t *testing.T) {
	p := writeMCP(t, `{"mcpServers":{"-bad":{"command":"x"}}}`)
	found, skipped, err := MCPServers(p, mcpSpec())
	if err != nil || len(found) != 0 || len(skipped) != 1 || !strings.Contains(skipped[0].Reason, "name must match") {
		t.Fatalf("found=%v skipped=%v err=%v", found, skipped, err)
	}
}

func TestMCPServersLinkedFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "t.json")
	if err := os.WriteFile(target, []byte(`{"mcpServers":{"a":{"command":"x"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "l.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	found, skipped, err := MCPServers(link, mcpSpec())
	if err != nil || len(found) != 0 || len(skipped) != 1 || skipped[0].Reason != SymlinkReason {
		t.Fatalf("found=%v skipped=%v err=%v", found, skipped, err)
	}
}

func TestMCPServersNotAnObject(t *testing.T) {
	p := writeMCP(t, `{"mcpServers":[]}`)
	if _, _, err := MCPServers(p, mcpSpec()); err == nil || !strings.Contains(err.Error(), "must be a JSON object") {
		t.Fatalf("err = %v", err)
	}
	p = writeMCP(t, `{"mcpServers":{"a":5}}`)
	_, skipped, err := MCPServers(p, mcpSpec())
	if err != nil || len(skipped) != 1 || !strings.Contains(skipped[0].Reason, "not a JSON object") {
		t.Fatalf("skipped=%v err=%v", skipped, err)
	}
}

func TestMCPServersKeyHook(t *testing.T) {
	spec := mcpSpec()
	spec.Key = func(k string, _ json.RawMessage) (bool, bool, string) {
		if k == "off" {
			return true, false, "disabled"
		}
		return false, false, ""
	}
	p := writeMCP(t, `{"mcpServers":{"a":{"off":true,"command":"x"},"b":{"args":[],"command":"y"}}}`)
	found, skipped, err := MCPServers(p, spec)
	if err != nil || len(found) != 1 || string(found[0].Item.Server) != `{"args":[],"command":"y"}` ||
		len(skipped) != 1 || skipped[0].Reason != "server a: disabled" {
		t.Fatalf("found=%v skipped=%v err=%v", found, skipped, err)
	}
}

func TestMCPServersUnreadableIsEnvironment(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads any file")
	}
	p := writeMCP(t, `{"mcpServers":{}}`)
	if err := os.Chmod(p, 0); err != nil {
		t.Fatal(err)
	}
	_, _, err := MCPServers(p, mcpSpec())
	var xe *exit.Error
	if !errors.As(err, &xe) || xe.Code != exit.Environment {
		t.Fatalf("err = %v", err)
	}
}

func TestMCPServersWideName(t *testing.T) {
	p := writeMCP(t, `{"mcpServers":{"awslabs.core-mcp-server":{"url":"https://x","headers":{"X-Api-Key":"k"}}}}`)
	found, skipped, err := MCPServers(p, MCPSpec{Origin: "o", IsServerKey: func(k string) bool { return k == "url" || k == "headers" }})
	if err != nil || len(found) != 1 || found[0].Name != "awslabs.core-mcp-server" || len(skipped) != 0 {
		t.Fatalf("found=%v skipped=%v err=%v", found, skipped, err)
	}
	out, names, err := content.HeaderRefs(found[0].Item.Server, found[0].Name)
	if err != nil || len(names) != 1 || names[0] != "AWSLABS_CORE_MCP_SERVER_X_API_KEY" {
		t.Fatalf("out=%s names=%v err=%v", out, names, err)
	}
}
