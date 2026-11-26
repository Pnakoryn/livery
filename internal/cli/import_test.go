package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/content"
	"github.com/Pnakoryn/livery/internal/engine/enginetest"
)

const importSecretValue = "s3cr3t-value-for-test"

func runImport(t *testing.T, env adapter.Env, stdin string, interactive bool, args ...string) (int, string, string) {
	t.Helper()
	return runCLI(t, env, stdin, interactive, append([]string{"import"}, args...)...)
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, data := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// kiroFolder fills ~/.kiro with a skill, three steering files and two MCP servers.
func kiroFolder(t *testing.T, env adapter.Env, headers string) {
	t.Helper()
	writeFiles(t, filepath.Join(env.Home, ".kiro"), map[string]string{
		"skills/review/SKILL.md": "review skill\n",
		"skills/lint/SKILL.md":   "lint skill\n",
		"steering/always.md":     "always on\n",
		"steering/go.md":         "---\ninclusion: fileMatch\nfileMatchPattern: \"**/*.go\"\n---\n\ngo rules\n",
		"steering/manual.md":     "---\ninclusion: manual\n---\n\nmanual rules\n",
		"settings/mcp.json": `{"mcpServers": {
  "github": {"command": "gh-mcp", "env": {"GITHUB_TOKEN": "` + importSecretValue + `"}},
  "docs": {"url": "https://docs.example.com/mcp"` + headers + `}
}}`,
	})
}

func TestImportKiro(t *testing.T) {
	t.Parallel()
	env := enginetest.Env(t)
	kiroFolder(t, env, "")
	d := filepath.Join(t.TempDir(), "src")
	code, out, errs := runImport(t, env, "", false, "--from", "kiro", "--into", d)
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	for _, rel := range []string{
		"livery.json",
		"base/skills/review/SKILL.md",
		"base/skills/lint/SKILL.md",
		"base/instructions/always.md",
		"base/instructions/go.md",
		"base/mcp/github.json",
		"base/mcp/docs.json",
		"base/kiro/steering/manual.md",
	} {
		if _, err := os.Stat(filepath.Join(d, rel)); err != nil {
			t.Errorf("%s: %v\n%s", rel, err, out)
		}
	}
	gh, _ := os.ReadFile(filepath.Join(d, "base/mcp/github.json"))
	if !strings.Contains(string(gh), `"${GITHUB_TOKEN}"`) {
		t.Errorf("github.json: %s", gh)
	}
	if !hasAll(out, "var     GITHUB_TOKEN:", "server github", "env.GITHUB_TOKEN") {
		t.Errorf("output: %s", out)
	}
	for p, data := range fileData(tree(t, d)) {
		if strings.Contains(data, importSecretValue) {
			t.Errorf("%s holds the secret value", p)
		}
	}
	if m, _ := os.ReadFile(filepath.Join(d, "livery.json")); string(m) != "{\"schema\": 1}\n" {
		t.Errorf("livery.json: %q", m)
	}
	items, err := content.LoadLayer(os.DirFS(filepath.Join(d, "base")), "base")
	if err != nil || len(items) != 7 {
		t.Fatalf("LoadLayer: %d items, %v", len(items), err)
	}
	for _, want := range []string{"livery check " + d, "livery setup --source " + d, "vX.Y.Z", "write   " + filepath.Join(d, "base/skills/review/SKILL.md")} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}

	// A second run adds nothing.
	code, out, errs = runImport(t, env, "", false, "--from", "kiro", "--into", d)
	if code != 0 || strings.Contains(out, "write ") || !strings.Contains(out, "same    "+filepath.Join(d, "base/mcp/github.json")) {
		t.Fatalf("second run: exit %d: %s%s", code, out, errs)
	}
}

func TestImportTeam(t *testing.T) {
	t.Parallel()
	env := enginetest.Env(t)
	kiroFolder(t, env, "")
	d := filepath.Join(t.TempDir(), "src")
	code, out, errs := runImport(t, env, "", false, "--from", "kiro", "--into", d, "--team", "web")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if _, err := os.Stat(filepath.Join(d, "teams/web/skills/review/SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(d, "base")); err == nil {
		t.Fatal("base written")
	}
	if code, _, errs := runImport(t, env, "", false, "--from", "kiro", "--into", d, "--team", "Bad_Team"); code != 2 || !strings.Contains(errs, "--team") {
		t.Fatalf("bad team: exit %d %s", code, errs)
	}
}

func TestImportPlan(t *testing.T) {
	t.Parallel()
	env := enginetest.Env(t)
	kiroFolder(t, env, "")
	d := filepath.Join(t.TempDir(), "src")
	code, out, errs := runImport(t, env, "", false, "--from", "kiro", "--into", d, "--plan")
	if code != 0 || !strings.Contains(out, "would write") {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	d2 := filepath.Join(t.TempDir(), "src")
	_, ran, _ := runImport(t, env, "", false, "--from", "kiro", "--into", d2)
	writes := func(s, dir string) []string {
		var res []string
		for _, l := range strings.Split(s, "\n") {
			if strings.HasPrefix(l, "write   ") {
				res = append(res, strings.TrimPrefix(l, "write   "+dir))
			}
		}
		return res
	}
	p, r := writes(out, d), writes(ran, d2)
	if len(p) == 0 || strings.Join(p, "\n") != strings.Join(r, "\n") {
		t.Fatalf("plan %v, run %v", p, r)
	}
}

func TestImportExistsWritesNothing(t *testing.T) {
	t.Parallel()
	env := enginetest.Env(t)
	kiroFolder(t, env, "")
	d := filepath.Join(t.TempDir(), "src")
	writeFiles(t, d, map[string]string{"base/skills/review/SKILL.md": "other\n"})
	before := tree(t, d)
	code, out, _ := runImport(t, env, "", false, "--from", "kiro", "--into", d)
	if code != 1 || !strings.Contains(out, "exists: "+filepath.Join(d, "base/skills/review/SKILL.md")) {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !equal(before, tree(t, d)) {
		t.Fatal("files written")
	}
}

func TestImportSymlinkBelowDir(t *testing.T) {
	t.Parallel()
	env := enginetest.Env(t)
	kiroFolder(t, env, "")
	d := filepath.Join(t.TempDir(), "src")
	elsewhere := t.TempDir()
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(d, "base")); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runImport(t, env, "", false, "--from", "kiro", "--into", d)
	if code != 1 || !strings.Contains(out, "symbolic link: "+filepath.Join(d, "base")) {
		t.Fatalf("exit %d: %s", code, out)
	}
	if len(fileData(tree(t, elsewhere))) != 0 {
		t.Fatal("written through the link")
	}
}

func TestImportSecret(t *testing.T) {
	t.Parallel()
	env := enginetest.Env(t)
	token := "gh" + "p_" + "aB3dE5fG7h" + "J9kLmN2pQr" + "S4tUvW6xYz" + "8bCdEf"
	// X-Github is not a header import replaces: the token is left to the secrets scan.
	kiroFolder(t, env, `, "headers": {"X-Github": "`+token+`"}`)
	d := filepath.Join(t.TempDir(), "src")
	code, out, _ := runImport(t, env, "", false, "--from", "kiro", "--into", d)
	if code != 1 || !strings.Contains(out, "possible secret") || strings.Contains(out, token) {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "remove it from "+filepath.Join(env.Home, ".kiro", "settings", "mcp.json")) {
		t.Errorf("fix text: %s", out)
	}
	if _, err := os.Stat(d); err == nil {
		t.Fatal("folder created")
	}
	m := regexp.MustCompile(`finding  (\S+):\d+: possible secret \([^,]+, fingerprint ([0-9a-f]{12})\)`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no finding line: %s", out)
	}
	writeFiles(t, d, map[string]string{"secrets-allow": m[1] + " " + m[2] + "\n"})
	code, out, errs := runImport(t, env, "", false, "--from", "kiro", "--into", d)
	if code != 0 {
		t.Fatalf("with allow: exit %d: %s%s", code, out, errs)
	}
}

func TestImportHeaderRefs(t *testing.T) {
	t.Parallel()
	env := enginetest.Env(t)
	token := "abcdefghij1234567890"
	kiroFolder(t, env, `, "headers": {"Authorization": "Bearer `+token+`", "X-Api-Key": "abc", "Cookie": "a=b", "Accept": "application/json", "X-Token": "pre-${DOCS_TOKEN}"}`)
	d := filepath.Join(t.TempDir(), "src")
	code, out, errs := runImport(t, env, "", false, "--from", "kiro", "--into", d)
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	docs, _ := os.ReadFile(filepath.Join(d, "base/mcp/docs.json"))
	for _, want := range []string{
		`"Authorization": "Bearer ${DOCS_AUTHORIZATION}"`,
		`"X-Api-Key": "${DOCS_X_API_KEY}"`,
		`"Cookie": "${DOCS_COOKIE}"`,
		`"Accept": "application/json"`,
		`"X-Token": "pre-${DOCS_TOKEN}"`,
	} {
		if !strings.Contains(string(docs), want) {
			t.Errorf("docs.json lacks %s:\n%s", want, docs)
		}
	}
	if !hasAll(out, "var     DOCS_AUTHORIZATION:", "server docs", "headers.Authorization") {
		t.Errorf("output: %s", out)
	}
	if strings.Contains(out+errs, token) {
		t.Errorf("output holds the token: %s%s", out, errs)
	}
	for p, data := range fileData(tree(t, d)) {
		if strings.Contains(data, token) || strings.Contains(data, `"abc"`) || strings.Contains(data, "a=b") {
			t.Errorf("%s holds a header value: %s", p, data)
		}
	}
}

func TestImportURLPassword(t *testing.T) {
	t.Parallel()
	env := enginetest.Env(t)
	kiroFolder(t, env, "")
	writeFiles(t, filepath.Join(env.Home, ".kiro"), map[string]string{
		"settings/mcp.json": `{"mcpServers": {"docs": {"url": "https://user:pw@example.com/mcp"}}}`,
	})
	d := filepath.Join(t.TempDir(), "src")
	code, out, errs := runImport(t, env, "", false, "--from", "kiro", "--into", d)
	if code != 1 || !strings.Contains(out, "mcp/docs") || !strings.Contains(out, "password") {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if strings.Contains(out+errs, "pw") {
		t.Errorf("output holds the password: %s%s", out, errs)
	}
	if _, err := os.Stat(d); err == nil {
		t.Fatal("folder created")
	}
}

func TestImportLeavesOutInstalled(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env := enginetest.Env(t)
	withKiro(t, env)
	if code, out, errs := runSetup(t, env, "", false, "--source", src); code != 0 {
		t.Fatalf("setup: exit %d: %s%s", code, out, errs)
	}
	writeFiles(t, filepath.Join(env.Home, ".kiro"), map[string]string{"skills/mine/SKILL.md": "mine\n"})
	d := filepath.Join(t.TempDir(), "src")
	code, out, errs := runImport(t, env, "", false, "--from", "kiro", "--into", d)
	if code != 0 || !hasAll(out, "left out", "3 items", "installed") {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	got := fileData(tree(t, d))
	if _, ok := got[filepath.Join(d, "base/skills/mine/SKILL.md")]; !ok || len(got) != 2 {
		t.Fatalf("written: %v", got)
	}
}

func TestImportAllInstalledIsNothing(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env := enginetest.Env(t)
	withKiro(t, env)
	if code, out, errs := runSetup(t, env, "", false, "--source", src); code != 0 {
		t.Fatalf("setup: exit %d: %s%s", code, out, errs)
	}
	d := filepath.Join(t.TempDir(), "src")
	code, out, errs := runImport(t, env, "", false, "--from", "kiro", "--into", d)
	if code != 0 || !strings.Contains(out, "nothing to import from "+filepath.Join(env.Home, ".kiro")) || strings.Contains(out, "next steps") {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if _, err := os.Stat(d); err == nil {
		t.Fatal("folder created")
	}
}

func TestImportOnly(t *testing.T) {
	t.Parallel()
	env := enginetest.Env(t)
	kiroFolder(t, env, "")
	d := filepath.Join(t.TempDir(), "src")
	code, out, errs := runImport(t, env, "", false, "--from", "kiro", "--into", d, "--only", "skill/review")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	got := fileData(tree(t, d))
	if _, ok := got[filepath.Join(d, "base/skills/review/SKILL.md")]; !ok || len(got) != 2 {
		t.Fatalf("written: %v", got)
	}
	d2 := filepath.Join(t.TempDir(), "src")
	code, _, errs = runImport(t, env, "", false, "--from", "kiro", "--into", d2, "--only", "skill/nope")
	if code != 2 || !strings.Contains(errs, "--only skill/nope matches nothing in "+filepath.Join(env.Home, ".kiro")+"; the items: ") || !strings.Contains(errs, "skill/review") {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if _, err := os.Stat(d2); err == nil {
		t.Fatal("folder created")
	}
}

func TestImportInteractive(t *testing.T) {
	t.Parallel()
	env := enginetest.Env(t)
	kiroFolder(t, env, "")
	d := filepath.Join(t.TempDir(), "src")
	// harness 1 (kiro), the folder, then item 1 only.
	code, out, errs := runImport(t, env, "1\n"+d+"\n1\n", true)
	if code != 0 || !strings.Contains(out, "Import which (numbers, comma-separated, empty for all): ") {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if got := fileData(tree(t, d)); len(got) != 2 {
		t.Fatalf("written: %v", got)
	}
}

// An empty answer at "Import which" imports every item, as --from without --only does.
func TestImportInteractiveEmptyAnswerImportsAll(t *testing.T) {
	t.Parallel()
	env := enginetest.Env(t)
	kiroFolder(t, env, "")
	all := filepath.Join(t.TempDir(), "all")
	if code, out, errs := runImport(t, env, "", false, "--from", "kiro", "--into", all); code != 0 {
		t.Fatalf("--from kiro: exit %d: %s%s", code, out, errs)
	}
	d := filepath.Join(t.TempDir(), "src")
	code, out, errs := runImport(t, env, "1\n"+d+"\n\n", true)
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	want := map[string]bool{}
	for p := range fileData(tree(t, all)) {
		rel, _ := filepath.Rel(all, p)
		want[rel] = true
	}
	got := map[string]bool{}
	for p := range fileData(tree(t, d)) {
		rel, _ := filepath.Rel(d, p)
		got[rel] = true
	}
	if len(want) < 6 || !reflect.DeepEqual(got, want) {
		t.Fatalf("empty answer wrote %v, --from kiro wrote %v", got, want)
	}
}

func TestImportUsage(t *testing.T) {
	t.Parallel()
	env := enginetest.Env(t)
	kiroFolder(t, env, "")
	code, _, errs := runImport(t, env, "", false, "--into", t.TempDir())
	if code != 2 || !strings.Contains(errs, "--from") {
		t.Fatalf("no --from: exit %d: %s", code, errs)
	}
	if code, _, errs := runImport(t, env, "", false, "--from", "cursor", "--into", t.TempDir()); code != 2 || !strings.Contains(errs, "kiro") {
		t.Fatalf("unknown harness: exit %d: %s", code, errs)
	}
	if code, _, errs := runImport(t, env, "", false, "--from", "kiro"); code != 2 || !strings.Contains(errs, "--into") {
		t.Fatalf("no --into: exit %d: %s", code, errs)
	}
}

func TestImportNothing(t *testing.T) {
	t.Parallel()
	env := enginetest.Env(t)
	d := filepath.Join(t.TempDir(), "src")
	code, out, errs := runImport(t, env, "", false, "--from", "kiro", "--into", d)
	if code != 0 || !strings.Contains(out, "nothing to import from "+filepath.Join(env.Home, ".kiro")) {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if _, err := os.Stat(d); err == nil {
		t.Fatal("folder created")
	}
}

func TestImportOpencode(t *testing.T) {
	t.Parallel()
	env := enginetest.Env(t)
	writeFiles(t, filepath.Join(env.Home, ".config", "opencode"), map[string]string{
		"skills/review/SKILL.md": "review skill\n",
		"opencode.json":          `{"mcp": {"servers": {"docs": {"type": "remote", "url": "https://docs.example.com/mcp"}}}}`,
	})
	d := filepath.Join(t.TempDir(), "src")
	code, out, errs := runImport(t, env, "", false, "--from", "opencode", "--into", d)
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	for _, rel := range []string{"base/skills/review/SKILL.md", "base/mcp/docs.json"} {
		if _, err := os.Stat(filepath.Join(d, rel)); err != nil {
			t.Errorf("%s: %v\n%s", rel, err, out)
		}
	}
}

func TestImportURLPasswordReference(t *testing.T) {
	t.Parallel()
	env := enginetest.Env(t)
	writeFiles(t, filepath.Join(env.Home, ".config", "opencode"), map[string]string{
		"opencode.json": `{"mcp": {"servers": {"docs": {"type": "remote", "url": "https://user:{env:DOCS_PW}@example.com/mcp"}}}}`,
	})
	d := filepath.Join(t.TempDir(), "src")
	code, out, errs := runImport(t, env, "", false, "--from", "opencode", "--into", d)
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	docs, _ := os.ReadFile(filepath.Join(d, "base/mcp/docs.json"))
	if !strings.Contains(string(docs), "user:${DOCS_PW}@") {
		t.Errorf("docs.json: %s", docs)
	}
}

func TestImportHeaderVariableCollision(t *testing.T) {
	t.Parallel()
	env := enginetest.Env(t)
	writeFiles(t, filepath.Join(env.Home, ".kiro"), map[string]string{
		"settings/mcp.json": `{"mcpServers": {
  "a-b": {"url": "https://a.example.com", "headers": {"X-Key": "one"}},
  "a": {"url": "https://b.example.com", "headers": {"B-X-Key": "two"}}
}}`,
	})
	d := filepath.Join(t.TempDir(), "src")
	code, out, errs := runImport(t, env, "", false, "--from", "kiro", "--into", d)
	if code != 2 || !strings.Contains(errs, "A_B_X_KEY") || !strings.Contains(errs, "mcp/a-b") || !strings.Contains(errs, "mcp/a ") {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if _, err := os.Stat(d); err == nil {
		t.Fatal("folder created")
	}
}

// A header variable and an env key with the same name are a finding, whichever server is read
// first.
func TestImportHeaderEnvCollision(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ servers, want string }{
		{`"a": {"command": "x", "env": {"B_X_API_KEY": "v"}},
  "b": {"url": "https://b.example.com", "headers": {"X-Api-Key": "two"}}`, "B_X_API_KEY"},
		{`"a": {"url": "https://example.com", "headers": {"X-Token": "1"}},
  "b": {"command": "x", "env": {"A_X_TOKEN": "1"}}`, "mcp/b: the env key A_X_TOKEN is already used by a header variable of mcp/a"},
	} {
		env := enginetest.Env(t)
		writeFiles(t, filepath.Join(env.Home, ".kiro"), map[string]string{
			"settings/mcp.json": `{"mcpServers": {` + c.servers + `}}`,
		})
		d := filepath.Join(t.TempDir(), "src")
		code, out, _ := runImport(t, env, "", false, "--from", "kiro", "--into", d)
		if code != 1 || !strings.Contains(out, c.want) {
			t.Errorf("exit %d, want %q: %s", code, c.want, out)
		}
	}
}

func TestImportUnscannedFiles(t *testing.T) {
	t.Parallel()
	env := enginetest.Env(t)
	writeFiles(t, filepath.Join(env.Home, ".kiro"), map[string]string{
		"skills/review/SKILL.md":   "review skill\n",
		"skills/review/blob.bin":   "a\x00b",
		"skills/review/client.p12": "\x30\x82\x00\x01",
	})
	d := filepath.Join(t.TempDir(), "src")
	code, out, errs := runImport(t, env, "", false, "--from", "kiro", "--into", d)
	if strings.Contains(out, "unscanned-binary") {
		t.Errorf("binary finding says possible secret: %s", out)
	}
	if code != 1 || !strings.Contains(out, "blob.bin: binary file, not scanned") || !strings.Contains(out, "client.p12:0: possible secret (pkcs12-file") {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	var allow strings.Builder
	for _, m := range regexp.MustCompile("add the line `([^`]+)` to").FindAllStringSubmatch(out, -1) {
		allow.WriteString(m[1] + "\n")
	}
	if strings.Count(allow.String(), "\n") != 2 {
		t.Fatalf("fix lines: %s", out)
	}
	writeFiles(t, d, map[string]string{"secrets-allow": allow.String()})
	code, out, errs = runImport(t, env, "", false, "--from", "kiro", "--into", d)
	if code != 0 {
		t.Fatalf("with allow: exit %d: %s%s", code, out, errs)
	}
}

func TestImportNotesAndVars(t *testing.T) {
	t.Parallel()
	env := enginetest.Env(t)
	mcp := filepath.Join(env.Home, ".kiro", "settings", "mcp.json")
	writeFiles(t, filepath.Join(env.Home, ".kiro"), map[string]string{
		"settings/mcp.json":  `{"mcpServers": {"docs": {"command": "x", "disabled": false, "args": ["${JIRA_BASE}"], "env": {"DOCS_TOKEN": "` + importSecretValue + `"}}}}`,
		"steering/manual.md": "---\ninclusion: manual\n---\n\nbody\n",
	})
	d := filepath.Join(t.TempDir(), "src")
	code, out, errs := runImport(t, env, "", false, "--from", "kiro", "--into", d)
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	for _, want := range []string{
		"note    " + mcp + ": server docs: imported without disabled",
		"imported as a Kiro-only file",
		"var     DOCS_TOKEN: server docs, env.DOCS_TOKEN (the value was replaced by ${DOCS_TOKEN})",
		"var     JIRA_BASE: server docs, args (already a reference)",
		"everyone who installs this needs these variables set",
		"(kept, yours)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "skip    ") || strings.Contains(out, importSecretValue) {
		t.Errorf("unexpected skip line or secret in:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(d, "base/mcp/docs.json")); err != nil {
		t.Error(err)
	}
	if _, err := os.Stat(filepath.Join(d, "base/kiro/steering/manual.md")); err != nil {
		t.Error(err)
	}
}

// A value import replaced is reported where it was (env.<key> of that server), also when the
// name is referenced earlier, in args or in another server.
func TestImportVarLineNamesTheReplacedPlace(t *testing.T) {
	t.Parallel()
	m := &mcpVars{owner: map[string]string{}, header: map[string]bool{}, info: map[string]varInfo{}}
	for _, it := range []content.Item{
		{Kind: content.MCP, Name: "a", Server: []byte(`{"command":"x","args":["${TOK}"]}`)},
		{Kind: content.MCP, Name: "b", Server: []byte(`{"command":"y","args":["--t","${API_KEY}"],"env":{"API_KEY":"lit-value-123","TOK":"other-literal"}}`)},
	} {
		if _, err := m.add(it); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []string{"API_KEY", "TOK"} {
		if got, want := m.info[n], (varInfo{server: "b", place: "env." + n, replaced: true}); got != want {
			t.Errorf("%s: got %+v, want %+v", n, got, want)
		}
	}
}
