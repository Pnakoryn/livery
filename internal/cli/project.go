package cli

import (
	"flag"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/apply"
	"github.com/Pnakoryn/livery/internal/exit"
	"github.com/Pnakoryn/livery/internal/paths"
)

// projectFlag registers --project on fs.
func projectFlag(fs *flag.FlagSet, what string) *bool {
	return fs.Bool("project", false, what)
}

// projectArg is " --project" in project scope, else "": the suffix of a fix hint that names a
// livery command, so the hint works in the scope the run is in.
func projectArg(env adapter.Env) string {
	return apply.ProjectArg(env.Project)
}

// scoped returns env in project scope when project is set, else env unchanged. create says
// whether a missing project may be created (only setup allows it).
func scoped(env adapter.Env, project, create bool) (adapter.Env, error) {
	if !project {
		return env, nil
	}
	return projectEnv(env, create)
}

// projectEnv returns env in project scope: Project is the nearest folder from env.Dir upward
// that holds .livery/config.json. When there is none, with create it is env.Dir (setup
// --project starts a project there), without it the run stops with exit 2.
func projectEnv(env adapter.Env, create bool) (adapter.Env, error) {
	dir, err := filepath.Abs(env.Dir)
	if err != nil {
		return env, exit.Environmentf("cannot find the working directory %q: %v; cd into an existing folder and run livery again", env.Dir, err)
	}
	if link := linkedProjectDir(dir); link != "" {
		return env, exit.Findingf("%s: is a symbolic link, and livery does not follow links in a project; replace the link with a real folder or file, then run livery again with --project", link)
	}
	if root, ok := paths.FindProject(dir); ok {
		env.Project = root
		return env, nil
	}
	if !create {
		return env, exit.Usagef("no %s in %s or above; run livery setup --project there", filepath.Join(paths.ProjectDir, "config.json"), dir)
	}
	env.Project = dir
	return env, nil
}

// linkedProjectDir returns the first of .livery, .livery/config.json and .livery/lock.json that
// is a symbolic link, in dir and each folder above it up to the nearest project (the first
// folder whose .livery/config.json is a regular file), or "" when there is none. A linked
// .livery would put the project's config and lock, and the project root, where the link points.
func linkedProjectDir(dir string) string {
	isLink := func(p string) (fs.FileInfo, bool) {
		fi, err := os.Lstat(p)
		return fi, err == nil && fi.Mode()&fs.ModeSymlink != 0
	}
	for {
		d := filepath.Join(dir, paths.ProjectDir)
		fi, link := isLink(d)
		if link {
			return d
		}
		if fi != nil && fi.IsDir() {
			if _, link := isLink(filepath.Join(d, "lock.json")); link {
				return filepath.Join(d, "lock.json")
			}
			cfg, link := isLink(filepath.Join(d, "config.json"))
			if link {
				return filepath.Join(d, "config.json")
			}
			if cfg != nil && cfg.Mode().IsRegular() {
				return ""
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
