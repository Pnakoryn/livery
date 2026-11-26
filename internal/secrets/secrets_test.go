package secrets

import (
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/exit"
)

// Fake secrets are built by concatenation so that no test file holds a string a scanner flags.

func fakeGitHubToken() string {
	return "gh" + "p_" + "aB3dE5fG7h" + "J9kLmN2pQr" + "S4tUvW6xYz" + "8bCdEf"
}

func fakeAWSKeyID() string {
	return "AK" + "IA" + "QZ7RT2" + "XW4MPL" + "6KVB"
}

func fakePEM() string {
	body := strings.Repeat("MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQC7"+"\n", 3)
	return "-----BEGIN " + "RSA PRIVATE KEY-----\n" + body + "-----END " + "RSA PRIVATE KEY-----\n"
}

func rules(fs []Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Rule)
	}
	return out
}

func TestRulesCompile(t *testing.T) {
	rs, err := compile(rulesJSON)
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, r := range rs.rules {
		have[r.id] = true
	}
	for _, id := range []string{"github-pat", "aws-access-token"} {
		if !have[id] {
			t.Errorf("rule %q missing from the rule set", id)
		}
	}
	if _, err := loadRules(); err != nil {
		t.Fatal(err)
	}
}

func TestGitHubTokenInMarkdown(t *testing.T) {
	tok := fakeGitHubToken()
	text := "# Setup\n\nPaste " + tok + " into the field.\n"
	got := Scan("docs/setup.md", []byte(text))
	if len(got) != 1 || got[0].Rule != "github-pat" || got[0].Line != 3 || got[0].Path != "docs/setup.md" {
		t.Fatalf("got %+v, want one github-pat in line 3", got)
	}
	if got[0].Fingerprint != fingerprint(tok) || len(got[0].Fingerprint) != 12 {
		t.Errorf("fingerprint %q", got[0].Fingerprint)
	}
}

func TestAWSAccessKey(t *testing.T) {
	got := Scan("notes.txt", []byte("aws id "+fakeAWSKeyID()+" here\n"))
	if len(got) != 1 || got[0].Rule != "aws-access-token" || got[0].Line != 1 {
		t.Fatalf("got %+v, want one aws-access-token", got)
	}
}

func TestPrivateKey(t *testing.T) {
	got := Scan("key.txt", []byte("before\n"+fakePEM()))
	if len(got) != 1 || got[0].Rule != "private-key" || got[0].Line != 2 {
		t.Fatalf("got %+v, want one private-key in line 2", got)
	}
}

func TestVariableReferences(t *testing.T) {
	for _, text := range []string{
		`{"env": {"GITHUB_TOKEN": "${GITHUB_TOKEN}"}}`,
		`"Authorization": "Bearer {env:TOKEN}"`,
		`{"headers": {"Authorization": "Bearer {env:API_TOKEN}"}}`,
	} {
		if got := Scan("mcp/github.json", []byte(text)); got != nil {
			t.Errorf("%s: got %+v, want none", text, rules(got))
		}
	}
}

func TestProseGivesNothing(t *testing.T) {
	prose := "Keep commits small. Each commit message names the reason for the change.\n" +
		"The API key for the docs server is read from the environment, never from a file.\n" +
		"Run the tests before you push; the token in the README is a placeholder.\n"
	if got := Scan("instructions/commits.md", []byte(prose)); got != nil {
		t.Errorf("got %+v, want none", got)
	}
}

func TestBinaryReportedUnscanned(t *testing.T) {
	got := Scan("a.txt", []byte("x\x00y "+fakeGitHubToken()+"\n"))
	if len(got) != 1 || got[0].Rule != UnscannedBinary || got[0].Line != 0 {
		t.Errorf("got %+v, want one unscanned-binary", got)
	}
}

func TestBinaryPKCS12FilenameRule(t *testing.T) {
	got := Scan("certs/client.p12", []byte("\x30\x82\x00"))
	if len(got) != 1 || got[0].Rule != "pkcs12-file" {
		t.Errorf("got %+v, want pkcs12-file", got)
	}
}

func TestLargeTextScanned(t *testing.T) {
	big := []byte(strings.Repeat("a", 6<<20) + " " + fakeGitHubToken())
	if got := Scan("a.txt", big); len(got) != 1 {
		t.Errorf("got %+v, want one finding", got)
	}
}

func TestLowEntropyDropped(t *testing.T) {
	low := "gh" + "p_" + strings.Repeat("a", 36)
	if got := Scan("a.md", []byte("token "+low+"\n")); got != nil {
		t.Errorf("got %+v, want none", got)
	}
}

func TestPathRuleWithoutRegex(t *testing.T) {
	got := Scan("certs/client.p12", []byte("whatever"))
	if len(got) != 1 || got[0].Rule != "pkcs12-file" || got[0].Line != 0 || got[0].Fingerprint != fingerprint("certs/client.p12") {
		t.Fatalf("got %+v, want pkcs12-file line 0", got)
	}
}

func TestAllowlists(t *testing.T) {
	// The aws-access-token rule allows keys ending in EXAMPLE.
	ex := "AK" + "IA" + "QZ7RT2XW4" + "EXAMPLE"
	if got := Scan("a.md", []byte(ex+"\n")); got != nil {
		t.Errorf("EXAMPLE key: got %+v", got)
	}
	// The global allowlist drops lock files by path.
	if got := Scan("web/package-lock.json", []byte(fakeGitHubToken())); got != nil {
		t.Errorf("lock file: got %+v", got)
	}
	// The github-pat rule's own path allowlist.
	if got := Scan("node/@octokit/auth-token/README.md", []byte(fakeGitHubToken())); got != nil {
		t.Errorf("octokit readme: got %+v", got)
	}

	c := &candidate{path: "x.bb", secret: "s", match: "m", line: "LICENSE = \"MIT\""}
	and := allowlist{and: true, target: "line", regexes: compileT(t, `LICENSE`), paths: compileT(t, `\.bb$`)}
	if !and.allows(c) {
		t.Error("AND: both hold, want allowed")
	}
	c.path = "x.txt"
	if and.allows(c) {
		t.Error("AND: path fails, want not allowed")
	}
	or := and
	or.and = false
	if !or.allows(c) {
		t.Error("OR: line holds, want allowed")
	}
	stop := allowlist{stopwords: []string{"abc"}}
	if !stop.allows(&candidate{secret: "xxABCxx"}) || stop.allows(&candidate{secret: "xyz"}) {
		t.Error("stopwords")
	}
}

func compileT(t *testing.T, exprs ...string) []*regexp.Regexp {
	t.Helper()
	res, err := compileAll("test", exprs)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestDedupeAndOrder(t *testing.T) {
	got := dedupe([]Finding{
		{Line: 2, Rule: "b", Fingerprint: "f1"},
		{Line: 1, Rule: "z", Fingerprint: "f2"},
		{Line: 2, Rule: "a", Fingerprint: "f1"},
		{Line: 2, Rule: "c", Fingerprint: "f3"},
	})
	want := []Finding{{Line: 1, Rule: "z", Fingerprint: "f2"}, {Line: 2, Rule: "a", Fingerprint: "f1"}, {Line: 2, Rule: "c", Fingerprint: "f3"}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestSecretNotInFinding(t *testing.T) {
	tok := fakeGitHubToken()
	for _, f := range Scan("a.md", []byte(tok)) {
		if strings.Contains(f.Path+f.Rule+f.Fingerprint, tok) {
			t.Errorf("secret in finding %+v", f)
		}
	}
}

func TestScanSpeed(t *testing.T) {
	para := "The team keeps its shared setup in one repository. A change goes through review, " +
		"and the release owner tags it when the tests pass. Access to the server uses a key " +
		"from the password manager; the token is never written to a file in the repository.\n"
	var b strings.Builder
	for b.Len() < 1<<20 {
		b.WriteString(para)
	}
	got := Scan("prose.md", []byte(b.String()))
	if got != nil {
		t.Errorf("got %d findings in prose, first %+v", len(got), got[0])
	}
}

func TestAllow(t *testing.T) {
	data := "# accepted\n\ndocs/a.md 0123456789ab\nskills/x/SKILL.md abcdefabcdef\n"
	a, err := ParseAllow(".livery-allow", []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if !a.Allows(Finding{Path: "docs/a.md", Fingerprint: "0123456789ab"}) {
		t.Error("listed pair: want true")
	}
	if a.Allows(Finding{Path: "docs/b.md", Fingerprint: "0123456789ab"}) {
		t.Error("other path: want false")
	}
	if u := a.Unused(); len(u) != 1 || u[0] != "skills/x/SKILL.md abcdefabcdef" {
		t.Errorf("unused: %v", u)
	}
	if (Allow{}).Allows(Finding{Path: "a", Fingerprint: "b"}) || (Allow{}).Unused() != nil {
		t.Error("zero Allow")
	}
}

func TestAllowErrors(t *testing.T) {
	for _, tc := range []struct{ data, want string }{
		{"# c\ndocs/a.md\n", ".livery-allow:2:"},
		{"a b c\n", ".livery-allow:1:"},
		{"\n\na NOTHEX\n", ".livery-allow:3:"},
	} {
		_, err := ParseAllow(".livery-allow", []byte(tc.data))
		var e *exit.Error
		if !errors.As(err, &e) || e.Code != exit.Usage || !strings.Contains(e.Message, tc.want) {
			t.Errorf("%q: got %v, want usage error with %q", tc.data, err, tc.want)
		}
	}
}

func TestSameSecretTwoRulesOneFinding(t *testing.T) {
	got := Scan("a.md", []byte("token: "+fakeGitHubToken()+"\n"))
	if len(got) != 1 || got[0].Rule != "github-pat" {
		t.Fatalf("got %+v, want one finding, rule github-pat (specific beats generic-api-key)", got)
	}
}

func TestOwnSourcesGiveNothing(t *testing.T) {
	for _, name := range []string{"secrets.go", "allow.go", "secrets_test.go"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if got := Scan("internal/secrets/"+name, data); got != nil {
			t.Errorf("%s: got %+v, want none", name, got)
		}
	}
}
