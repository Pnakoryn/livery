package content

import "strings"

// Frontmatter is a Markdown file split into its frontmatter lines and its body. It is not YAML:
// a frontmatter line is a key: value line, a "- " list item, blank or a # comment. The scanner
// decides nothing about which keys are allowed; the parsers that use it do.
type Frontmatter struct {
	// Opened: the first line is ---. Without it the whole text is the body.
	Opened bool
	// Unclosed: the frontmatter is opened and never closed with a --- line.
	Unclosed bool
	// Items are the list items before the first key line.
	Items []FrontmatterItem
	// Fields are the key lines, in file order.
	Fields []FrontmatterField
	// Malformed is the file line of the first line that is neither key: value, a list item,
	// blank nor a comment (0 if there is none). Scanning stops there: every parser rejects it,
	// so Items and Fields only hold what comes before it.
	Malformed int
	// Body is the text after the closing --- with leading newlines trimmed, or the whole text
	// when there is no frontmatter. CRLF line ends become LF.
	Body string
}

// FrontmatterField is a key: value line and the list items that follow it.
type FrontmatterField struct {
	Line  int // file line, from 1
	Key   string
	Value string // trimmed, otherwise as written (quotes and [...] are kept)
	Items []FrontmatterItem
}

// FrontmatterItem is a "- item" line; Value is the trimmed text after the dash.
type FrontmatterItem struct {
	Line  int
	Value string
}

// ScanFrontmatter splits data into its frontmatter lines and its body.
func ScanFrontmatter(data []byte) Frontmatter {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if lines[0] != "---" {
		return Frontmatter{Body: text}
	}
	fm := Frontmatter{Opened: true}
	end := -1
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		fm.Unclosed = true
		return fm
	}
	fm.Body = strings.TrimLeft(strings.Join(lines[end+1:], "\n"), "\n")
	for i := 1; i < end; i++ {
		n := i + 1 // file line
		line := strings.TrimSpace(lines[i])
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
			continue
		case line == "-" || strings.HasPrefix(line, "- "):
			item := FrontmatterItem{Line: n, Value: strings.TrimSpace(strings.TrimPrefix(line, "-"))}
			if len(fm.Fields) == 0 {
				fm.Items = append(fm.Items, item)
			} else {
				f := &fm.Fields[len(fm.Fields)-1]
				f.Items = append(f.Items, item)
			}
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			fm.Malformed = n
			break
		}
		fm.Fields = append(fm.Fields, FrontmatterField{Line: n, Key: strings.TrimSpace(key), Value: strings.TrimSpace(value)})
	}
	return fm
}
