//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

const reviewerAgent = "---\nname: reviewer\ndescription: Reviews a change.\n---\n\nReview the diff.\n"

// TestClaudeAgents: a Claude Code subagent in base/claude/agents/ is installed, reported as
// edited, removed by uninstall, installed in a project, and imported back.
func TestClaudeAgents(t *testing.T) {
	t.Run("machine", func(t *testing.T) {
		j := newJourney(t)
		commit(t, j.src, "v0.1.1", func() {
			writeFile(t, filepath.Join(j.src, "base/claude/agents/reviewer.md"), reviewerAgent)
		})
		out, _ := j.expect(0, "setup", "--source", j.src, "--harness", "claude", "--team", "")
		j.has(out, "write   ~/.claude/agents/reviewer.md")
		agent := j.path(".claude/agents/reviewer.md")
		if got := readFile(t, agent); got != reviewerAgent {
			t.Fatalf("agent = %q", got)
		}

		writeFile(t, agent, reviewerAgent+"Mine.\n")
		out, _ = j.expect(0, "update", "--plan")
		j.has(out, "edited by you: ~/.claude/agents/reviewer.md")
		writeFile(t, agent, reviewerAgent)

		j.expect(0, "uninstall")
		if exists(agent) {
			t.Error("uninstall left ~/.claude/agents/reviewer.md")
		}
	})

	t.Run("project", func(t *testing.T) {
		j := newJourney(t)
		commit(t, j.src, "v0.1.1", func() {
			writeFile(t, filepath.Join(j.src, "base/claude/agents/reviewer.md"), reviewerAgent)
		})
		proj := filepath.Join(t.TempDir(), "project")
		if err := os.MkdirAll(proj, 0o755); err != nil {
			t.Fatal(err)
		}
		j.cwd = proj
		homeBefore := walk(t, j.home, ".cache/livery")
		j.expect(0, "setup", "--project", "--source", j.src, "--harness", "claude", "--team", "")
		if got := readFile(t, filepath.Join(proj, ".claude/agents/reviewer.md")); got != reviewerAgent {
			t.Fatalf("agent = %q", got)
		}
		sameWalk(t, "setup --project", homeBefore, walk(t, j.home, ".cache/livery"))
	})

	t.Run("outside agents is refused", func(t *testing.T) {
		j := newJourney(t)
		commit(t, j.src, "v0.1.1", func() {
			writeFile(t, filepath.Join(j.src, "base/claude/commands/x.md"), "x\n")
		})
		const msg = "only files below agents/ are copied for Claude Code"
		_, errs := j.expect(2, "setup", "--source", j.src, "--harness", "claude", "--team", "")
		j.has(errs, "claude/commands/x.md: "+msg)
		out, _ := j.expect(1, "check", j.src) // check reports a render error as a finding
		j.has(out, "finding  render claude: base: claude/commands/x.md: "+msg)
	})

	t.Run("import", func(t *testing.T) {
		j := newJourney(t)
		writeFile(t, j.path(".claude/agents/reviewer.md"), reviewerAgent)
		into := filepath.Join(t.TempDir(), "shared")
		j.expect(0, "import", "--from", "claude", "--into", into)
		if got := readFile(t, filepath.Join(into, "base/claude/agents/reviewer.md")); got != reviewerAgent {
			t.Errorf("imported = %q", got)
		}
	})
}
