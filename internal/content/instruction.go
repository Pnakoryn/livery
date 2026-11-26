// Package content reads the shared repository's content: instructions, skills and so on.
package content

import (
	"strings"

	"github.com/Pnakoryn/livery/internal/exit"
)

// Instruction is one always-on guidance file, instructions/<name>.md.
type Instruction struct {
	Description string
	// Globs lists the files the instruction applies to; empty means always on.
	Globs []string
	Body  string
}

// ParseInstruction parses an instruction file. The frontmatter is a small fixed format (keys
// description and globs), not YAML. path is only used in error messages.
func ParseInstruction(path string, data []byte) (Instruction, error) {
	fm := ScanFrontmatter(data)
	if !fm.Opened {
		return Instruction{Body: fm.Body}, nil
	}
	if fm.Unclosed {
		return Instruction{}, exit.Usagef("%s: frontmatter starts with --- but is never closed; add a closing --- line", path)
	}
	var ins Instruction
	if err := parseFrontmatter(path, fm, &ins); err != nil {
		return Instruction{}, err
	}
	ins.Body = fm.Body
	return ins, nil
}

func parseFrontmatter(path string, fm Frontmatter, ins *Instruction) error {
	if len(fm.Items) > 0 {
		return exit.Usagef("%s:%d: list item outside globs; put it under a globs: line or remove it", path, fm.Items[0].Line)
	}
	for _, f := range fm.Fields {
		switch f.Key {
		case "description":
			ins.Description = unquote(f.Value)
		case "globs":
			globs, err := parseGlobs(path, f.Line, f.Value)
			if err != nil {
				return err
			}
			ins.Globs = globs
		default:
			return exit.Usagef("%s:%d: unknown frontmatter key %q; only description and globs are allowed, remove the line", path, f.Line, f.Key)
		}
		for _, item := range f.Items {
			if f.Key != "globs" {
				return exit.Usagef("%s:%d: list item outside globs; put it under a globs: line or remove it", path, item.Line)
			}
			ins.Globs = appendGlob(ins.Globs, item.Value)
		}
	}
	if fm.Malformed > 0 {
		return exit.Usagef("%s:%d: frontmatter line is not key: value; fix or remove it", path, fm.Malformed)
	}
	return nil
}

func parseGlobs(path string, n int, value string) ([]string, error) {
	switch {
	case value == "":
		return nil, nil
	case strings.HasPrefix(value, "["):
		inner, ok := strings.CutSuffix(value, "]")
		if !ok {
			return nil, exit.Usagef("%s:%d: globs list has no closing ]; add it", path, n)
		}
		inner = strings.TrimSpace(strings.TrimPrefix(inner, "["))
		if inner == "" {
			return nil, nil
		}
		items, err := splitGlobs(path, n, inner)
		if err != nil {
			return nil, err
		}
		var out []string
		for _, item := range items {
			out = appendGlob(out, item)
		}
		return out, nil
	default:
		return appendGlob(nil, value), nil
	}
}

// splitGlobs splits an inline list body at commas outside quotes and outside {...}. There are
// no escape sequences. n is the file line, for error messages.
func splitGlobs(path string, n int, inner string) ([]string, error) {
	var items []string
	var quote byte
	depth, start := 0, 0
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '{':
			depth++
		case c == '}':
			if depth > 0 {
				depth--
			}
		case c == ',' && depth == 0:
			items = append(items, inner[start:i])
			start = i + 1
		}
	}
	if quote != 0 {
		return nil, exit.Usagef("%s:%d: globs list has an unterminated quote; close the quote", path, n)
	}
	if depth != 0 {
		return nil, exit.Usagef("%s:%d: globs list has a { without a closing }; add the }", path, n)
	}
	return append(items, inner[start:]), nil
}

// appendGlob appends the unquoted item unless it is empty: an empty glob would match nothing
// and make the instruction no longer always on, so it is dropped.
func appendGlob(globs []string, item string) []string {
	if g := unquote(item); g != "" {
		return append(globs, g)
	}
	return globs
}

// unquote trims s and removes one pair of surrounding quotes.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
