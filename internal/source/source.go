package source

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/Pnakoryn/livery/internal/exit"
)

// Resolved is a channel resolved to one commit of the source.
type Resolved struct {
	Source  string
	Channel string
	Tag     string // empty for channel next
	Commit  string // full 40-hex id
	Newest  string // the newest vX.Y.Z tag in the cache after the fetch; empty when there is none
}

func gitPath() (string, error) {
	p, err := exec.LookPath("git")
	if err != nil {
		return "", exit.Environmentf("git is needed but was not found in PATH; install git and run the command again")
	}
	return p, nil
}

// gitReason returns git's reason from its stderr: the text of the first "fatal: " line, else of
// the first "error: " line, else the last non-empty line.
func gitReason(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for _, prefix := range []string{"fatal: ", "error: "} {
		for _, l := range lines {
			if t, ok := strings.CutPrefix(strings.TrimSpace(l), prefix); ok {
				return strings.TrimSpace(t)
			}
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
}

// isLocalPath reports whether addr is a path: no "://" and no ":" before the first "/"
// (host:path is a remote address).
func isLocalPath(addr string) bool {
	if strings.Contains(addr, "://") {
		return false
	}
	i := strings.Index(addr, ":")
	return i < 0 || strings.Contains(addr[:i], "/")
}

// localEnvVars are the repository-local variables of `git rev-parse --local-env-vars`.
// git exports them to hooks, `rebase -x` commands and `!` aliases; they must not reach
// livery's own git calls, which work in the source cache.
var localEnvVars = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_COMMON_DIR", "GIT_NAMESPACE", "GIT_PREFIX",
	"GIT_CONFIG", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT", "GIT_GRAFT_FILE",
	"GIT_IMPLICIT_WORK_TREE", "GIT_NO_REPLACE_OBJECTS", "GIT_REPLACE_REF_BASE",
	"GIT_SHALLOW_FILE", "GIT_INTERNAL_SUPER_PREFIX",
}

func isLocalEnv(kv string) bool {
	name, _, _ := strings.Cut(kv, "=")
	for _, v := range localEnvVars {
		if name == v {
			return true
		}
	}
	return strings.HasPrefix(name, "GIT_CONFIG_KEY_") || strings.HasPrefix(name, "GIT_CONFIG_VALUE_")
}

// GitEnv returns the environment for a git subprocess: os.Environ() without the
// repository-local variables, plus extra.
func GitEnv(extra ...string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if !isLocalEnv(kv) {
			env = append(env, kv)
		}
	}
	return append(env, extra...)
}

// runGit runs git with args (in dir when non-empty) and returns stdout and the stderr.
func runGit(dir string, args ...string) (stdout, stderr string, err error) {
	gp, err := gitPath()
	if err != nil {
		return "", "", err
	}
	cmd := exec.Command(gp, args...)
	cmd.Dir = dir
	cmd.Env = GitEnv("GIT_TERMINAL_PROMPT=0")
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err = cmd.Run()
	return out.String(), errb.String(), err
}

func cacheName(source string) string {
	base := strings.TrimRight(filepath.ToSlash(source), "/")
	if base != "" {
		base = path.Base(base)
	}
	base = strings.TrimSuffix(base, ".git")
	var b strings.Builder
	for _, r := range base {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	name := b.String()
	if name == "" {
		name = "src"
	}
	sum := sha256.Sum256([]byte(source))
	return name + "-" + hex.EncodeToString(sum[:])[:12] + ".git"
}

// ShortCommit returns the first 12 characters of a commit hash.
func ShortCommit(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}

// Canonical makes a relative path that exists on disk absolute and clean; anything else (URLs,
// user@host:path, absolute or missing paths) is returned unchanged.
func Canonical(src string) string {
	if src == "" || filepath.IsAbs(src) {
		return src
	}
	if _, err := os.Stat(src); err == nil {
		if abs, err := filepath.Abs(src); err == nil {
			return abs
		}
	}
	return src
}

// HasCredentials reports whether addr carries credentials: scheme://user:password@host with any
// scheme, or a user part alone over http or https (a token). git@host:path, ssh://git@host/path,
// paths and addresses without a user part have none.
func HasCredentials(addr string) bool {
	scheme, rest, ok := strings.Cut(addr, "://")
	if !ok || scheme == "" || strings.ContainsAny(scheme, "/@") {
		return false
	}
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		rest = rest[:i]
	}
	at := strings.LastIndex(rest, "@")
	if at <= 0 {
		return false
	}
	if strings.Contains(rest[:at], ":") {
		return true
	}
	scheme = strings.ToLower(scheme)
	return scheme == "http" || scheme == "https"
}

// CredentialsError is the refusal of an address with credentials; prefix names where the
// address came from (a config file) or is empty; setup is the setup command to run again.
// The address itself is never in the message.
func CredentialsError(prefix, setup string) error {
	return exit.Usagef("%sthe source address has credentials in it; use an SSH address or a git credential helper, then run %s --source <address without credentials>", prefix, setup)
}

// Resolve fetches source into the bare cache below cacheDir and resolves channel to a commit.
func Resolve(cacheDir, source, channel, retry string) (Resolved, error) {
	if HasCredentials(source) {
		setup := "livery setup"
		if strings.HasSuffix(retry, " --project") {
			setup += " --project"
		}
		return Resolved{}, CredentialsError("", setup)
	}
	if !ValidChannel(channel) {
		return Resolved{}, exit.Usagef("channel %q is not valid; use stable, next, or an exact release tag like v1.2.3", channel)
	}
	if strings.HasPrefix(source, "-") {
		return Resolved{}, exit.Usagef("source %q starts with '-' and would be read as a git option; give a URL or a path (use ./%s for a local directory)", source, source)
	}
	if _, err := gitPath(); err != nil {
		return Resolved{}, err
	}
	if retry == "" {
		retry = "livery update"
	}
	local := isLocalPath(source)
	if local {
		if _, err := os.Stat(source); err != nil && errors.Is(err, fs.ErrNotExist) {
			return Resolved{}, exit.Environmentf("source %s: no such folder; check the path, then run %s", source, retry)
		}
	}
	source = Canonical(source)
	repo := filepath.Join(cacheDir, "src", cacheName(source))
	if _, err := os.Stat(filepath.Join(repo, "HEAD")); err != nil {
		if err := os.MkdirAll(repo, 0o755); err != nil {
			return Resolved{}, exit.Environmentf("cannot create cache %s: %v; check the directory permissions, then run %s", repo, err, retry)
		}
		if _, se, err := runGit("", "init", "--bare", "-q", repo); err != nil {
			return Resolved{}, exit.Environmentf("cannot create cache %s: %s; check the directory permissions, then run %s", repo, gitReason(se), retry)
		}
	}
	if _, se, err := runGit(repo, "fetch", "--prune", "--force", "-q", source,
		"+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*"); err != nil {
		if local {
			return Resolved{}, exit.Environmentf("cannot fetch source %s: %s; a shared repository folder needs a commit and a tag: in that folder run git init -b main, git add -A, git commit -m \"first release\", git tag v0.1.0; then run %s", source, gitReason(se), retry)
		}
		return Resolved{}, exit.Environmentf("cannot fetch source %s: %s; check the address, your network and your git credentials (livery never lets git ask for a password: use an SSH address or a credential helper), then run %s", source, gitReason(se), retry)
	}

	so, se, err := runGit(repo, "tag", "-l")
	if err != nil {
		return Resolved{}, exit.Environmentf("cannot list tags of %s: %s; remove the cache directory %s, then run %s", source, gitReason(se), repo, retry)
	}
	releases := ReleaseTags(strings.Fields(so))
	res := Resolved{Source: source, Channel: channel}
	if len(releases) > 0 {
		res.Newest = releases[0]
	}
	var ref string
	switch channel {
	case "stable":
		if res.Newest == "" {
			return Resolved{}, exit.Findingf("%s: has no release yet (no vX.Y.Z tag); use the newest commit with livery setup --channel next, or tag a release in the shared repository", source)
		}
		res.Tag = res.Newest
		ref = "refs/tags/" + res.Tag
	case "next":
		so, se, err := runGit("", "ls-remote", "--symref", source, "HEAD")
		if err != nil {
			return Resolved{}, exit.Environmentf("cannot read HEAD of source %s: %s; then run %s", source, gitReason(se), retry)
		}
		branch, ok := DefaultBranch(so)
		if !ok {
			return Resolved{}, exit.Findingf("%s: no default branch (HEAD is not a symbolic ref); check the shared repository", source)
		}
		ref = "refs/heads/" + branch
	default:
		if _, _, err := runGit(repo, "rev-parse", "-q", "--verify", "refs/tags/"+channel); err != nil {
			return Resolved{}, exit.Usagef("%s", unknownTag(channel, source, releases))
		}
		res.Tag = channel
		ref = "refs/tags/" + channel
	}

	so, se, err = runGit(repo, "rev-parse", ref+"^{commit}")
	if err != nil {
		return Resolved{}, exit.Findingf("cannot resolve %s in source %s: %s", ref, source, gitReason(se))
	}
	res.Commit = strings.TrimSpace(so)
	return res, nil
}

// ErrNotCached is wrapped by the error Cached returns when the cache does not hold the commit.
var ErrNotCached = errors.New("commit not in the cache")

// Cached resolves a commit already known (from a lock) in the bare cache below cacheDir without
// git fetch: no network. source must be the canonical address the lock records. Newest is the
// newest release tag the cache holds from the last fetch. A cache without the commit is an
// Environment error wrapping ErrNotCached.
func Cached(cacheDir, source, channel, tag, commit string) (Resolved, error) {
	res := Resolved{Source: source, Channel: channel, Tag: tag, Commit: commit}
	notCached := func() (Resolved, error) {
		e := exit.Environmentf("commit %s of %s is not in the cache %s", commit, source, cacheDir)
		e.Cause = ErrNotCached
		return res, e
	}
	if _, err := gitPath(); err != nil {
		return res, err
	}
	repo := filepath.Join(cacheDir, "src", cacheName(source))
	if _, err := os.Stat(filepath.Join(repo, "HEAD")); err != nil {
		return notCached()
	}
	if strings.HasPrefix(commit, "-") {
		return notCached()
	}
	if _, _, err := runGit(repo, "cat-file", "-e", commit+"^{commit}"); err != nil {
		return notCached()
	}
	if so, _, err := runGit(repo, "tag", "-l"); err == nil {
		if releases := ReleaseTags(strings.Fields(so)); len(releases) > 0 {
			res.Newest = releases[0]
		}
	}
	return res, nil
}

// Export writes the files of r's commit to cacheDir/tree/<commit> and returns that directory.
// An existing directory is reused unchecked.
func Export(cacheDir string, r Resolved) (dir string, err error) {
	dir = filepath.Join(cacheDir, "tree", r.Commit)
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	gp, err := gitPath()
	if err != nil {
		return "", err
	}
	repo := filepath.Join(cacheDir, "src", cacheName(r.Source))
	tmp := fmt.Sprintf("%s.tmp-%d", dir, os.Getpid())
	if err := os.RemoveAll(tmp); err != nil {
		return "", exit.Environmentf("cannot clear %s: %v; remove it and run again", tmp, err)
	}
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return "", exit.Environmentf("cannot create %s: %v; check the directory permissions", tmp, err)
	}
	fail := func(e error) (string, error) {
		_ = os.RemoveAll(tmp)
		return "", e
	}

	cmd := exec.Command(gp, "--git-dir", repo, "archive", "--format=tar", r.Commit)
	cmd.Env = GitEnv("GIT_TERMINAL_PROMPT=0")
	var errb bytes.Buffer
	cmd.Stderr = &errb
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fail(exit.Environmentf("cannot run git archive: %v", err))
	}
	if err := cmd.Start(); err != nil {
		return fail(exit.Environmentf("cannot run git archive: %v", err))
	}
	werr := extract(stdout, tmp)
	_, _ = io.Copy(io.Discard, stdout)
	cerr := cmd.Wait()
	if werr != nil {
		return fail(werr)
	}
	if cerr != nil {
		return fail(exit.Environmentf("git archive of %s failed: %s; then run livery update", r.Commit, gitReason(errb.String())))
	}
	if err := chmodDirs(tmp); err != nil {
		return fail(err)
	}
	if err := os.Rename(tmp, dir); err != nil {
		if _, serr := os.Stat(dir); serr == nil {
			_ = os.RemoveAll(tmp)
			return dir, nil
		}
		return fail(exit.Environmentf("cannot move %s to %s: %v", tmp, dir, err))
	}
	return dir, nil
}

// chmodDirs sets every directory below root (and root) to 0755, whatever the umask.
func chmodDirs(root string) error {
	return filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return exit.Environmentf("cannot read %s: %v", p, err)
		}
		if d.IsDir() {
			if err := os.Chmod(p, 0o755); err != nil {
				return exit.Environmentf("cannot chmod %s: %v", p, err)
			}
		}
		return nil
	})
}

func unsafeName(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") || filepath.IsAbs(name) {
		return true
	}
	for _, el := range strings.Split(name, "/") {
		if el == ".." {
			return true
		}
	}
	return false
}

func extract(r io.Reader, root string) error {
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return exit.Environmentf("cannot read git archive: %v", err)
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		if unsafeName(h.Name) {
			return exit.Usagef("unsafe path %q in the shared repository; remove it", h.Name)
		}
		clean := path.Clean(h.Name)
		if clean == "." {
			continue
		}
		switch strings.SplitN(clean, "/", 2)[0] {
		case "livery.json", "base", "teams":
		default:
			continue
		}
		target := filepath.Join(root, filepath.FromSlash(clean))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return exit.Environmentf("cannot create %s: %v", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return exit.Environmentf("cannot create %s: %v", filepath.Dir(target), err)
			}
			mode := os.FileMode(0o644)
			if h.Mode&0o111 != 0 {
				mode = 0o755
			}
			f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
			if err != nil {
				return exit.Environmentf("cannot write %s: %v", target, err)
			}
			_, cerr := io.Copy(f, tr)
			if e := f.Close(); cerr == nil {
				cerr = e
			}
			if cerr != nil {
				return exit.Environmentf("cannot write %s: %v", target, cerr)
			}
			if err := os.Chmod(target, mode); err != nil {
				return exit.Environmentf("cannot chmod %s: %v", target, err)
			}
		case tar.TypeSymlink, tar.TypeLink:
			return exit.Usagef("%q in the shared repository is a link; links are not allowed, replace it with a regular file", h.Name)
		default:
			return exit.Usagef("%q in the shared repository has an unsupported file type; remove it", h.Name)
		}
	}
}

// unknownTag is the message for a channel tag the source does not have; releases is newest first.
func unknownTag(tag, source string, releases []string) string {
	if len(releases) == 0 {
		return fmt.Sprintf("tag %s does not exist in the shared repository %s; it has no release yet; use channel next, or tag a release", tag, source)
	}
	list := releases
	more := ""
	if len(list) > 10 {
		list = list[:10]
		more = ", ..."
	}
	return fmt.Sprintf("tag %s does not exist in the shared repository %s; the releases, newest first: %s%s; use channel stable or next, or one of them",
		tag, source, strings.Join(list, ", "), more)
}
