// Package secrets finds text that may hold a secret, with the rules of gitleaks (embedded as
// data in rules.json, see NOTICE) and livery's own matcher. A secret itself is never returned:
// a Finding names the path, line, rule and a fingerprint of the secret.
package secrets

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
)

//go:embed rules.json
var rulesJSON []byte

const (
	// binaryProbe is how many leading bytes are checked for a NUL byte.
	binaryProbe = 8000
)

// Finding is one possible secret. The secret itself is not part of it.
type Finding struct {
	Path        string
	Line        int
	Rule        string
	Fingerprint string
}

type rawAllowlist struct {
	Description string   `json:"description"`
	Condition   string   `json:"condition"`
	RegexTarget string   `json:"regexTarget"`
	Regexes     []string `json:"regexes"`
	Paths       []string `json:"paths"`
	Stopwords   []string `json:"stopwords"`
}

type rawRule struct {
	ID          string         `json:"id"`
	Description string         `json:"description"`
	Regex       string         `json:"regex"`
	Entropy     float64        `json:"entropy"`
	Keywords    []string       `json:"keywords"`
	SecretGroup int            `json:"secretGroup"`
	Path        string         `json:"path"`
	Allowlists  []rawAllowlist `json:"allowlists"`
}

type rawRules struct {
	Source    string       `json:"source"`
	Allowlist rawAllowlist `json:"allowlist"`
	Rules     []rawRule    `json:"rules"`
}

type allowlist struct {
	and       bool
	target    string // "", "match" or "line"
	regexes   []*regexp.Regexp
	paths     []*regexp.Regexp
	stopwords []string
}

type rule struct {
	id          string
	regex       *regexp.Regexp
	entropy     float64
	keywords    [][]byte
	secretGroup int
	path        *regexp.Regexp
	allowlists  []allowlist
}

type ruleset struct {
	global allowlist
	rules  []rule
}

var loadRules = sync.OnceValues(func() (*ruleset, error) { return compile(rulesJSON) })

func compileAll(where string, exprs []string) ([]*regexp.Regexp, error) {
	out := make([]*regexp.Regexp, 0, len(exprs))
	for _, e := range exprs {
		re, err := regexp.Compile(e)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", where, err)
		}
		out = append(out, re)
	}
	return out, nil
}

func compileAllowlist(where string, a rawAllowlist) (allowlist, error) {
	var out allowlist
	var err error
	switch strings.ToUpper(a.Condition) {
	case "", "OR":
	case "AND":
		out.and = true
	default:
		return out, fmt.Errorf("%s: unknown condition %q", where, a.Condition)
	}
	switch a.RegexTarget {
	case "", "secret":
	case "match", "line":
		out.target = a.RegexTarget
	default:
		return out, fmt.Errorf("%s: unknown regexTarget %q", where, a.RegexTarget)
	}
	if out.regexes, err = compileAll(where+" regexes", a.Regexes); err != nil {
		return out, err
	}
	if out.paths, err = compileAll(where+" paths", a.Paths); err != nil {
		return out, err
	}
	for _, w := range a.Stopwords {
		out.stopwords = append(out.stopwords, strings.ToLower(w))
	}
	return out, nil
}

// compile parses and compiles a rules file in the shape of rules.json.
func compile(data []byte) (*ruleset, error) {
	var raw rawRules
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("rules.json: %w", err)
	}
	global, err := compileAllowlist("rules.json allowlist", raw.Allowlist)
	if err != nil {
		return nil, err
	}
	rs := &ruleset{global: global}
	for i, r := range raw.Rules {
		where := fmt.Sprintf("rules.json rule %d (%s)", i, r.ID)
		if r.ID == "" {
			return nil, fmt.Errorf("%s: no id", where)
		}
		if r.Regex == "" && r.Path == "" {
			return nil, fmt.Errorf("%s: neither regex nor path", where)
		}
		c := rule{id: r.ID, entropy: r.Entropy, secretGroup: r.SecretGroup}
		if r.Regex != "" {
			if c.regex, err = regexp.Compile(r.Regex); err != nil {
				return nil, fmt.Errorf("%s regex: %w", where, err)
			}
			if r.SecretGroup > c.regex.NumSubexp() {
				return nil, fmt.Errorf("%s: secretGroup %d, regex has %d groups", where, r.SecretGroup, c.regex.NumSubexp())
			}
		}
		if r.Path != "" {
			if c.path, err = regexp.Compile(r.Path); err != nil {
				return nil, fmt.Errorf("%s path: %w", where, err)
			}
		}
		for _, k := range r.Keywords {
			c.keywords = append(c.keywords, []byte(strings.ToLower(k)))
		}
		for j, a := range r.Allowlists {
			al, err := compileAllowlist(fmt.Sprintf("%s allowlist %d", where, j), a)
			if err != nil {
				return nil, err
			}
			c.allowlists = append(c.allowlists, al)
		}
		rs.rules = append(rs.rules, c)
	}
	return rs, nil
}

// candidate is what an allowlist looks at for one match.
type candidate struct {
	path, secret, match, line string
}

func anyMatch(res []*regexp.Regexp, s string) bool {
	for _, re := range res {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// allows reports whether the allowlist drops the candidate.
func (a *allowlist) allows(c *candidate) bool {
	var results []bool
	if len(a.regexes) > 0 {
		target := c.secret
		switch a.target {
		case "match":
			target = c.match
		case "line":
			target = c.line
		}
		results = append(results, anyMatch(a.regexes, target))
	}
	if len(a.paths) > 0 {
		results = append(results, anyMatch(a.paths, c.path))
	}
	if len(a.stopwords) > 0 {
		lower := strings.ToLower(c.secret)
		hit := false
		for _, w := range a.stopwords {
			if strings.Contains(lower, w) {
				hit = true
				break
			}
		}
		results = append(results, hit)
	}
	if len(results) == 0 {
		return false
	}
	for _, r := range results {
		if r && !a.and {
			return true
		}
		if !r && a.and {
			return false
		}
	}
	return a.and
}

func (rs *ruleset) allowed(r *rule, c *candidate) bool {
	for i := range r.allowlists {
		if r.allowlists[i].allows(c) {
			return true
		}
	}
	return rs.global.allows(c)
}

// variableRef is a secret that only names a variable: ${NAME} or {env:NAME}.
var variableRef = regexp.MustCompile(`^(?:\$\{[A-Za-z_][A-Za-z0-9_]*\}|\{env:[A-Za-z_][A-Za-z0-9_]*\})$`)

// entropy is the Shannon entropy of s in bits per character.
func entropy(s string) float64 {
	counts := map[rune]int{}
	n := 0
	for _, r := range s {
		counts[r]++
		n++
	}
	if n == 0 {
		return 0
	}
	var h float64
	for _, c := range counts {
		p := float64(c) / float64(n)
		h -= p * math.Log2(p)
	}
	return h
}

func fingerprint(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])[:12]
}

// lines maps byte offsets to 1-based line numbers and line bounds.
type lines struct {
	data   []byte
	starts []int // offset of the first byte of each line
}

func newLines(data []byte) *lines {
	l := &lines{data: data, starts: []int{0}}
	for i, b := range data {
		if b == '\n' {
			l.starts = append(l.starts, i+1)
		}
	}
	return l
}

// number is the 1-based line of the byte at offset.
func (l *lines) number(offset int) int {
	return sort.Search(len(l.starts), func(i int) bool { return l.starts[i] > offset })
}

// full is the text of the lines from the one holding start to the one holding end-1.
func (l *lines) full(start, end int) string {
	from := l.starts[l.number(start)-1]
	last := end - 1
	if last < start {
		last = start
	}
	to := len(l.data)
	if n := l.number(last); n < len(l.starts) {
		to = l.starts[n] - 1
	}
	if to < from {
		to = from
	}
	return string(l.data[from:to])
}

// Scan returns the possible secrets in data, sorted by line, then rule. path is slash-separated
// and relative to the source root; it is used for path rules and returned in the findings.
// Data with a NUL byte in its first 8000 bytes is binary: only path rules apply to it, and an
// "unscanned-binary" finding (line 0) says its content was not scanned; secrets-allow can accept it.
func Scan(path string, data []byte) []Finding {
	rs, err := loadRules()
	if err != nil {
		// rules.json is embedded and checked by a test: this is a build defect.
		panic(err)
	}
	return rs.scan(path, data)
}

func (rs *ruleset) scan(path string, data []byte) []Finding {
	binary := bytes.IndexByte(data[:min(len(data), binaryProbe)], 0) >= 0
	lower := bytes.ToLower(data)
	var ls *lines
	var found []Finding
	for i := range rs.rules {
		r := &rs.rules[i]
		if r.path != nil && !r.path.MatchString(path) {
			continue
		}
		if r.regex == nil {
			c := candidate{path: path, secret: path, match: path, line: path}
			if !rs.allowed(r, &c) {
				found = append(found, Finding{Path: path, Line: 0, Rule: r.id, Fingerprint: fingerprint(path)})
			}
			continue
		}
		if binary {
			continue
		}
		if len(r.keywords) > 0 && !containsAny(lower, r.keywords) {
			continue
		}
		for _, m := range r.regex.FindAllSubmatchIndex(data, -1) {
			secret := secretOf(r, data, m)
			if secret == "" {
				continue
			}
			if r.entropy > 0 && entropy(secret) <= r.entropy {
				continue
			}
			if variableRef.MatchString(secret) {
				continue
			}
			if ls == nil {
				ls = newLines(data)
			}
			c := candidate{path: path, secret: secret, match: string(data[m[0]:m[1]]), line: ls.full(m[0], m[1])}
			if rs.allowed(r, &c) {
				continue
			}
			found = append(found, Finding{Path: path, Line: ls.number(m[0]), Rule: r.id, Fingerprint: fingerprint(secret)})
		}
	}
	if binary {
		found = append(found, Finding{Path: path, Line: 0, Rule: UnscannedBinary, Fingerprint: fingerprint(path)})
	}
	return dedupe(found)
}

// UnscannedBinary is the rule of a finding for a binary file, which is not scanned.
const UnscannedBinary = "unscanned-binary"

func containsAny(lower []byte, keywords [][]byte) bool {
	for _, k := range keywords {
		if bytes.Contains(lower, k) {
			return true
		}
	}
	return false
}

// secretOf is capture group secretGroup when set, else the first non-empty group, else the
// whole match.
func secretOf(r *rule, data []byte, m []int) string {
	group := func(g int) string {
		if 2*g+1 >= len(m) || m[2*g] < 0 {
			return ""
		}
		return string(data[m[2*g]:m[2*g+1]])
	}
	if r.secretGroup > 0 {
		return group(r.secretGroup)
	}
	for g := 1; 2*g+1 < len(m); g++ {
		if s := group(g); s != "" {
			return s
		}
	}
	return group(0)
}

// dedupe keeps, of findings with the same line and fingerprint, a specific rule over
// generic-api-key (as gitleaks does), else the one whose rule sorts first; the result is sorted
// by line, then rule.
func dedupe(found []Finding) []Finding {
	const generic = "generic-api-key"
	sort.Slice(found, func(i, j int) bool {
		a, b := found[i], found[j]
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Rule != b.Rule {
			if a.Rule == generic || b.Rule == generic {
				return b.Rule == generic
			}
			return a.Rule < b.Rule
		}
		return a.Fingerprint < b.Fingerprint
	})
	type key struct {
		line int
		fp   string
	}
	seen := map[key]bool{}
	out := found[:0]
	for _, f := range found {
		k := key{f.Line, f.Fingerprint}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, f)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		return a.Fingerprint < b.Fingerprint
	})
	return out
}
