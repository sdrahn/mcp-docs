package asciidoc

import (
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Inline markup, in the order it is taken: what is literal first (it
// holds no other markup), then macros, then formatting.
var (
	passMacro   = regexp.MustCompile(`pass:[a-z,]*\[((?:[^\]\\]|\\.)*)\]`)
	triplePlus  = regexp.MustCompile(`\+\+\+(.+?)\+\+\+`)
	doublePlus  = regexp.MustCompile(`\+\+(.+?)\+\+`)
	monoLiteral = regexp.MustCompile("`\\+([^`\n]+?)\\+`")
	monoDouble  = regexp.MustCompile("``(.+?)``")
	mono        = regexp.MustCompile("`([^`\n]+?)`")
	singlePlus  = regexp.MustCompile(`(^|[\s(\[{,;:"'>])\+([^\s+](?:[^+\n]*[^\s+])?)\+($|[\s)\]},;:.!?"'<])`)

	inlineImage = regexp.MustCompile(`image:([^\s:\[][^\s\[]*)\[((?:[^\]\\]|\\.)*)\]`)
	iconMacro   = regexp.MustCompile(`icon:[A-Za-z0-9_-]+\[[^\]]*\]`)
	kbdMacro    = regexp.MustCompile(`(kbd|btn):\[((?:[^\]\\]|\\.)*)\]`)
	menuMacro   = regexp.MustCompile(`menu:([^\[\n]+?)\[((?:[^\]\\]|\\.)*)\]`)
	footnote    = regexp.MustCompile(`footnote(?:ref)?:[A-Za-z0-9_-]*\[((?:[^\]\\]|\\.)*)\]`)
	xrefMacro   = regexp.MustCompile(`xref:([^\s\[]+)\[((?:[^\]\\]|\\.)*)\]`)
	// A link whose target an attribute left empty: its text.
	emptyLink = regexp.MustCompile(`(?:xref|link):\[((?:[^\]\\]|\\.)*)\]`)
	angleRef  = regexp.MustCompile(`<<([^,>\n]+?)(?:,\s*([^>\n]*?))?>>`)
	linkMacro = regexp.MustCompile(`(?:link:|mailto:)([^\s\[]+)\[((?:[^\]\\]|\\.)*)\]`)
	urlMacro  = regexp.MustCompile(`((?:https?|ftp|irc)://[^\s\[\]<>]+)\[((?:[^\]\\]|\\.)*)\]`)
	bareURL   = regexp.MustCompile(`(^|[\s(<])((?:https?|ftp)://[^\s\[\]<>]*[^\s\[\]<>.,;:!?)'"])`)
	inlineID  = regexp.MustCompile(`\[\[[^\]\n]+\]\]|anchor:[^\s\[]+\[[^\]]*\]`)
	roleSpan  = regexp.MustCompile(`\[[.#%][^\]\n]*\]#([^#\n]+)#`)

	boldU   = regexp.MustCompile(`\*\*(.+?)\*\*`)
	bold    = regexp.MustCompile(`(^|[^\pL\pN_*\\])\*(\S(?:[^*\n]*?\S)?)\*($|[^\pL\pN_*])`)
	italicU = regexp.MustCompile(`__(.+?)__`)
	italic  = regexp.MustCompile(`(^|[^\pL\pN_\\])_(\S(?:[^_\n]*?\S)?)_($|[^\pL\pN_])`)
	mark    = regexp.MustCompile(`(^|[^\pL\pN#&\\])#(\S(?:[^#\n]*?\S)?)#($|[^\pL\pN#])`)
	super   = regexp.MustCompile(`\^(\S+?)\^`)
	sub     = regexp.MustCompile(`~(\S+?)~`)
)

const (
	held    = '\x00' // a held text: \x00 N \x00
	boldM   = "\x02"
	italicM = "\x03"
)

// linker says where a page's links lead.
type linker interface {
	// xref resolves the target of a cross-reference (a resource id of a
	// page, with or without a fragment) from the page it is in: the
	// link's address, and the title to show where the link has no text.
	xref(target, from string) (href, title string, ok bool)
}

// inliner turns a line's inline markup into Markdown.
type inliner struct {
	links linker
	page  string // the page, as its links' targets are relative to
	// anchors are the titles of the page's sections, by id; titles
	// their ids, by title (a reference may name a section by its
	// title).
	anchors map[string]string
	titles  map[string]string
	// local, if set, finds the section of this page that an id names
	// ("" for the page itself).
	local  func(id string) (section string, ok bool)
	broken map[string]bool
}

func (in *inliner) inline(s string) string {
	if s == "" {
		return ""
	}
	var holds []string
	hold := func(x string) string {
		holds = append(holds, x)
		return string(held) + strconv.Itoa(len(holds)-1) + string(held)
	}
	sub1 := func(re *regexp.Regexp, s string, f func(m []string) string) string {
		return re.ReplaceAllStringFunc(s, func(x string) string { return f(re.FindStringSubmatch(x)) })
	}
	// Literal text.
	s = sub1(passMacro, s, func(m []string) string { return hold(unescapeBracket(m[1])) })
	s = sub1(triplePlus, s, func(m []string) string { return hold(m[1]) })
	s = sub1(monoLiteral, s, func(m []string) string { return hold(codeSpan(m[1])) })
	s = sub1(monoDouble, s, func(m []string) string { return hold(codeSpan(strings.Trim(m[1], "+"))) })
	s = sub1(mono, s, func(m []string) string { return hold(codeSpan(m[1])) })
	s = sub1(doublePlus, s, func(m []string) string { return hold(escapeText(m[1])) })
	s = sub1(singlePlus, s, func(m []string) string { return m[1] + hold(escapeText(m[2])) + m[3] })

	// Macros.
	s = iconMacro.ReplaceAllString(s, "")
	s = inlineID.ReplaceAllString(s, "")
	s = sub1(inlineImage, s, func(m []string) string {
		if alt := firstAttr(m[2]); alt != "" {
			return hold("*" + escapeText(alt) + "*")
		}
		return ""
	})
	s = sub1(kbdMacro, s, func(m []string) string {
		if m[1] == "btn" {
			return hold("**" + escapeText(unescapeBracket(m[2])) + "**")
		}
		return hold(codeSpan(unescapeBracket(m[2])))
	})
	s = sub1(menuMacro, s, func(m []string) string {
		items := strings.TrimSpace(m[1])
		if rest := strings.TrimSpace(unescapeBracket(m[2])); rest != "" {
			items += " > " + rest
		}
		return hold("**" + escapeText(items) + "**")
	})
	s = sub1(footnote, s, func(m []string) string {
		if m[1] == "" {
			return ""
		}
		return hold(" (" + in.inline(unescapeBracket(m[1])) + ")")
	})
	s = sub1(xrefMacro, s, func(m []string) string { return hold(in.ref(m[1], linkText(m[2]))) })
	s = sub1(emptyLink, s, func(m []string) string { return in.inline(linkText(m[1])) })
	s = sub1(angleRef, s, func(m []string) string { return hold(in.ref(strings.TrimSpace(m[1]), strings.TrimSpace(m[2]))) })
	s = sub1(linkMacro, s, func(m []string) string { return hold(in.link(m[1], linkText(m[2]))) })
	s = sub1(urlMacro, s, func(m []string) string { return hold(in.link(m[1], linkText(m[2]))) })
	s = sub1(bareURL, s, func(m []string) string { return m[1] + hold("<"+m[2]+">") })
	s = sub1(roleSpan, s, func(m []string) string { return m[1] })

	// Formatting: marked first, so that what is left of "*" and "_" is
	// text.
	s = boldU.ReplaceAllString(s, boldM+"$1"+boldM)
	s = bold.ReplaceAllString(s, "$1"+boldM+"$2"+boldM+"$3")
	s = bold.ReplaceAllString(s, "$1"+boldM+"$2"+boldM+"$3") // a neighbour the first pass's match took the edge of
	s = italicU.ReplaceAllString(s, italicM+"$1"+italicM)
	s = italic.ReplaceAllString(s, "$1"+italicM+"$2"+italicM+"$3")
	s = italic.ReplaceAllString(s, "$1"+italicM+"$2"+italicM+"$3")
	s = mark.ReplaceAllString(s, "$1$2$3")
	s = super.ReplaceAllString(s, "^$1")
	s = sub.ReplaceAllString(s, "$1")
	s = escapeText(s)
	s = strings.NewReplacer(boldM, "**", italicM, "*", "(C)", "©", "(R)", "®", "(TM)", "™", " -- ", " — ").Replace(s)

	// The held texts back, innermost last held first.
	for i := len(holds) - 1; i >= 0; i-- {
		s = strings.ReplaceAll(s, string(held)+strconv.Itoa(i)+string(held), holds[i])
	}
	return s
}

// plain is a text without its inline markup: a title as an id or a
// link's text is made of.
func plainText(s string) string {
	s = passMacro.ReplaceAllString(s, "$1")
	s = xrefMacro.ReplaceAllString(s, "$2")
	s = linkMacro.ReplaceAllString(s, "$2")
	s = urlMacro.ReplaceAllString(s, "$2")
	s = inlineID.ReplaceAllString(s, "")
	s = strings.NewReplacer("`", "", "*", "").Replace(s)
	return strings.TrimSpace(s)
}

// ref renders a cross-reference: to a page (and a section of it), or to
// a section of this page.
func (in *inliner) ref(target, text string) string {
	if id, ok := in.titles[plainText(target)]; ok {
		if text == "" {
			text = target
		}
		return "[" + in.inline(text) + "](#" + id + ")"
	}
	if !strings.Contains(target, ".adoc") && !strings.ContainsAny(target, ":$/") {
		// A section of this page.
		id := strings.TrimPrefix(target, "#")
		if text == "" {
			text = in.anchors[id]
		}
		if text == "" {
			text = id
		}
		if in.local != nil {
			section, ok := in.local(id)
			switch {
			case !ok:
				if in.broken != nil {
					in.broken["#"+id] = true
				}
				return in.inline(text)
			case section == "":
				return in.inline(text) // the page itself
			}
			id = section
		}
		return "[" + in.inline(text) + "](#" + id + ")"
	}
	if strings.HasPrefix(target, "#") {
		return in.ref(target[1:], text)
	}
	href, title, ok := "", "", false
	if in.links != nil {
		href, title, ok = in.links.xref(target, in.page)
	}
	if text == "" {
		text = title
	}
	if !ok {
		if in.broken != nil {
			in.broken[target] = true
		}
		if text == "" {
			text = strings.TrimSuffix(path.Base(strings.SplitN(target, "#", 2)[0]), ".adoc")
		}
		return in.inline(text)
	}
	if text == "" {
		text = href
	}
	return "[" + in.inline(text) + "](" + href + ")"
}

func (in *inliner) link(url, text string) string {
	if text == "" {
		return "<" + url + ">"
	}
	return "[" + in.inline(text) + "](" + url + ")"
}

// linkText is a link's text from the macro's attributes: the first,
// without the "^" that opens it in a new window, and without the named
// attributes after it.
func linkText(attrs string) string {
	t := firstAttr(attrs)
	return strings.TrimSpace(strings.TrimSuffix(t, "^"))
}

// firstAttr is the first positional attribute of a macro's list.
func firstAttr(attrs string) string {
	attrs = strings.TrimSpace(unescapeBracket(attrs))
	if attrs == "" {
		return ""
	}
	if attrs[0] == '"' {
		if end := strings.Index(attrs[1:], `"`); end >= 0 {
			return attrs[1 : 1+end]
		}
	}
	// Named attributes follow a comma: "text, role=x", "text,window=_blank".
	if i := namedAttrStart(attrs); i >= 0 {
		attrs = attrs[:i]
	}
	if strings.Contains(attrs, "=") && !strings.ContainsAny(attrs, " ") {
		return "" // only a named attribute
	}
	return strings.TrimSpace(attrs)
}

var namedAttr = regexp.MustCompile(`,\s*(?:[a-z][a-z0-9_-]*=|\d+\s*(?:,|$))`)

func namedAttrStart(attrs string) int {
	if loc := namedAttr.FindStringIndex(attrs); loc != nil {
		return loc[0]
	}
	return -1
}

func unescapeBracket(s string) string { return strings.ReplaceAll(s, `\]`, "]") }

// codeSpan is a Markdown code span of a text, with as many backticks as
// keep the text's own from ending it.
func codeSpan(s string) string {
	fence := "`"
	for strings.Contains(s, fence) {
		fence += "`"
	}
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		return fence + " " + s + " " + fence
	}
	return fence + s + fence
}

// escapeText keeps text from being read as Markdown emphasis or code.
func escapeText(s string) string {
	if !strings.ContainsAny(s, "\\*`") {
		return s
	}
	return strings.NewReplacer("\\", "\\\\", "*", "\\*", "`", "\\`").Replace(s)
}
