package importer

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/content"
)

func write(t *testing.T, p, data string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
}

func TestSkipAndRel(t *testing.T) {
	b := &Base{Root: "/r"}
	b.Skip("/r/a/b", "why")
	if len(b.Skipped) != 1 || b.Skipped[0].Path != "/r/a/b" || b.Skipped[0].Reason != "why" {
		t.Fatalf("Skipped = %+v", b.Skipped)
	}
	if got := b.Rel("/r/a/b"); got != "a/b" {
		t.Errorf("Rel = %q", got)
	}
	if got := (&Base{Root: "rel"}).Rel("/abs/x"); got != "/abs/x" {
		t.Errorf("Rel without relation = %q", got)
	}
}

func TestReadDir(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "d", "f"), "x", 0o644)
	b := &Base{Root: dir}
	if n := len(b.ReadDir(filepath.Join(dir, "d"))); n != 1 {
		t.Errorf("entries = %d", n)
	}
	if b.ReadDir(filepath.Join(dir, "missing")) != nil || b.ReadDir(filepath.Join(dir, "d", "f")) != nil {
		t.Error("missing dir or file must give nothing")
	}
	if len(b.Skipped) != 0 {
		t.Errorf("Skipped = %+v", b.Skipped)
	}
	if err := os.Symlink(filepath.Join(dir, "d"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if b.ReadDir(filepath.Join(dir, "link")) != nil || len(b.Skipped) != 1 || b.Skipped[0].Reason != SymlinkReason {
		t.Errorf("linked dir: %+v", b.Skipped)
	}
}

func TestReadDirUnreadable(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads everything")
	}
	dir := filepath.Join(t.TempDir(), "d")
	write(t, filepath.Join(dir, "f"), "x", 0o644)
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	b := &Base{Root: filepath.Dir(dir)}
	if b.ReadDir(dir) != nil || len(b.Skipped) != 1 || !strings.HasPrefix(b.Skipped[0].Reason, "cannot read: ") {
		t.Errorf("Skipped = %+v", b.Skipped)
	}
}

func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "x.sh"), "#!/bin/sh", 0o755)
	write(t, filepath.Join(dir, "y"), "data", 0o644)
	f, err := ReadFile(filepath.Join(dir, "x.sh"))
	if err != nil || !f.Exec || string(f.Data) != "#!/bin/sh" {
		t.Errorf("x.sh = %+v, %v", f, err)
	}
	f, err = ReadFile(filepath.Join(dir, "y"))
	if err != nil || f.Exec || string(f.Data) != "data" {
		t.Errorf("y = %+v, %v", f, err)
	}
	if _, err := ReadFile(filepath.Join(dir, "none")); err == nil {
		t.Error("missing file must fail")
	}
}

func TestSkills(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "skills")
	write(t, filepath.Join(dir, "good", "SKILL.md"), "# s", 0o644)
	write(t, filepath.Join(dir, "good", "sub", "run.sh"), "#!/bin/sh", 0o755)
	write(t, filepath.Join(dir, "Bad Name", "SKILL.md"), "x", 0o644)
	write(t, filepath.Join(dir, "empty", "other.md"), "x", 0o644)
	write(t, filepath.Join(dir, "plain-file"), "x", 0o644)
	write(t, filepath.Join(root, "outside", "SKILL.md"), "x", 0o644)
	if err := os.Symlink(filepath.Join(root, "outside"), filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "outside", "SKILL.md"), filepath.Join(dir, "good", "ln.md")); err != nil {
		t.Fatal(err)
	}
	found, skipped, err := Skills(root, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].Item.Name != "good" || found[0].Item.Origin != "skills/good" {
		t.Fatalf("found = %+v", found)
	}
	var names []string
	for _, f := range found[0].Item.Files {
		names = append(names, f.Path)
	}
	if want := []string{"SKILL.md", "sub/run.sh"}; !reflect.DeepEqual(names, want) {
		t.Errorf("files = %v, want %v", names, want)
	}
	if !found[0].Item.Files[1].Exec || len(found[0].Paths) != 2 {
		t.Errorf("exec/paths: %+v", found[0])
	}
	got := map[string]string{}
	for _, s := range skipped {
		got[filepath.Base(s.Path)] = s.Reason
	}
	want := map[string]string{
		"linked":   SymlinkReason,
		"ln.md":    SymlinkReason,
		"Bad Name": "name must match ^[a-z0-9][a-z0-9-]*$; rename the folder",
		"empty":    "SKILL.md is missing",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("skipped = %v, want %v", got, want)
	}
}

func TestSkillsLinkedFolder(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "real", "s", "SKILL.md"), "x", 0o644)
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "skills")); err != nil {
		t.Fatal(err)
	}
	found, skipped, err := Skills(root, filepath.Join(root, "skills"))
	if err != nil || len(found) != 0 || len(skipped) != 1 || skipped[0].Reason != SymlinkReason {
		t.Errorf("found=%v skipped=%v err=%v", found, skipped, err)
	}
}

func TestSkillsMissingFolder(t *testing.T) {
	root := t.TempDir()
	found, skipped, err := Skills(root, filepath.Join(root, "skills"))
	if err != nil || found != nil || skipped != nil {
		t.Errorf("found=%v skipped=%v err=%v", found, skipped, err)
	}
}

func TestSkillsUnreadableFolder(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads everything")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "skills")
	write(t, filepath.Join(dir, "s", "SKILL.md"), "x", 0o644)
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	found, skipped, err := Skills(root, dir)
	if err != nil || len(found) != 0 || len(skipped) != 1 || !strings.HasPrefix(skipped[0].Reason, "cannot read: ") {
		t.Errorf("found=%v skipped=%v err=%v", found, skipped, err)
	}
}

func TestUnquote(t *testing.T) {
	for _, c := range []struct {
		in, out string
		ok      bool
	}{
		{"", "", true}, {"plain", "plain", true}, {`"a b"`, "a b", true}, {`'it''s'`, "it's", true},
		{`"open`, "", false}, {"'open", "", false}, {"'", "", false},
	} {
		out, ok := Unquote(c.in)
		if out != c.out || ok != c.ok {
			t.Errorf("Unquote(%q) = %q, %v", c.in, out, ok)
		}
	}
}

func TestParseInline(t *testing.T) {
	for _, c := range []struct {
		in   string
		want []string
		ok   bool
	}{
		{`["a","b"]`, []string{"a", "b"}, true},
		{`[a, 'b', "c"]`, []string{"a", "b", "c"}, true},
		{`[]`, nil, false}, {`[ ]`, nil, false}, {`[""]`, nil, false}, {`[a,]`, nil, false},
		{`['**/*.{ts,tsx}']`, []string{"**/*.{ts,tsx}"}, true},
		{`["a,b", 'c', d]`, []string{"a,b", "c", "d"}, true},
		{`[a`, nil, false}, {`[a, "b]`, nil, false},
	} {
		got, ok := ParseInline(c.in)
		if ok != c.ok || (ok && !reflect.DeepEqual(got, c.want)) {
			t.Errorf("ParseInline(%q) = %v, %v", c.in, got, ok)
		}
	}
}

func TestQuoteName(t *testing.T) {
	if got := QuoteName("a\"b\n"); got != `"a\"b\n"` {
		t.Errorf("QuoteName = %s", got)
	}
}

func TestOrderedObject(t *testing.T) {
	keys, vals, err := content.OrderedObject([]byte(`{"z": 1, "a": {"x": [1,2]}, "m": "s"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(keys, []string{"z", "a", "m"}) {
		t.Errorf("keys = %v", keys)
	}
	if string(vals[1]) != `{"x": [1,2]}` || string(vals[2]) != `"s"` {
		t.Errorf("vals = %s", vals)
	}
	for _, bad := range []string{`[1]`, `"s"`, ``, `{"a":`} {
		if _, _, err := content.OrderedObject([]byte(bad)); err == nil {
			t.Errorf("content.OrderedObject(%q) must fail", bad)
		}
	}
}
