package server

import (
	"fmt"
	"math/rand"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// packAll is the packing as it was before it read a section through:
// all units at once, the parts as [from, to). The packer must cut the
// same parts.
func packAll(units []unit, budget int64) [][2]int {
	var parts [][2]int
	start := 0
	for start < len(units) {
		var n int64
		end := start
		for end < len(units) && n+int64(len(units[end].text)) <= budget {
			n += int64(len(units[end].text))
			end++
		}
		if end == len(units) {
			parts = append(parts, [2]int{start, end})
			break
		}
		if end == start {
			end = start + 1
		} else {
			var m int64
			best := -1
			for k := start; k < end; k++ {
				if k > start && units[k].head && m >= budget/2 {
					best = k
				}
				m += int64(len(units[k].text))
			}
			if best > start {
				end = best
			}
		}
		parts = append(parts, [2]int{start, end})
		start = end
	}
	if len(parts) == 0 {
		parts = append(parts, [2]int{0, 0})
	}
	return parts
}

func TestPackerCutsAsBefore(t *testing.T) {
	rnd := rand.New(rand.NewSource(1))
	const budget = 200
	for round := range 2000 {
		units := make([]unit, rnd.Intn(60))
		for i := range units {
			n := 1 + rnd.Intn(40)
			if rnd.Intn(25) == 0 {
				n = budget + rnd.Intn(300) // a line longer than a part
			}
			units[i] = unit{line: i + 1, text: strings.Repeat("x", n-1) + "\n", head: rnd.Intn(6) == 0}
			if rnd.Intn(15) == 0 {
				units[i].line = 0 // a subsection left out
			}
		}
		want := packAll(units, budget)
		for part := 1; part <= len(want)+1; part++ {
			p := packer{budget: budget, want: part}
			for _, u := range units {
				p.add(u)
			}
			p.end()
			if p.parts != len(want) {
				t.Fatalf("round %d: %d parts, want %d", round, p.parts, len(want))
			}
			if part > len(want) {
				continue
			}
			w := units[want[part-1][0]:want[part-1][1]]
			var got, exp []int
			for _, u := range p.part {
				got = append(got, u.line)
				if int64(len(u.text)) > budget {
					t.Fatalf("round %d part %d: a line of %d bytes", round, part, len(u.text))
				}
			}
			for _, u := range w {
				exp = append(exp, u.line)
			}
			if len(got) != len(exp) || (len(got) > 0 && !reflect.DeepEqual(got, exp)) {
				t.Fatalf("round %d part %d: lines %v, want %v", round, part, got, exp)
			}
			next := 0
			if part < len(want) {
				next = units[want[part][0]].line
			}
			if p.next != next {
				t.Fatalf("round %d part %d: next %d, want %d", round, part, p.next, next)
			}
		}
	}
}

func TestContainsFold(t *testing.T) {
	rnd := rand.New(rand.NewSource(2))
	alphabet := []string{"a", "A", "b", "B", "k", "K", "i", "I", " ", "é", "É", "\u212a", "\u0130", "ß", "1", "\u00a0"}
	text := func(n int) string {
		var b strings.Builder
		for range n {
			b.WriteString(alphabet[rnd.Intn(len(alphabet))])
		}
		return b.String()
	}
	for range 20000 {
		s := text(rnd.Intn(30))
		q := strings.ToLower(text(1 + rnd.Intn(3)))
		if !isASCII(q) {
			continue
		}
		if got, want := containsFold(s, q), strings.Contains(strings.ToLower(s), q); got != want {
			t.Fatalf("containsFold(%q, %q) = %v, want %v", s, q, got, want)
		}
	}
}

// The fs-compat tools keep to what one call returns, too.
func TestCompatWithinMaxRead(t *testing.T) {
	s, dirs := newServer(t, 4096, "app")
	s.FSCompat = true
	var big strings.Builder
	big.WriteString("# Big\n")
	for i := range 400 {
		write(t, filepath.Join(dirs["app"], "many", fmt.Sprintf("document-number-%03d.md", i)), "# D\n")
		fmt.Fprintf(&big, "\n## Heading number %d\n\ntext\n", i)
	}
	write(t, filepath.Join(dirs["app"], "big.md"), big.String())
	for name, args := range map[string]map[string]any{
		"search_files":   {"path": dirs["app"], "pattern": "**/*.md"},
		"list_directory": {"path": filepath.Join(dirs["app"], "many")},
		"outline_file":   {"path": filepath.Join(dirs["app"], "big.md")},
	} {
		text := mustOK(t, call(t, s, name, args))
		if len(text) > 4096 {
			t.Errorf("%s: %d bytes, more than one call returns", name, len(text))
		}
		if !strings.Contains(text, "(stopped at ") && !strings.Contains(text, "(more headings than one call returns") {
			t.Errorf("%s does not say it stopped: …%s", name, text[len(text)-120:])
		}
	}
}
