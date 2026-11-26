// Package source holds pure functions that decide what a channel means.
package source

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var releaseTagRe = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// ValidChannel reports whether s is a valid channel name: "stable", "next", or
// an exact release tag vM.m.p where each component is a decimal number without
// a leading zero (a single 0 is fine).
func ValidChannel(s string) bool {
	if s == "stable" || s == "next" {
		return true
	}
	return releaseTagRe.MatchString(s)
}

// PickStable returns the release tag with the highest version from tags,
// compared numerically per component (major, minor, patch). Every non-release
// string is ignored. ok is false when no release tag is in the list.
func PickStable(tags []string) (tag string, ok bool) {
	var bestMajor, bestMinor, bestPatch int
	var best string
	found := false

	for _, t := range tags {
		m := releaseTagRe.FindStringSubmatch(t)
		if m == nil {
			continue
		}
		major, e1 := strconv.Atoi(m[1])
		minor, e2 := strconv.Atoi(m[2])
		patch, e3 := strconv.Atoi(m[3])
		if e1 != nil || e2 != nil || e3 != nil {
			continue // a component too big for an int is not a usable version
		}

		if !found || major > bestMajor ||
			(major == bestMajor && minor > bestMinor) ||
			(major == bestMajor && minor == bestMinor && patch > bestPatch) {
			bestMajor, bestMinor, bestPatch = major, minor, patch
			best = t
			found = true
		}
	}

	return best, found
}

// DefaultBranch extracts the default branch from the output of
// `git ls-remote --symref <source> HEAD`. The branch appears in the line
// `ref: refs/heads/<branch>\tHEAD`. ok is false when no such line exists.
func DefaultBranch(lsRemote string) (branch string, ok bool) {
	for _, line := range strings.Split(lsRemote, "\n") {
		if strings.HasPrefix(line, "ref: refs/heads/") && strings.HasSuffix(line, "\tHEAD") {
			branch = strings.TrimPrefix(line, "ref: refs/heads/")
			branch = strings.TrimSuffix(branch, "\tHEAD")
			return branch, branch != ""
		}
	}
	return "", false
}

// releaseVersion parses a release tag into its numbers.
func releaseVersion(t string) (v [3]int, ok bool) {
	m := releaseTagRe.FindStringSubmatch(t)
	if m == nil {
		return v, false
	}
	for i := range v {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

// ReleaseTags returns the release tags of tags, newest first (the order PickStable uses).
// Every non-release string is ignored.
func ReleaseTags(tags []string) []string {
	var out []string
	for _, t := range tags {
		if _, ok := releaseVersion(t); ok {
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, _ := releaseVersion(out[i])
		b, _ := releaseVersion(out[j])
		for k := range a {
			if a[k] != b[k] {
				return a[k] > b[k]
			}
		}
		return false
	})
	return out
}

// Newer reports whether release tag a is newer than release tag b.
func Newer(a, b string) bool {
	va, ok1 := releaseVersion(a)
	vb, ok2 := releaseVersion(b)
	if !ok1 || !ok2 {
		return false
	}
	for k := range va {
		if va[k] != vb[k] {
			return va[k] > vb[k]
		}
	}
	return false
}
