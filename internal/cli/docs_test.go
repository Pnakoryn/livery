package cli

import (
	"os"
	"strings"
	"testing"
)

// The guided-setup docs must show the prompts and the hint that setup prints.
func TestDocsShowCurrentGuidedSetup(t *testing.T) {
	t.Parallel()
	for _, f := range []string{"../../docs/developers.md", "../../docs/onboarding.md"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		if !strings.Contains(s, "full diff: livery setup --source ... --harness ... --team ... --plan") &&
			!strings.Contains(s, "full diff: livery setup --source /home/you/team-setup --harness kiro") {
			t.Errorf("%s: no full diff hint", f)
		}
	}
	b, _ := os.ReadFile("../../docs/developers.md")
	for _, p := range []string{"Scope (1 or 2) [1]:", "Address of your team's shared repository", "Coding agents to set up", "Teams to add to the base setup", "inside a git repository"} {
		if !strings.Contains(string(b), p) {
			t.Errorf("docs/developers.md lacks %q", p)
		}
	}
}
