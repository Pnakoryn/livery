// Package paths says where livery keeps its config, lock, personal layer and cache.
package paths

import (
	"os"
	"path/filepath"

	"github.com/Pnakoryn/livery/internal/adapter"
)

// xdgDir returns $name when it is an absolute path, else <home>/<fallback>.
func xdgDir(env adapter.Env, name, fallback string) string {
	if env.Getenv != nil {
		if v := env.Getenv(name); filepath.IsAbs(v) {
			return filepath.Join(v, "livery")
		}
	}
	return filepath.Join(env.Home, fallback, "livery")
}

// ProjectDir is the folder of a project's config and lock, relative to the project root.
const ProjectDir = ".livery"

// ConfigDir is $XDG_CONFIG_HOME/livery, or <home>/.config/livery.
func ConfigDir(env adapter.Env) string { return xdgDir(env, "XDG_CONFIG_HOME", ".config") }

// CacheDir is $XDG_CACHE_HOME/livery, or <home>/.cache/livery, in both scopes.
func CacheDir(env adapter.Env) string { return xdgDir(env, "XDG_CACHE_HOME", ".cache") }

// configDir is the folder of the config and lock of env's scope.
func configDir(env adapter.Env) string {
	if env.Project != "" {
		return filepath.Join(env.Project, ProjectDir)
	}
	return ConfigDir(env)
}

// ConfigFile is <config>/config.json, or <project>/.livery/config.json in project scope.
func ConfigFile(env adapter.Env) string { return filepath.Join(configDir(env), "config.json") }

// LockFile is <config>/lock.json, or <project>/.livery/lock.json in project scope.
func LockFile(env adapter.Env) string { return filepath.Join(configDir(env), "lock.json") }

// PersonalDir is <config>/personal; empty in project scope, which has no personal layer.
func PersonalDir(env adapter.Env) string {
	if env.Project != "" {
		return ""
	}
	return filepath.Join(ConfigDir(env), "personal")
}

// FindProject returns the nearest folder from dir upward that holds .livery/config.json as a
// regular file in a real folder (a symbolic link to either does not count). dir is made absolute first.
func FindProject(dir string) (root string, found bool) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	for {
		if d, err := os.Lstat(filepath.Join(dir, ProjectDir)); err == nil && d.IsDir() {
			fi, err := os.Lstat(filepath.Join(dir, ProjectDir, "config.json"))
			if err == nil && fi.Mode().IsRegular() {
				return dir, true
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}
