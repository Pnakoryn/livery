package apply

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/exit"
)

func mustParse(t *testing.T, data string) *Doc {
	t.Helper()
	d, err := ParseDoc("p.json", []byte(data))
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	return d
}

// assertOrder fails unless every needle occurs in s, in the given order.
func assertOrder(t *testing.T, s string, needles ...string) {
	t.Helper()
	last := -1
	for _, n := range needles {
		i := strings.Index(s, n)
		if i < 0 {
			t.Fatalf("%q not found in:\n%s", n, s)
		}
		if i <= last {
			t.Fatalf("%q out of order in:\n%s", n, s)
		}
		last = i
	}
}

const sample = `{"z":1,"mcpServers":{"b":{"command":"x"},"a":{"url":"u"}},"y":[1,2]}` + "\n"

func TestSetAppendsKeepingOrder(t *testing.T) {
	d := mustParse(t, sample)
	if err := d.Set("mcpServers", "c", json.RawMessage(`{"command":"c"}`)); err != nil {
		t.Fatal(err)
	}
	got := string(d.Bytes())
	assertOrder(t, got, `"z"`, `"mcpServers"`, `"b"`, `"a"`, `"c"`, `"y"`)
	if !strings.HasSuffix(got, "}\n") || !strings.Contains(got, "\n  \"z\": 1,") {
		t.Fatalf("not two-space indented with trailing newline:\n%s", got)
	}
}

func TestSetExistingInPlaceAndRemove(t *testing.T) {
	d := mustParse(t, sample)
	if err := d.Set("mcpServers", "b", json.RawMessage(`{"command":"new"}`)); err != nil {
		t.Fatal(err)
	}
	got := string(d.Bytes())
	assertOrder(t, got, `"b"`, `"new"`, `"a"`)
	if strings.Contains(got, `"x"`) {
		t.Fatalf("old value kept:\n%s", got)
	}
	if err := d.Remove("mcpServers", "a"); err != nil {
		t.Fatal(err)
	}
	e, err := d.Entries("mcpServers")
	if err != nil {
		t.Fatal(err)
	}
	if len(e) != 1 || string(e["b"]) != `{"command":"new"}` {
		t.Fatalf("entries = %v", e)
	}
	if err := d.Remove("mcpServers", "missing"); err != nil {
		t.Fatalf("remove missing: %v", err)
	}
	if err := d.Remove("other", "missing"); err != nil {
		t.Fatalf("remove under missing key: %v", err)
	}
	if err := d.Remove("mcpServers", "b"); err != nil {
		t.Fatal(err)
	}
	if got := string(d.Bytes()); !strings.Contains(got, `"mcpServers": {}`) {
		t.Fatalf("last entry removed:\n%s", got)
	}
}

func TestSetNewKey(t *testing.T) {
	d := mustParse(t, `{"x":1}`)
	if err := d.Set("mcpServers", "s", json.RawMessage(`{"url":"u"}`)); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, string(d.Bytes()), `"x"`, `"mcpServers"`, `"s"`)

	for _, empty := range []string{"", "  \n\t"} {
		d := mustParse(t, empty)
		if got := string(d.Bytes()); got != "{}\n" {
			t.Fatalf("empty doc = %q", got)
		}
		if e, err := d.Entries("mcpServers"); err != nil || len(e) != 0 {
			t.Fatalf("Entries of empty = %v, %v", e, err)
		}
		if err := d.Set("mcpServers", "s", json.RawMessage(`{"url":"u"}`)); err != nil {
			t.Fatal(err)
		}
		want := "{\n  \"mcpServers\": {\n    \"s\": {\n      \"url\": \"u\"\n    }\n  }\n}\n"
		if got := string(d.Bytes()); got != want {
			t.Fatalf("got\n%s\nwant\n%s", got, want)
		}
	}
}

func TestUntouchedValuesKeepBytes(t *testing.T) {
	d := mustParse(t, `{"other":{"n":12345678901234567890,"f":1e3,"s":"<&\u00e9>"},"mcpServers":{}}`)
	if err := d.Set("mcpServers", "c", json.RawMessage(`{"command":"c"}`)); err != nil {
		t.Fatal(err)
	}
	got := string(d.Bytes())
	for _, want := range []string{`12345678901234567890`, `1e3`, `"<&\u00e9>"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("%s not kept:\n%s", want, got)
		}
	}
}

func TestSetThenRemoveIsIdentity(t *testing.T) {
	// 60 keys in a fixed, non-alphabetical order, with mcpServers among them.
	var b strings.Builder
	b.WriteString("{")
	for i := range 60 {
		if i > 0 {
			b.WriteString(",")
		}
		k := fmt.Sprintf("k%02d", (i*37)%60)
		switch i % 4 {
		case 0:
			fmt.Fprintf(&b, "%q: %d", k, i)
		case 1:
			fmt.Fprintf(&b, "%q: 1.50e%d", k, i)
		case 2:
			fmt.Fprintf(&b, "%q: {\"x\": [1, 2.0, null], \"z\": true, \"a\": \"v%d\"}", k, i)
		default:
			fmt.Fprintf(&b, "%q: \"s%d\"", k, i)
		}
		if i == 30 {
			b.WriteString(`, "mcpServers": {"q": {"command": "q"}, "b": {"url": "u"}}`)
		}
	}
	b.WriteString("}")
	src := b.String()
	want := string(mustParse(t, src).Bytes())

	for _, name := range []string{"new", "a", "zz", "m"} {
		d := mustParse(t, src)
		if err := d.Set("mcpServers", name, json.RawMessage(`{"command":"run","args":[1e2]}`)); err != nil {
			t.Fatal(err)
		}
		if string(d.Bytes()) == want {
			t.Fatalf("Set %s changed nothing", name)
		}
		if err := d.Remove("mcpServers", name); err != nil {
			t.Fatal(err)
		}
		if got := string(d.Bytes()); got != want {
			t.Fatalf("Set then Remove %s:\ngot\n%s\nwant\n%s", name, got, want)
		}
	}
}

func assertFinding(t *testing.T, err error) {
	t.Helper()
	var e *exit.Error
	if !errors.As(err, &e) || e.Code != exit.Finding {
		t.Fatalf("want exit.Finding, got %v", err)
	}
	if !strings.Contains(e.Message, "p.json") {
		t.Fatalf("message does not name p.json: %s", e.Message)
	}
}

func TestErrors(t *testing.T) {
	for _, data := range []string{"// c\n{}", "[]", "3", `{"a":1} {}`, `{"a":`} {
		_, err := ParseDoc("p.json", []byte(data))
		assertFinding(t, err)
	}
	d := mustParse(t, `{"mcpServers": 3}`)
	err := d.Set("mcpServers", "a", json.RawMessage(`{}`))
	assertFinding(t, err)
	if !strings.Contains(err.Error(), "mcpServers") {
		t.Fatalf("message does not name the key: %v", err)
	}
	_, err = d.Entries("mcpServers")
	assertFinding(t, err)
	assertFinding(t, d.Remove("mcpServers", "a"))
	assertFinding(t, mustParse(t, `{}`).Set("mcpServers", "a", json.RawMessage(`{`)))
}

func TestKeysKeptAsRawBytes(t *testing.T) {
	in := "{ \"a\\ud800\" : 1, \"b\xff\": {\"x\\ud800\": 2, \"\\u0041\": 3}}"
	d := mustParse(t, in)
	if err := d.Set("b", "new", json.RawMessage(`4`)); err != nil {
		t.Fatal(err)
	}
	got := string(d.Bytes())
	for _, want := range []string{"\"a\\ud800\"", "\"b\xff\"", "\"x\\ud800\"", "\"\\u0041\""} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q lost in:\n%s", want, got)
		}
	}
}

// claudeLayout is a file in the layout Claude Code writes itself: JSON.stringify(v, null, 2).
func claudeLayout(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "claude.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// outsideMcp splits s around the mcpServers member: what comes before and after it.
func outsideMcp(t *testing.T, s string) (before, after string) {
	t.Helper()
	const start = "\n  \"mcpServers\": {"
	i := strings.Index(s, start)
	if i < 0 {
		t.Fatalf("no mcpServers in:\n%s", s)
	}
	j := strings.Index(s[i:], "\n  }")
	if j < 0 {
		t.Fatalf("mcpServers does not end in:\n%s", s)
	}
	return s[:i], s[i+j+len("\n  }"):]
}

func TestClaudeLayoutKeepsEveryUntouchedByte(t *testing.T) {
	orig := claudeLayout(t)
	wantBefore, wantAfter := outsideMcp(t, orig)

	d := mustParse(t, orig)
	if got := string(d.Bytes()); got != orig {
		t.Fatalf("Bytes without a change differs from the original")
	}
	for _, name := range []string{"new", "mine"} { // a new entry and a replaced one
		d := mustParse(t, orig)
		if err := d.Set("mcpServers", name, json.RawMessage(`{"command":"c","args":["<&>"]}`)); err != nil {
			t.Fatal(err)
		}
		got := string(d.Bytes())
		before, after := outsideMcp(t, got)
		if before != wantBefore {
			t.Errorf("Set %s: bytes before mcpServers changed", name)
		}
		if after != wantAfter {
			t.Errorf("Set %s: bytes after mcpServers changed", name)
		}
		if !strings.Contains(got, "\"command\": \"c\"") {
			t.Errorf("Set %s: entry missing:\n%s", name, got)
		}
		if name == "new" {
			if err := d.Remove("mcpServers", name); err != nil {
				t.Fatal(err)
			}
			if got := string(d.Bytes()); got != orig {
				t.Errorf("Remove after Set: file is not the original")
			}
		}
	}

	d = mustParse(t, orig)
	if err := d.Remove("mcpServers", "mine"); err != nil {
		t.Fatal(err)
	}
	before, after := outsideMcp(t, string(d.Bytes()))
	if before != wantBefore || after != wantAfter {
		t.Error("Remove mine: bytes outside mcpServers changed")
	}
}

func TestOtherLayoutsAreReindented(t *testing.T) {
	orig := claudeLayout(t)
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(orig)); err != nil {
		t.Fatal(err)
	}
	layouts := map[string]string{
		"one line": compact.String(),
		"tabs":     indentWith(t, orig, "\t"),
		"4 spaces": indentWith(t, orig, "    "),
	}
	for name, src := range layouts {
		d := mustParse(t, src)
		got := d.Bytes()
		if string(got) != orig {
			t.Errorf("%s: not re-indented to two spaces", name)
		}
		var a, b any
		if err := json.Unmarshal([]byte(src), &a); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(got, &b); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(a, b) {
			t.Errorf("%s: parsed values differ", name)
		}
	}
}

func indentWith(t *testing.T, s, indent string) string {
	t.Helper()
	var b bytes.Buffer
	if err := json.Indent(&b, []byte(s), "", indent); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestBytesKeepsMissingTrailingNewline(t *testing.T) {
	src := "{\n  \"a\": 1,\n  \"mcpServers\": {}\n}"
	d := mustParse(t, src)
	if got := string(d.Bytes()); got != src {
		t.Fatalf("got %q", got)
	}
	if err := d.Set("mcpServers", "n", json.RawMessage(`{"url":"u"}`)); err != nil {
		t.Fatal(err)
	}
	if err := d.Remove("mcpServers", "n"); err != nil {
		t.Fatal(err)
	}
	if got := string(d.Bytes()); got != src {
		t.Fatalf("after set and remove: %q", got)
	}
}
