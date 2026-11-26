package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/exit"
)

func TestProjectEnv(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cfg := filepath.Join(root, ".livery", "config.json")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, create := range []bool{false, true} {
		got, err := projectEnv(adapter.Env{Home: "/h", Dir: nested}, create)
		if err != nil || got.Project != root || got.Dir != nested || got.Home != "/h" {
			t.Errorf("create=%v: %+v, %v", create, got, err)
		}
	}

	none := t.TempDir()
	_, err := projectEnv(adapter.Env{Dir: none}, false)
	var e *exit.Error
	want := "no .livery/config.json in " + none + " or above; run livery setup --project there"
	if !errors.As(err, &e) || e.Code != exit.Usage || !strings.Contains(err.Error(), want) {
		t.Errorf("not found: err = %v, want exit 2 %q", err, want)
	}
	got, err := projectEnv(adapter.Env{Dir: none}, true)
	if err != nil || got.Project != none {
		t.Errorf("create: %+v, %v", got, err)
	}
}
