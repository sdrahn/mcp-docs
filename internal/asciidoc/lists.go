package asciidoc

import (
	"strconv"
	"strings"
)

// item is an item of a list: of a bulleted or numbered one, or a term
// of a description list with what describes it.
type item struct {
	marker   string // "*", "**", ".", "1.", "::", …: items of one list have one
	ordered  bool
	term     string
	text     []string   // its first paragraph's lines
	attached [][]string // the blocks attached to it with "+", as lines
	children []*item
}

// itemOf reads a line as the start of a list item.
func itemOf(l string) (*item, bool) {
	if m := listMarker.FindStringSubmatch(l); m != nil {
		it := &item{marker: m[1], text: []string{m[2]}}
		switch {
		case m[1][0] == '.':
			it.ordered = true
		case m[1][0] >= '0' && m[1][0] <= '9':
			it.ordered, it.marker = true, "1."
		case m[1][0] == '<':
			it.ordered, it.marker = true, "<1>"
		}
		return it, true
	}
	if m := dlistItem.FindStringSubmatch(l); m != nil && !strings.Contains(m[1], "://") && !blockMacro.MatchString(l) {
		it := &item{marker: m[2], term: m[1]}
		if m[3] != "" {
			it.text = []string{m[3]}
		}
		return it, true
	}
	return nil, false
}

// continuation reports whether a line is a list continuation: a "+"
// of its own, which attaches the block after it to the item.
func continuation(l string) bool { return strings.TrimSpace(l) == "+" }

// blockStart reports whether a line starts a block of its own: a
// delimiter, or what precedes a block.
func blockStart(l string) bool {
	if _, ok := delimiter(l); ok {
		return true
	}
	return (attrListLine.MatchString(l) && !strings.HasPrefix(l, "[[")) || anchorLine.MatchString(l)
}

func isListStart(l string) bool {
	_, ok := itemOf(l)
	return ok
}

// attachedEnd returns the index after the block attached at lines[i]
// (the line after a "+"): what precedes a block, then a delimited block
// to its end, or a paragraph.
func attachedEnd(lines []string, i int) int {
	j := i
	for j < len(lines) && (attrListLine.MatchString(lines[j]) || titleLine.MatchString(lines[j])) {
		j++
	}
	if j < len(lines) {
		if _, ok := delimiter(lines[j]); ok {
			return min(closing(lines, j)+1, len(lines))
		}
	}
	for j < len(lines) && strings.TrimSpace(lines[j]) != "" && !continuation(lines[j]) {
		if _, ok := itemOf(lines[j]); ok && j > i {
			break
		}
		j++
	}
	return j
}

// listEnd returns the index after the list that starts at lines[i]: it
// goes on over blank lines while an item or an attached block follows.
func listEnd(lines []string, i int) int {
	j := i
	for j < len(lines) {
		l := lines[j]
		switch {
		case strings.TrimSpace(l) == "":
			k := j
			for k < len(lines) && strings.TrimSpace(lines[k]) == "" {
				k++
			}
			if k < len(lines) && (isListStart(lines[k]) || continuation(lines[k])) {
				j = k
				continue
			}
			return j
		case continuation(l):
			j = attachedEnd(lines, j+1)
		case j > i && !isListStart(l) && blockStart(l):
			// A block that is not attached ends the list, also without a
			// blank line before it.
			return j
		default:
			j++
		}
	}
	return j
}

// parseList reads a list's lines into its items, nested by their
// markers: an item whose marker an item above has is that one's
// sibling, any other the child of the item before it.
func parseList(lines []string) []*item {
	var roots, stack []*item
	var cur *item
	for i := 0; i < len(lines); {
		l := lines[i]
		if continuation(l) && cur != nil {
			end := attachedEnd(lines, i+1)
			if end > i+1 {
				cur.attached = append(cur.attached, lines[i+1:end])
			}
			i = max(end, i+1)
			continue
		}
		if strings.TrimSpace(l) == "" {
			i++
			continue
		}
		if it, ok := itemOf(l); ok {
			for k := len(stack) - 1; k >= 0; k-- {
				if stack[k].marker == it.marker {
					stack = stack[:k]
					break
				}
			}
			if len(stack) == 0 {
				roots = append(roots, it)
			} else {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, it)
			}
			stack = append(stack, it)
			cur = it
			i++
			continue
		}
		if cur != nil {
			if t := strings.TrimSpace(l); blockMacro.MatchString(t) {
				cur.attached = append(cur.attached, []string{t}) // an image under the item
			} else {
				cur.text = append(cur.text, t)
			}
		}
		i++
	}
	return roots
}

func (r *renderer) list(lines []string, m *meta) []block {
	var out []block
	if m.title != "" {
		out = append(out, block{"**" + r.in.inline(m.title) + "**"})
	}
	return append(out, r.listItems(parseList(lines), 0)...)
}

// listItems renders the items of one list as one block: an item's
// attached blocks and the lists below it are indented under it, with
// blank lines around blocks.
func (r *renderer) listItems(items []*item, _ int) []block {
	if len(items) == 0 {
		return nil
	}
	var lines block
	n := 0
	blank := func() {
		if len(lines) > 0 && lines[len(lines)-1] != "" {
			lines = append(lines, "")
		}
	}
	for _, it := range items {
		prefix := "- "
		if it.ordered {
			n++
			prefix = strconv.Itoa(n) + ". "
		}
		indent := strings.Repeat(" ", len(prefix))
		text := r.text(it.text)
		if it.term != "" {
			term := "**" + r.in.inline(strings.TrimSpace(it.term)) + "**"
			if text != "" {
				text = term + ": " + text
			} else {
				text = term
			}
		}
		lines = append(lines, prefix+text)
		loose := false
		for _, a := range it.attached {
			for _, b := range r.blocks(a) {
				blank()
				for _, l := range b {
					if l == "" {
						lines = append(lines, "")
					} else {
						lines = append(lines, indent+l)
					}
				}
				loose = true
			}
		}
		for _, b := range r.listItems(it.children, 0) {
			if loose {
				blank()
			}
			for _, l := range b {
				if l == "" {
					lines = append(lines, "")
				} else {
					lines = append(lines, indent+l)
				}
			}
		}
		if loose {
			blank()
		}
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return []block{lines}
}
