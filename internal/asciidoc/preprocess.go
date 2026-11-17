// Package asciidoc converts the pages of an Antora component, written
// in AsciiDoc, into a collection of Markdown documents for mcp-docs
// (docs/architecture.md, §18): SUSE's documentation of its cloud-native
// products (Rancher, RKE2, K3s, …) is written so. It reads the AsciiDoc
// those sources use, not all there is: what it does not know it reports,
// and a strict conversion fails on it.
package asciidoc

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// builtins are AsciiDoc's own attributes for characters.
var builtins = map[string]string{
	"nbsp": " ", "zwsp": "", "wj": "", "empty": "", "blank": "", "sp": " ", "plus": "+", "pp": "++", "cpp": "C++",
	"startsb": "[", "endsb": "]", "vbar": "|", "caret": "^", "tilde": "~", "backtick": "`", "asterisk": "*",
	"apos": "'", "quot": "\"", "lt": "<", "gt": ">", "amp": "&", "two-colons": "::", "two-semicolons": ";;",
	"deg": "°", "lsquo": "‘", "rsquo": "’", "ldquo": "“", "rdquo": "”", "brvbar": "¦", "backslash": "\\",
}

var (
	attrEntry   = regexp.MustCompile(`^:(!?)([A-Za-z0-9_][A-Za-z0-9_-]*)(!?):(?:[ \t]+(.*))?$`)
	attrRef     = regexp.MustCompile(`\\?\{([A-Za-z0-9_][A-Za-z0-9_-]*)\}`)
	conditional = regexp.MustCompile(`^(ifdef|ifndef|ifeval|endif)::([^\[]*)\[(.*)\]\s*$`)
	includeLine = regexp.MustCompile(`^include::([^\[]+)\[(.*)\]\s*$`)
	evalExpr    = regexp.MustCompile(`^\s*(.+?)\s*(==|!=|>=|<=|>|<)\s*(.+?)\s*$`)
	// A section title: "== Title", or as Markdown has it, "## Title",
	// which AsciiDoc takes too.
	headingLine = regexp.MustCompile(`^(={1,6}|#{1,6})[ \t]+(\S.*)$`)
	tagLine     = regexp.MustCompile(`(?:tag|end)::[A-Za-z0-9_.-]+\[\]`)
)

// preprocessor reads a document's lines as AsciiDoc's preprocessor and
// its attribute entries have them: conditionals decided, includes read,
// attribute entries applied and attribute references replaced, comments
// left out. What is left are the lines of blocks.
type preprocessor struct {
	attrs map[string]string
	// include returns the lines of an include's target, as it is named
	// in the file from.
	include func(target, from string) (lines []string, file string, err error)
	// problems are what could not be read (a missing include).
	problems []string
}

// verbatimDelimiter reports whether a line opens or closes a block whose
// text is taken as it is: a listing, a literal, a passthrough, a
// comment, or a fenced code block.
func verbatimDelimiter(l string) (string, bool) {
	t := strings.TrimRight(l, " \t")
	if strings.HasPrefix(t, "```") {
		return "```", true
	}
	if len(t) < 4 {
		return "", false
	}
	switch c := t[0]; c {
	case '-', '.', '+', '/':
		if strings.Trim(t, string(c)) == "" {
			return t, true
		}
	}
	return "", false
}

func (p *preprocessor) run(lines []string, file string, depth int) []string {
	var out []string
	var active []bool // the conditionals open, and whether their text is read
	reading := func() bool {
		for _, a := range active {
			if !a {
				return false
			}
		}
		return true
	}
	verbatim := ""      // the delimiter of the verbatim block open
	substitute := false // attribute references are replaced in it ([subs=attributes])
	nextSubs := false
	for i := 0; i < len(lines); i++ {
		l := strings.TrimRight(lines[i], "\r")
		// Conditionals and includes are the preprocessor's: in any block
		// but a comment.
		if !strings.HasPrefix(verbatim, "////") {
			if m := conditional.FindStringSubmatch(l); m != nil {
				switch m[1] {
				case "endif":
					if len(active) > 0 {
						active = active[:len(active)-1]
					}
				case "ifdef", "ifndef":
					set := p.defined(m[2])
					if m[1] == "ifndef" {
						set = !set
					}
					if m[3] != "" { // ifdef::attr[text]: one line
						if set && reading() {
							out = append(out, p.refs(m[3]))
						}
					} else {
						active = append(active, set)
					}
				case "ifeval":
					active = append(active, p.eval(m[3]))
				}
				continue
			}
			if !reading() {
				continue
			}
			if m := includeLine.FindStringSubmatch(l); m != nil && p.include != nil {
				target := p.refs(m[1])
				if depth > 16 {
					p.problems = append(p.problems, fmt.Sprintf("%s: includes nested too deep (%s)", file, target))
					continue
				}
				inc, incFile, err := p.include(target, file)
				if err != nil {
					p.problems = append(p.problems, fmt.Sprintf("%s: include %s: %v", file, target, err))
					continue
				}
				inc = includeOptions(inc, m[2])
				out = append(out, p.run(inc, incFile, depth+1)...)
				continue
			}
		} else if !reading() {
			continue
		}
		if verbatim != "" {
			if d, ok := verbatimDelimiter(l); ok && d == verbatim {
				if !strings.HasPrefix(verbatim, "////") {
					out = append(out, l)
				}
				verbatim = ""
				continue
			}
			switch {
			case strings.HasPrefix(verbatim, "////"):
			case substitute:
				out = append(out, p.refs(l))
			default:
				out = append(out, l)
			}
			continue
		}
		if d, ok := verbatimDelimiter(l); ok {
			verbatim, substitute, nextSubs = d, nextSubs, false
			if !strings.HasPrefix(d, "////") {
				out = append(out, l)
			}
			continue
		}
		if strings.HasPrefix(l, "//") && !strings.HasPrefix(l, "///") {
			continue // a comment line
		}
		if m := attrEntry.FindStringSubmatch(l); m != nil {
			name, value := m[2], m[4]
			// A value goes on while its lines end in " \".
			for strings.HasSuffix(value, " \\") && i+1 < len(lines) {
				i++
				value = strings.TrimSuffix(value, "\\") + strings.TrimSpace(lines[i])
			}
			if m[1] == "!" || m[3] == "!" {
				delete(p.attrs, name)
			} else {
				p.attrs[name] = p.refs(value)
			}
			continue
		}
		if strings.HasPrefix(l, "[") && strings.HasSuffix(strings.TrimSpace(l), "]") {
			nextSubs = strings.Contains(l, "subs=") && (strings.Contains(l, "attributes") || strings.Contains(l, "normal"))
		} else if strings.TrimSpace(l) != "" && !strings.HasPrefix(l, ".") {
			nextSubs = false
		}
		out = append(out, p.refs(l))
	}
	return out
}

// defined decides ifdef's condition: a name, or names of which any
// (",") or all ("+") are set.
func (p *preprocessor) defined(names string) bool {
	if strings.Contains(names, "+") {
		for _, n := range strings.Split(names, "+") {
			if _, ok := p.attrs[strings.TrimSpace(n)]; !ok {
				return false
			}
		}
		return true
	}
	for _, n := range strings.Split(names, ",") {
		if _, ok := p.attrs[strings.TrimSpace(n)]; ok {
			return true
		}
	}
	return false
}

// eval decides ifeval's condition: two values compared, strings in
// quotes or numbers.
func (p *preprocessor) eval(expr string) bool {
	m := evalExpr.FindStringSubmatch(p.refs(expr))
	if m == nil {
		return false
	}
	a, b := unquote(m[1]), unquote(m[3])
	if x, err := strconv.ParseFloat(a, 64); err == nil {
		if y, err := strconv.ParseFloat(b, 64); err == nil {
			switch m[2] {
			case "==":
				return x == y
			case "!=":
				return x != y
			case ">":
				return x > y
			case "<":
				return x < y
			case ">=":
				return x >= y
			case "<=":
				return x <= y
			}
		}
	}
	switch m[2] {
	case "==":
		return a == b
	case "!=":
		return a != b
	case ">":
		return a > b
	case "<":
		return a < b
	case ">=":
		return a >= b
	}
	return a <= b
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

// refs replaces the references to attributes that are set; one to an
// attribute that is not stays as it is written, as Asciidoctor leaves
// it (a "{range}" of a command is no reference).
func (p *preprocessor) refs(l string) string {
	if !strings.Contains(l, "{") {
		return l
	}
	return attrRef.ReplaceAllStringFunc(l, func(ref string) string {
		if ref[0] == '\\' {
			return ref[1:]
		}
		name := ref[1 : len(ref)-1]
		if v, ok := p.attrs[name]; ok {
			return v
		}
		if v, ok := builtins[name]; ok {
			return v
		}
		return ref
	})
}

// includeOptions applies an include's options to its lines: the lines
// of its tags, and a shift of its headings' levels.
func includeOptions(lines []string, opts string) []string {
	named := map[string]string{}
	for _, o := range strings.Split(opts, ",") {
		if k, v, ok := strings.Cut(strings.TrimSpace(o), "="); ok {
			named[k] = unquote(v)
		}
	}
	tags := named["tag"]
	if tags == "" {
		tags = named["tags"]
	}
	var out []string
	if tags != "" {
		want := map[string]bool{}
		for _, t := range strings.FieldsFunc(tags, func(r rune) bool { return r == ';' || r == ',' }) {
			want[t] = true
		}
		in := 0
		for _, l := range lines {
			if m := tagLine.FindString(l); m != "" {
				name := m[strings.Index(m, "::")+2 : len(m)-2]
				if want[name] {
					if strings.HasPrefix(m, "tag") {
						in++
					} else if in > 0 {
						in--
					}
				}
				continue
			}
			if in > 0 {
				out = append(out, l)
			}
		}
	} else {
		for _, l := range lines {
			if tagLine.MatchString(l) && strings.TrimSpace(tagLine.ReplaceAllString(l, "")) == "" ||
				strings.HasPrefix(strings.TrimSpace(l), "//") && tagLine.MatchString(l) {
				continue // a tag's own line
			}
			out = append(out, l)
		}
	}
	if off := named["leveloffset"]; off != "" {
		n, err := strconv.Atoi(strings.TrimPrefix(off, "+"))
		if err == nil && n != 0 {
			verbatim := ""
			for i, l := range out {
				if d, ok := verbatimDelimiter(l); ok {
					if verbatim == "" {
						verbatim = d
					} else if d == verbatim {
						verbatim = ""
					}
					continue
				}
				if m := headingLine.FindStringSubmatch(l); m != nil && verbatim == "" {
					level := min(max(len(m[1])+n, 1), 6)
					out[i] = strings.Repeat("=", level) + " " + m[2]
				}
			}
		}
	}
	return out
}
