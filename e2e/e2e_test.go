//go:build e2e

// Package e2e holds the user journeys: they run the built livery binary in a throwaway home
// directory against a fixture source repository. Run them with `make e2e`.
package e2e

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var (
	binary  string // the built livery binary
	tempDir string // everything the tests write lives below this directory
)

func TestMain(m *testing.M) { os.Exit(run(m)) }

func run(m *testing.M) int {
	dir, err := os.MkdirTemp("", "livery-e2e-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e:", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()
	tempDir = dir
	binary = filepath.Join(dir, "bin", "livery")
	build := exec.Command("go", "build", "-o", binary, "../cmd/livery")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: go build: %v\n%s", err, out)
		return 1
	}
	// t.TempDir() lives below TMPDIR: every journey home is below dir.
	if err := os.Setenv("TMPDIR", dir); err != nil {
		fmt.Fprintln(os.Stderr, "e2e:", err)
		return 1
	}
	return m.Run()
}

// gitEnv is the environment of every git call: no user or system configuration, fixed identity.
func gitEnv() []string {
	return append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_AUTHOR_DATE=2020-01-01T00:00:00Z",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com", "GIT_COMMITTER_DATE=2020-01-01T00:00:00Z")
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		dst := filepath.Join(to, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// newSource creates a git repository from testdata/source, tagged v0.1.0, and returns its path.
func newSource(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	copyTree(t, "testdata/source", repo)
	if err := os.Chmod(filepath.Join(repo, "base/skills/review/scripts/check.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q", "-b", "main")
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-q", "-m", "v0.1.0")
	git(t, repo, "tag", "v0.1.0")
	return repo
}

// commit applies change to the repository, commits, and tags when tag is not empty.
func commit(t *testing.T, repo, tag string, change func()) {
	t.Helper()
	change()
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-q", "-m", "change "+tag)
	if tag != "" {
		git(t, repo, "tag", tag)
	}
}

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// journey is one throwaway home and a helper to run the binary in it.
type journey struct {
	t    *testing.T
	home string
	src  string
	// env is added to the binary's environment.
	env []string
	// cwd is the binary's working directory; empty means home.
	cwd string
}

// newJourney makes a home with .kiro/ and .claude/ and a fresh source repository.
func newJourney(t *testing.T) *journey {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	if !strings.HasPrefix(home, tempDir+string(filepath.Separator)) {
		t.Fatalf("HOME %s is not below the test's temp directory %s", home, tempDir)
	}
	for _, d := range []string{".kiro", ".claude"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return &journey{t: t, home: home, src: newSource(t)}
}

func (j *journey) path(p string) string { return filepath.Join(j.home, filepath.FromSlash(p)) }

// livery runs the binary and returns stdout, stderr and the exit code; the command and its
// output are logged, so a failing journey shows them.
func (j *journey) livery(args ...string) (stdout, stderr string, code int) {
	j.t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = j.home
	if j.cwd != "" {
		cmd.Dir = j.cwd
	}
	cmd.Env = []string{
		"HOME=" + j.home,
		"PATH=" + os.Getenv("PATH"),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	}
	cmd.Env = append(cmd.Env, j.env...)
	var out, errb bytes.Buffer
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, &out, &errb
	err := cmd.Run()
	code = 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			j.t.Fatalf("livery %s: %v", strings.Join(args, " "), err)
		}
		code = ee.ExitCode()
	}
	j.t.Logf("$ livery %s\nexit %d\nstdout:\n%sstderr:\n%s", strings.Join(args, " "), code, out.String(), errb.String())
	return out.String(), errb.String(), code
}

// ok runs livery, requires exit code want, and returns stdout and stderr.
func (j *journey) expect(want int, args ...string) (string, string) {
	j.t.Helper()
	o, e, c := j.livery(args...)
	if c != want {
		j.t.Fatalf("livery %s: exit %d, want %d\nstdout:\n%sstderr:\n%s", strings.Join(args, " "), c, want, o, e)
	}
	return o, e
}

func (j *journey) has(s string, want ...string) {
	j.t.Helper()
	for _, w := range want {
		if !strings.Contains(s, w) {
			j.t.Errorf("output lacks %q:\n%s", w, s)
		}
	}
}

// walk records path, mode and sha256 of every file and the path and mode of every directory below
// dir. Symbolic links are not followed; skip lists slash-separated paths relative to dir that are
// left out with everything below them, and so are the directories that lead to a skipped path
// (livery creates ~/.cache/livery and ~/.config/livery and may leave them empty).
func walk(t *testing.T, dir string, skip ...string) map[string]string {
	t.Helper()
	res := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		for _, s := range skip {
			if rel == s || strings.HasPrefix(rel, s+"/") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if d.IsDir() && (rel == "." || isAncestor(rel, skip)) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		sum := ""
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			h := sha256.Sum256(data)
			sum = hex.EncodeToString(h[:])
		}
		res[rel] = fmt.Sprintf("%v %s", info.Mode(), sum)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// isAncestor reports whether a skipped path lies below the directory rel.
func isAncestor(rel string, skip []string) bool {
	for _, s := range skip {
		if strings.HasPrefix(s, rel+"/") {
			return true
		}
	}
	return false
}

func sameWalk(t *testing.T, what string, before, after map[string]string) {
	t.Helper()
	var keys []string
	for k := range before {
		keys = append(keys, k)
	}
	for k := range after {
		if _, ok := before[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		if before[k] != after[k] {
			t.Errorf("%s: %s: before %q, after %q", what, k, before[k], after[k])
		}
	}
}

// TestJourneys holds the machine-scope journeys, each in a fresh home with .kiro/ and .claude/:
//
//	J1  first install on a fresh machine: setup --plan writes nothing, setup installs the
//	    release for Kiro and Claude Code, doctor is clean, a second update changes nothing.
//	J2  update to a new release: a changed skill, a removed instruction and a new MCP server,
//	    shown by update --plan and applied by update.
//	J3  the user's own files and servers are kept, yours through setup, update and uninstall,
//	    also a server named like a shared one.
//	J4  an edited file is skipped, the rest is applied, and update exits 1 until it is removed.
//	J5  a team and the personal layer override the base; two clashing teams are refused.
//	J6  channels: a pinned release, then next (an untagged commit) and stable.
//	J7  uninstall leaves the home as it was, except the config and the source cache.
//	J8  the user's keys and servers in ~/.claude.json survive setup, update and uninstall.
//
// ExecBitOnly: a release that only drops a script's exec bit changes the file mode.
func TestJourneys(t *testing.T) {
	t.Run("J1 first install on a fresh machine", func(t *testing.T) {
		j := newJourney(t)
		before := walk(t, j.home)
		out, _ := j.expect(0, "setup", "--source", j.src, "--plan")
		j.has(out, "write   ~/.kiro/skills/review/SKILL.md", "write   ~/.claude/skills/review/SKILL.md")
		sameWalk(t, "setup --plan", before, walk(t, j.home, ".cache/livery"))
		if exists(j.path(".config/livery/config.json")) {
			t.Error("setup --plan wrote the config")
		}

		out, _ = j.expect(0, "setup", "--source", j.src)
		j.has(out, "v0.1.0",
			"write   ~/.kiro/skills/review/SKILL.md",
			"write   ~/.kiro/skills/review/scripts/check.sh",
			"write   ~/.kiro/steering/go.md",
			"write   ~/.kiro/steering/commits.md",
			"write   ~/.kiro/hooks/lint.json",
			"set     ~/.kiro/settings/mcp.json mcpServers.docs",
			"set     ~/.kiro/settings/mcp.json mcpServers.github",
			"write   ~/.claude/skills/review/SKILL.md",
			"write   ~/.claude/rules/go.md",
			"write   ~/.claude/rules/commits.md",
			"set     ~/.claude.json mcpServers.docs",
		)
		for _, p := range []string{".kiro/skills/review/scripts/check.sh", ".claude/skills/review/scripts/check.sh"} {
			fi, err := os.Stat(j.path(p))
			if err != nil {
				t.Fatal(err)
			}
			if fi.Mode().Perm()&0o100 == 0 {
				t.Errorf("%s is not executable: %v", p, fi.Mode())
			}
		}
		if s := readFile(t, j.path(".kiro/steering/go.md")); !strings.Contains(s, "fileMatch") {
			t.Errorf("steering/go.md has no fileMatch:\n%s", s)
		}
		if s := readFile(t, j.path(".kiro/steering/commits.md")); !strings.Contains(s, "inclusion: always") {
			t.Errorf("steering/commits.md is not always on:\n%s", s)
		}
		if s := readFile(t, j.path(".kiro/settings/mcp.json")); !strings.Contains(s, "${GITHUB_TOKEN}") {
			t.Errorf("mcp.json lacks the github server:\n%s", s)
		}

		j.expect(0, "doctor")
		installed := walk(t, j.home, ".cache/livery")
		out, _ = j.expect(0, "update")
		j.has(out, "up to date")
		sameWalk(t, "update", installed, walk(t, j.home, ".cache/livery"))
	})

	t.Run("J2 update to a new release", func(t *testing.T) {
		j := newJourney(t)
		j.expect(0, "setup", "--source", j.src)
		commit(t, j.src, "v0.2.0", func() {
			writeFile(t, filepath.Join(j.src, "base/skills/review/SKILL.md"),
				"---\nname: review\ndescription: Review a change before it is merged.\n---\n\nRead the diff and list findings by severity. Version two.\n")
			if err := os.Remove(filepath.Join(j.src, "base/instructions/commits.md")); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(j.src, "base/mcp/search.json"), `{"url": "https://search.example.com/mcp"}`+"\n")
		})
		installed := walk(t, j.home, ".cache/livery")
		out, _ := j.expect(0, "update", "--plan")
		j.has(out, "v0.2.0",
			"write   ~/.kiro/skills/review/SKILL.md",
			"remove  ~/.kiro/steering/commits.md",
			"set     ~/.kiro/settings/mcp.json mcpServers.search",
			"remove  ~/.claude/rules/commits.md",
			"set     ~/.claude.json mcpServers.search",
			"--- ", "+++ ", "+Read the diff and list findings by severity. Version two.")
		sameWalk(t, "update --plan", installed, walk(t, j.home, ".cache/livery"))

		out, _ = j.expect(0, "update")
		j.has(out, "write   ~/.kiro/skills/review/SKILL.md", "remove  ~/.kiro/steering/commits.md")
		if !strings.Contains(readFile(t, j.path(".kiro/skills/review/SKILL.md")), "Version two.") {
			t.Error("the skill was not updated")
		}
		if exists(j.path(".kiro/steering/commits.md")) || exists(j.path(".claude/rules/commits.md")) {
			t.Error("the removed instruction is still installed")
		}
		if !strings.Contains(readFile(t, j.path(".kiro/settings/mcp.json")), "search.example.com") {
			t.Error("the new server is not in mcp.json")
		}
		out, _ = j.expect(0, "update")
		j.has(out, "up to date")
	})

	t.Run("ExecBitOnly", func(t *testing.T) {
		j := newJourney(t)
		j.expect(0, "setup", "--source", j.src)
		commit(t, j.src, "v0.2.0", func() {
			if err := os.Chmod(filepath.Join(j.src, "base/skills/review/scripts/check.sh"), 0o644); err != nil {
				t.Fatal(err)
			}
		})
		script := j.path(".kiro/skills/review/scripts/check.sh")
		out, _ := j.expect(0, "update", "--plan")
		j.has(out, "write   ~/.kiro/skills/review/scripts/check.sh", "mode    ~/.kiro/skills/review/scripts/check.sh: not executable")
		out, _ = j.expect(0, "update")
		j.has(out, "write   ~/.kiro/skills/review/scripts/check.sh")
		fi, err := os.Stat(script)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm()&0o111 != 0 {
			t.Errorf("script is still executable: %v", fi.Mode())
		}
		out, _ = j.expect(0, "update")
		j.has(out, "up to date")
	})

	t.Run("J3 the user's own files and servers are kept", func(t *testing.T) {
		j := newJourney(t)
		mine := "# my own steering\n"
		writeFile(t, j.path(".kiro/steering/mine.md"), mine)
		writeFile(t, j.path(".kiro/settings/mcp.json"),
			`{"mcpServers": {"mine": {"command": "my-server"}}}`+"\n")
		mineServer := `"command": "my-server"`
		// The user's own regular file at a path livery wants: the Kiro steering file of the
		// fixture's instruction.
		ownGo := "# my own go rules\n"
		writeFile(t, j.path(".kiro/steering/go.md"), ownGo)

		check := func(step string) {
			t.Helper()
			if got := readFile(t, j.path(".kiro/steering/mine.md")); got != mine {
				t.Errorf("%s: mine.md changed: %q", step, got)
			}
			if got := readFile(t, j.path(".kiro/steering/go.md")); got != ownGo {
				t.Errorf("%s: the user's steering/go.md changed: %q", step, got)
			}
			s := readFile(t, j.path(".kiro/settings/mcp.json"))
			if !strings.Contains(s, `"mine"`) || !strings.Contains(s, mineServer) {
				t.Errorf("%s: the user's server is gone from mcp.json:\n%s", step, s)
			}
		}
		out, _ := j.expect(0, "setup", "--source", j.src)
		j.has(out, "kept, yours: ~/.kiro/steering/go.md")
		check("setup")
		out, _ = j.expect(1, "doctor")
		j.has(out, "finding  kept, yours:", "steering/go.md")
		commit(t, j.src, "v0.2.0", func() {
			writeFile(t, filepath.Join(j.src, "base/mcp/search.json"), `{"url": "https://search.example.com/mcp"}`+"\n")
		})
		j.expect(0, "update")
		check("update")
		j.expect(0, "uninstall")
		check("uninstall")

		// A user server named like a shared one is theirs.
		j2 := newJourney(t)
		writeFile(t, j2.path(".kiro/settings/mcp.json"),
			`{"mcpServers": {"github": {"command": "my-own-github"}}}`+"\n")
		out, _ = j2.expect(0, "setup", "--source", j2.src)
		j2.has(out, "kept, yours: ~/.kiro/settings/mcp.json mcpServers.github")
		if s := readFile(t, j2.path(".kiro/settings/mcp.json")); !strings.Contains(s, "my-own-github") || strings.Contains(s, "github-mcp") {
			t.Errorf("the user's github server was changed:\n%s", s)
		}
		out, _ = j2.expect(1, "doctor")
		j2.has(out, "finding  kept, yours:", "mcpServers.github")
	})

	t.Run("J4 an edited file is skipped until removed", func(t *testing.T) {
		// An edited file is skipped, the rest of the release is applied, and the run exits 1
		// until the user removes or reverts it.
		j := newJourney(t)
		j.expect(0, "setup", "--source", j.src)
		ins := j.path(".kiro/steering/go.md")
		skill := j.path(".kiro/skills/review/SKILL.md")
		edited := readFile(t, ins) + "\nMy own rule.\n"
		writeFile(t, ins, edited)
		commit(t, j.src, "v0.2.0", func() {
			writeFile(t, filepath.Join(j.src, "base/instructions/go.md"),
				"---\ndescription: Go style\nglobs: [\"**/*.go\"]\n---\n\nGo rules, version two.\n")
			writeFile(t, filepath.Join(j.src, "base/skills/review/SKILL.md"),
				"---\nname: review\ndescription: Review a change before it is merged.\n---\n\nVersion two.\n")
		})

		before := walk(t, j.home, ".cache/livery")
		out, errs := j.expect(0, "update", "--plan")
		j.has(out, "edited by you: ~/.kiro/steering/go.md", "write   ~/.kiro/skills/review/SKILL.md")
		if strings.Contains(out+errs, "edited  ") {
			t.Errorf("--plan printed an edited line:\n%s", out)
		}
		sameWalk(t, "update --plan", before, walk(t, j.home, ".cache/livery"))

		out, errs = j.expect(1, "update")
		j.has(out, "write   ~/.kiro/skills/review/SKILL.md",
			"edited  ~/.kiro/steering/go.md: you changed it; livery kept your version and did not install the team's",
			"to take the team's version: delete ~/.kiro/steering/go.md, then run livery update",
			"to keep your change: put your text into ~/.config/livery/personal/instructions/go.md (Markdown; the only frontmatter keys are description and globs)")
		if strings.Contains(out, "edited by you:") {
			t.Errorf("an applying update listed the edited file twice:\n%s", out)
		}
		j.has(errs, "1 edited by you, kept; the rest is applied")
		if got := readFile(t, skill); !strings.Contains(got, "Version two.") {
			t.Errorf("the skill was not updated:\n%s", got)
		}
		if got := readFile(t, ins); got != edited {
			t.Errorf("the edited instruction changed:\n%s", got)
		}
		if got := readFile(t, j.path(".config/livery/lock.json")); !strings.Contains(got, "v0.2.0") {
			t.Errorf("the lock does not record v0.2.0:\n%s", got)
		}

		out, _ = j.expect(1, "doctor")
		j.has(out, "finding  edited:", "~/.kiro/steering/go.md")

		out, _ = j.expect(1, "update")
		j.has(out, "edited  ~/.kiro/steering/go.md: ")
		if strings.Contains(out, "write   ") {
			t.Errorf("the second update wrote something:\n%s", out)
		}

		if err := os.Remove(ins); err != nil {
			t.Fatal(err)
		}
		j.expect(0, "update")
		if got := readFile(t, ins); !strings.Contains(got, "Go rules, version two.") || strings.Contains(got, "My own rule.") {
			t.Errorf("the release's version was not installed:\n%s", got)
		}
		out, _ = j.expect(0, "update")
		j.has(out, "up to date")
		j.expect(0, "doctor")
	})

	t.Run("J5 team and personal layers override the base", func(t *testing.T) {
		j := newJourney(t)
		j.expect(0, "setup", "--source", j.src, "--team", "web")
		if s := readFile(t, j.path(".kiro/skills/review/SKILL.md")); !strings.Contains(s, "accessibility") {
			t.Errorf("the web team's review skill is not installed:\n%s", s)
		}
		if !exists(j.path(".kiro/steering/css.md")) {
			t.Error("the web team's css instruction is not installed")
		}
		out, _ := j.expect(0, "doctor")
		j.has(out, "info     override:")

		writeFile(t, j.path(".config/livery/personal/instructions/go.md"), "My own Go rules.\n")
		j.expect(0, "update")
		if s := readFile(t, j.path(".kiro/steering/go.md")); !strings.Contains(s, "My own Go rules.") || strings.Contains(s, "standard library") {
			t.Errorf("the personal go instruction does not win:\n%s", s)
		}
		out, _ = j.expect(0, "doctor")
		j.has(out, "info     override:", "personal")

		cfgBefore := readFile(t, j.path(".config/livery/config.json"))
		_, stderr := j.expect(1, "setup", "--team", "api", "--team", "clash")
		j.has(stderr, "team:api: skills/openapi", "team:clash: skills/openapi")
		if got := readFile(t, j.path(".config/livery/config.json")); got != cfgBefore {
			t.Errorf("the rejected setup changed the config:\n%s\nwas:\n%s", got, cfgBefore)
		}
	})

	t.Run("J6 pinned release, next and stable channels", func(t *testing.T) {
		j := newJourney(t)
		j.expect(0, "setup", "--source", j.src, "--channel", "v0.1.0")
		commit(t, j.src, "v0.2.0", func() {
			writeFile(t, filepath.Join(j.src, "base/instructions/commits.md"), "Version two of the commit rules.\n")
		})
		out, _ := j.expect(0, "update")
		j.has(out, "up to date", "v0.1.0")
		commit(t, j.src, "", func() {
			writeFile(t, filepath.Join(j.src, "base/instructions/commits.md"), "Untagged commit rules.\n")
		})

		before := walk(t, j.home, ".cache/livery")
		out, _ = j.expect(0, "setup", "--channel", "next", "--plan")
		j.has(out, "next (", "write   ~/.kiro/steering/commits.md", "+Untagged commit rules.")
		sameWalk(t, "setup --channel next --plan", before, walk(t, j.home, ".cache/livery"))

		out, _ = j.expect(0, "setup", "--channel", "stable")
		j.has(out, "v0.2.0")
		if s := readFile(t, j.path(".kiro/steering/commits.md")); !strings.Contains(s, "Version two of the commit rules.") {
			t.Errorf("stable did not move to v0.2.0:\n%s", s)
		}
		out, _ = j.expect(0, "setup", "--channel", "next")
		j.has(out, "next (")
		if s := readFile(t, j.path(".kiro/steering/commits.md")); !strings.Contains(s, "Untagged commit rules.") {
			t.Errorf("next did not install the untagged commit:\n%s", s)
		}
	})

	t.Run("J7 uninstall restores the home", func(t *testing.T) {
		j := newJourney(t)
		before := walk(t, j.home)
		j.expect(0, "setup", "--source", j.src)
		if !exists(j.path(".kiro/settings/mcp.json")) || !exists(j.path(".claude.json")) {
			t.Fatal("setup wrote no mcp.json or .claude.json")
		}
		out, _ := j.expect(0, "uninstall")
		j.has(out, "kept    ~/.config/livery/config.json")
		// walk records directories too, so none may be left behind: the home must equal the
		// original, except the kept config and the source cache.
		sameWalk(t, "uninstall", before, walk(t, j.home, ".config/livery/config.json", ".cache/livery"))
		if exists(j.path(".config/livery/lock.json")) {
			t.Error("uninstall left the lock")
		}
		if !exists(j.path(".config/livery/config.json")) {
			t.Error("uninstall removed the config")
		}
	})

	t.Run("J8 the user's claude.json survives setup, update and uninstall", func(t *testing.T) {
		j := newJourney(t)
		orig := readFile(t, "testdata/claude.json")
		writeFile(t, j.path(".claude.json"), orig)

		// theirs asserts that every top-level key other than mcpServers, and every server not
		// from the source, has the bytes of the original file.
		theirs := func(step string) {
			t.Helper()
			want, got := rawMembers(t, orig), rawMembers(t, readFile(t, j.path(".claude.json")))
			for k, v := range want {
				if k == "mcpServers" {
					continue
				}
				if string(got[k]) != string(v) {
					t.Errorf("%s: top-level key %q changed:\n%s\nwas:\n%s", step, k, got[k], v)
				}
			}
			if len(got) != len(want) {
				t.Errorf("%s: %d top-level keys, want %d", step, len(got), len(want))
			}
			wantSrv, gotSrv := rawMembers(t, string(want["mcpServers"])), rawMembers(t, string(got["mcpServers"]))
			for _, name := range []string{"mine", "zeta"} {
				if string(gotSrv[name]) != string(wantSrv[name]) {
					t.Errorf("%s: server %q changed:\n%s\nwas:\n%s", step, name, gotSrv[name], wantSrv[name])
				}
			}
		}

		j.expect(0, "setup", "--source", j.src)
		theirs("setup")
		srv := rawMembers(t, string(rawMembers(t, readFile(t, j.path(".claude.json")))["mcpServers"]))
		if srv["docs"] == nil || srv["github"] == nil {
			t.Fatalf("setup did not add the source servers: %v", srv)
		}

		commit(t, j.src, "v0.2.0", func() {
			writeFile(t, filepath.Join(j.src, "base/mcp/docs.json"), `{"url": "https://docs2.example.com/mcp"}`+"\n")
		})
		out, _ := j.expect(0, "update")
		j.has(out, "set     ~/.claude.json mcpServers.docs")
		theirs("update")
		srv = rawMembers(t, string(rawMembers(t, readFile(t, j.path(".claude.json")))["mcpServers"]))
		if !strings.Contains(string(srv["docs"]), "docs2.example.com") {
			t.Errorf("update did not change the docs server: %s", srv["docs"])
		}

		j.expect(0, "uninstall")
		if got := readFile(t, j.path(".claude.json")); got != orig {
			t.Errorf("uninstall: ~/.claude.json is not the original:\n%s", got)
		}
	})
}

// rawMembers returns the top-level members of a JSON object with their value bytes as written.
func rawMembers(t *testing.T, data string) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		t.Fatalf("not a JSON object: %v\n%s", err, data)
	}
	return m
}

// A config with sources is refused like any config with an unknown key; setup leaves it as it is.
func TestSourcesConfigRefused(t *testing.T) {
	j := newJourney(t)
	cfgPath := j.path(".config/livery/config.json")
	writeFile(t, cfgPath, `{"sources":[{"name":"a","source":"x","channel":"stable","teams":[]}],"harnesses":["kiro"]}`)
	_, stderr := j.expect(2, "update")
	j.has(stderr, cfgPath, "not a valid livery config")
	_, stderr = j.expect(2, "source", "list")
	j.has(stderr, `unknown command "source"`)
	j.expect(2, "setup", "--source", j.src, "--harness", "kiro")
}

func TestList(t *testing.T) {
	j := newJourney(t)
	commit(t, j.src, "v0.1.1", func() { writeFile(t, filepath.Join(j.src, "teams/empty/README.md"), "x") })
	out, _ := j.expect(0, "list", "--source", j.src)
	j.has(out, "    mcp          docs\n", "  team web\n", "  team api\n", "  team clash\n", "  team empty\n    (empty)\n")
}
