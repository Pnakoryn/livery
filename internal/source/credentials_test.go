package source

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/exit"
)

func TestHasCredentials(t *testing.T) {
	for addr, want := range map[string]bool{
		"https://tok@host/x.git":    true,
		"https://u:p@host/x.git":    true,
		"http://u@host/x":           true,
		"ssh://u:p@host/x":          true,
		"HTTPS://tok@host/x.git":    true,
		"git@host:team/x.git":       false,
		"ssh://git@host/x":          false,
		"https://host/x.git":        false,
		"https://host/x@y.git":      false,
		"/srv/x":                    false,
		"./x":                       false,
		"":                          false,
		"file:///srv/a@b/x.git":     false,
		"https://@host/x.git":       false,
		"git://host/x.git":          false,
		"https://host:8443/x.git":   false,
		"https://u:p@host:8443/x":   true,
		"ssh://git@host:2222/x.git": false,
	} {
		if got := HasCredentials(addr); got != want {
			t.Errorf("HasCredentials(%q) = %v, want %v", addr, got, want)
		}
	}
}

// Resolve refuses an address with credentials before git runs or the cache is named, and the
// message does not contain the address.
func TestResolveRefusesCredentials(t *testing.T) {
	cache := t.TempDir()
	for retry, setup := range map[string]string{
		"livery update":           "then run livery setup --source",
		"livery setup --project":  "then run livery setup --project --source",
		"livery doctor --project": "then run livery setup --project --source",
	} {
		_, err := Resolve(cache, "https://s3cr3tTok@host.invalid/x.git", "stable", retry)
		var e *exit.Error
		if !errors.As(err, &e) || e.Code != exit.Usage {
			t.Fatalf("%s: want usage error, got %v", retry, err)
		}
		msg := err.Error()
		if strings.Contains(msg, "s3cr3tTok") || !strings.Contains(msg, "credential helper") || !strings.Contains(msg, setup) {
			t.Errorf("%s: %s", retry, msg)
		}
	}
	if entries, _ := os.ReadDir(cache); len(entries) != 0 {
		t.Errorf("cache has entries: %v", entries)
	}
}
