// Package contenttest holds the small fixture source for adapter and content unit tests.
// It imports nothing from livery, so any test package can use it without an import cycle.
package contenttest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Source writes a small source for adapter and content unit tests below dir
// (call it with t.TempDir()) and returns its path. It has one base layer with
// instructions, a hook, two MCP servers and a skill with a script, and one team.
func Source(t *testing.T, dir string) string {
	t.Helper()
	files := map[string]string{
		"livery.json":                         `{"schema": 1}` + "\n",
		"base/instructions/commits.md":        "Write commit subjects in the imperative mood, at most 72 characters.\n",
		"base/instructions/go.md":             "---\ndescription: Go style\nglobs: [\"**/*.go\"]\n---\n\nUse the standard library first. Wrap errors with context.\n",
		"base/kiro/hooks/lint.json":           `{"name": "lint", "when": {"type": "fileEdited", "patterns": ["**/*.go"]}, "then": {"type": "runCommand", "command": "golangci-lint run"}}` + "\n",
		"base/mcp/docs.json":                  `{"url": "https://docs.example.com/mcp"}` + "\n",
		"base/mcp/github.json":                `{"command": "github-mcp", "args": ["stdio"], "env": {"GITHUB_TOKEN": "${GITHUB_TOKEN}"}}` + "\n",
		"base/skills/review/SKILL.md":         "---\nname: review\ndescription: Review a change before it is merged.\n---\n\nRead the diff, run `scripts/check.sh`, and list findings by severity.\n",
		"base/skills/review/scripts/check.sh": "#!/bin/sh\nset -e\ngo vet ./...\n",
		"teams/api/skills/openapi/SKILL.md":   "---\nname: openapi\ndescription: Edit the API description from the api team.\n---\n\nKeep openapi.yaml in step with the handlers.\n",
	}
	for p, c := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		mode := os.FileMode(0o644)
		if strings.HasSuffix(p, ".sh") {
			mode = 0o755
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), mode); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// Base returns the path of the base layer of Source written below dir.
func Base(t *testing.T, dir string) string {
	t.Helper()
	return filepath.Join(Source(t, dir), "base")
}
