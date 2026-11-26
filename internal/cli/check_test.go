package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"

	"github.com/Pnakoryn/livery/internal/content/contenttest"
	"github.com/Pnakoryn/livery/internal/engine/enginetest"
)

func runCheck(t *testing.T, args ...string) (int, string) {
	t.Helper()
	code, out, errs := runCLI(t, enginetest.Env(t), "", false, append([]string{"check"}, args...)...)
	return code, out + errs
}

func checkCopy(t *testing.T) string {
	t.Helper()
	return contenttest.Source(t, t.TempDir())
}

// hasAll reports whether s contains every part: tests of a message assert the file or flag it
// names, the next command and a key word of the reason, not the whole sentence.
func hasAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

func fakeToken() string { return "ghp_" + strings.Repeat("aB3dE5gH7j", 3) + "AbCdEf" }

func appendTo(t *testing.T, p, s string) {
	t.Helper()
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

func TestCheckClean(t *testing.T) {
	t.Parallel()
	code, out := runCheck(t, checkCopy(t))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	for _, w := range []string{"ok       manifest:", "ok       layer base:", "ok       render kiro:", "ok       render claude:", "ok       secrets:"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in %s", w, out)
		}
	}
}

func TestCheckSecretAndAllow(t *testing.T) {
	t.Parallel()
	dir := checkCopy(t)
	tok := fakeToken()
	appendTo(t, filepath.Join(dir, "base/skills/review/SKILL.md"), "\ntoken = "+tok+"\n")
	code, out := runCheck(t, dir)
	if code != 1 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if strings.Contains(out, tok) {
		t.Fatalf("token printed: %s", out)
	}
	m := regexp.MustCompile(`finding  secrets: base/skills/review/SKILL\.md:\d+: possible secret \(github-pat, fingerprint ([0-9a-f]{12})\)`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no finding line: %s", out)
	}
	allow := filepath.Join(dir, "secrets-allow")
	line := "base/skills/review/SKILL.md " + m[1] + "\n"
	if err := os.WriteFile(allow, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := runCheck(t, dir); code != 0 {
		t.Fatalf("allowed: exit %d: %s", code, out)
	}
	appendTo(t, allow, "base/x.md 0123456789ab\n")
	code, out = runCheck(t, dir)
	if code != 0 || strings.Count(out, "info     secrets: secrets-allow: base/x.md 0123456789ab is not needed any more") != 1 {
		t.Fatalf("unused: exit %d: %s", code, out)
	}
}

func TestCheckNoManifest(t *testing.T) {
	t.Parallel()
	dir := checkCopy(t)
	if err := os.Remove(filepath.Join(dir, "livery.json")); err != nil {
		t.Fatal(err)
	}
	code, out := runCheck(t, dir)
	if code != 1 || !strings.HasPrefix(out, "finding  manifest:") || !strings.Contains(out, "livery doctor") || strings.Count(out, "finding  ") != 1 {
		t.Fatalf("exit %d: %s", code, out)
	}
}

func TestCheckMalformedManifestRunsOthers(t *testing.T) {
	t.Parallel()
	dir := checkCopy(t)
	if err := os.WriteFile(filepath.Join(dir, "livery.json"), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	appendTo(t, filepath.Join(dir, "base/skills/review/SKILL.md"), "\ntoken = "+fakeToken()+"\n")
	code, out := runCheck(t, dir)
	if code != 1 || !strings.Contains(out, "finding  manifest:") || !strings.Contains(out, "possible secret (github-pat") {
		t.Fatalf("exit %d: %s", code, out)
	}
}

func TestCheckBrokenLayerAndSecret(t *testing.T) {
	t.Parallel()
	dir := checkCopy(t)
	if err := os.WriteFile(filepath.Join(dir, "base/mcp/bad.json"), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	appendTo(t, filepath.Join(dir, "base/skills/review/SKILL.md"), "\ntoken = "+fakeToken()+"\n")
	code, out := runCheck(t, dir)
	if code != 1 || !strings.Contains(out, "ok       manifest:") || !strings.Contains(out, "finding  layer base:") ||
		!strings.Contains(out, "bad.json") || !strings.Contains(out, "possible secret (github-pat") {
		t.Fatalf("exit %d: %s", code, out)
	}
}

func TestCheckAdapterNoteIsInfo(t *testing.T) {
	t.Parallel()
	dir := checkCopy(t)
	code, out := runCheck(t, dir)
	if code != 0 || !strings.Contains(out, "info     render opencode: instruction go applies to every file") || strings.Count(out, "instruction go applies") != 1 {
		t.Fatalf("exit %d: %s", code, out)
	}
}

func TestCheckBinaryAllowed(t *testing.T) {
	t.Parallel()
	dir := checkCopy(t)
	if err := os.WriteFile(filepath.Join(dir, "base/skills/review/logo.png"), []byte("\x00PNG"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := runCheck(t, dir)
	if code != 1 || !strings.Contains(out, "binary file, not scanned") || !strings.Contains(out, "secrets-allow") {
		t.Fatalf("exit %d: %s", code, out)
	}
	var line string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "logo.png") {
			line = l
		}
	}
	if strings.Contains(line, "${NAME}") || strings.Contains(line, "possible secret") {
		t.Fatalf("line: %s", line)
	}
	i := strings.Index(line, `add the line "`)
	allowLine := strings.SplitN(line[i+len(`add the line "`):], `"`, 2)[0]
	if err := os.WriteFile(filepath.Join(dir, "secrets-allow"), []byte(allowLine+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := runCheck(t, dir); code != 0 {
		t.Fatalf("allowed: exit %d: %s", code, out)
	}
}

func TestCheckBadMCP(t *testing.T) {
	t.Parallel()
	dir := checkCopy(t)
	if err := os.WriteFile(filepath.Join(dir, "base/mcp/bad.json"), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := runCheck(t, dir)
	if code != 1 || !strings.Contains(out, "finding  layer base:") || !strings.Contains(out, "bad.json") {
		t.Fatalf("exit %d: %s", code, out)
	}
}

func TestCheckUsage(t *testing.T) {
	t.Parallel()
	if code, out := runCheck(t, filepath.Join(t.TempDir(), "nope")); code != 2 {
		t.Fatalf("missing: exit %d: %s", code, out)
	}
	if code, out := runCheck(t, "a", "b"); code != 2 {
		t.Fatalf("two args: exit %d: %s", code, out)
	}
}

func TestCheckSkipsGit(t *testing.T) {
	t.Parallel()
	dir := checkCopy(t)
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git/config"), []byte("token = "+fakeToken()+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := runCheck(t, dir); code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
}

func TestCheckReportsLinkOutsideLayers(t *testing.T) {
	t.Parallel()
	dir := checkCopy(t)
	if err := os.Symlink("livery.json", filepath.Join(dir, "base", "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	code, out := runCheck(t, dir)
	if code != 1 || !strings.Contains(out, `finding  links: "base/AGENTS.md" in the shared repository is a link; links are not allowed`) {
		t.Fatalf("exit %d: %s", code, out)
	}
}

func TestCheckReportsLinkedBaseAndTeams(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"base", "teams"} {
		dir := checkCopy(t)
		if err := os.RemoveAll(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(t.TempDir(), filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
		code, out := runCheck(t, dir)
		if code != 1 || !strings.Contains(out, `links: "`+name+`" in the shared repository is a link`) {
			t.Fatalf("%s: exit %d: %s", name, code, out)
		}
	}
}

func TestCheckLinkedRootScansSecrets(t *testing.T) {
	t.Parallel()
	dir := checkCopy(t)
	appendTo(t, filepath.Join(dir, "base/skills/review/SKILL.md"), "\ntoken = "+fakeToken()+"\n")
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	code, out := runCheck(t, link)
	if code != 1 || !strings.Contains(out, "possible secret (github-pat") {
		t.Fatalf("exit %d: %s", code, out)
	}
}

func TestCheckBinaryNotClean(t *testing.T) {
	t.Parallel()
	dir := checkCopy(t)
	if err := os.WriteFile(filepath.Join(dir, "base/skills/review/client.p12"), []byte("\x30\x82\x00\x01"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "base/skills/review/blob.bin"), []byte("a\x00b"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := runCheck(t, dir)
	if code != 1 || !strings.Contains(out, "client.p12:0: possible secret (pkcs12-file") || !strings.Contains(out, "blob.bin: binary file, not scanned") {
		t.Fatalf("exit %d: %s", code, out)
	}
}

func TestCheckReportsFifo(t *testing.T) {
	t.Parallel()
	dir := checkCopy(t)
	if err := syscall.Mkfifo(filepath.Join(dir, "base", "pipe"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := runCheck(t, dir)
	if code != 1 || !strings.Contains(out, `finding  links: "base/pipe" in the shared repository has an unsupported file type`) {
		t.Fatalf("exit %d: %s", code, out)
	}
}
