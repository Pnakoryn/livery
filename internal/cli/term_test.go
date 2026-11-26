package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsTerminalFalse(t *testing.T) {
	t.Parallel()
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = null.Close() }()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close(); _ = w.Close() }()
	reg, err := os.Create(filepath.Join(t.TempDir(), "f"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reg.Close() }()
	for name, f := range map[string]*os.File{"/dev/null": null, "pipe read": r, "pipe write": w, "regular file": reg} {
		if isTerminal(f) {
			t.Errorf("isTerminal(%s) = true, want false", name)
		}
	}
	if isTerminal("not a file") {
		t.Error("isTerminal(string) = true")
	}
}
