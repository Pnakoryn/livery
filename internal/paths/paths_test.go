package paths

import (
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
)

func env(home string, vars map[string]string) adapter.Env {
	return adapter.Env{Home: home, Getenv: func(k string) string { return vars[k] }}
}

func TestDirs(t *testing.T) {
	tests := []struct {
		name          string
		env           adapter.Env
		config, cache string
	}{
		{"default", env("/h", nil), "/h/.config/livery", "/h/.cache/livery"},
		{"xdg", env("/h", map[string]string{"XDG_CONFIG_HOME": "/x", "XDG_CACHE_HOME": "/c"}), "/x/livery", "/c/livery"},
		{"relative ignored", env("/h", map[string]string{"XDG_CONFIG_HOME": "rel", "XDG_CACHE_HOME": "rel"}), "/h/.config/livery", "/h/.cache/livery"},
		{"nil getenv", adapter.Env{Home: "/h"}, "/h/.config/livery", "/h/.cache/livery"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ConfigDir(tc.env); got != tc.config {
				t.Errorf("ConfigDir = %q, want %q", got, tc.config)
			}
			if got := CacheDir(tc.env); got != tc.cache {
				t.Errorf("CacheDir = %q, want %q", got, tc.cache)
			}
			if got := ConfigFile(tc.env); got != tc.config+"/config.json" {
				t.Errorf("ConfigFile = %q", got)
			}
			if got := LockFile(tc.env); got != tc.config+"/lock.json" {
				t.Errorf("LockFile = %q", got)
			}
			if got := PersonalDir(tc.env); got != tc.config+"/personal" {
				t.Errorf("PersonalDir = %q", got)
			}
		})
	}
}
