// Package eval measures what answers cost (docs/architecture.md, §10):
// for each question over a fixed corpus (a snapshot of mcp-gateway's
// documentation, testdata/mcp-gateway), it makes the tool calls the
// server's instructions lead an agent to, deterministically, and counts
// the bytes they return. A question fails when its calls miss the
// section that answers it or cost more than its budget. It is the
// regression test for every change to tools, defaults or instructions.
//
//	make eval                          # the table
//	EVAL_REPORT=report.md make eval    # also as Markdown (CI: the job summary)
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/sdrahn/mcp-docs/internal/catalog"
	"github.com/sdrahn/mcp-docs/internal/server"
)

// A question and how an agent goes about it.
type question struct {
	Ask string
	// Search: the precise term (search, then the match's section, or
	// the lines around the match when its section is large).
	Query string
	// Outline: the document, and the headings from its outline down to
	// the section that answers (outline, outline of a section, …,
	// read_section).
	Path []string
	// Doc limits a search to a document; it is the document of an
	// outline.
	Doc string
	// Want is the id of the section that answers; Text is in the
	// answer.
	Want, Text string
}

// largeSection is the size from which an agent reads the lines around
// a match rather than the match's section.
const largeSection = 8 << 10

// aroundLines are the lines read around a match: from 3 before.
const aroundLines = 40

var questions = []question{
	{Ask: "What does agents.no_request_timeout do?", Query: "no_request_timeout",
		Doc: "mcp-gateway/user-guide/03-configuration.md", Want: "protocol-versions-per-client", Text: "no request timeout of their own"},
	{Ask: "How do I run the gateway's self-check?", Doc: "mcp-gateway/user-guide/10-operations.md",
		Path: []string{"Troubleshooting", "Self-check"}, Want: "self-check", Text: "doctor"},
	{Ask: "Who may approve a call?", Doc: "mcp-gateway/user-guide/07-approvals.md",
		Path: []string{"Who may approve"}, Want: "who-may-approve", Text: "Approver rules"},
	{Ask: "How long does an approval last (once, session)?", Doc: "mcp-gateway/user-guide/07-approvals.md",
		Path: []string{"Scopes and grants"}, Want: "scopes-and-grants", Text: "session"},
	{Ask: "What does \"outside the allowed directories\" mean?", Query: "outside the allowed directories",
		Doc: "mcp-gateway/README.md", Want: "error-messages", Text: "outside the allowed directories"},
	{Ask: "How is the policy changed and rolled out?", Doc: "mcp-gateway/architecture.md",
		Path: []string{"6. Policy model", "6.6 Policy lifecycle"}, Want: "66-policy-lifecycle", Text: "bundle"},
	{Ask: "Do token scopes limit what a role allows?", Doc: "mcp-gateway/architecture.md",
		Path: []string{"6. Policy model", "6.7 Token scopes as a ceiling"}, Want: "67-token-scopes-as-a-ceiling", Text: "scope"},
	{Ask: "What did decision D13 decide?", Query: "**D13", Doc: "mcp-gateway/architecture.md",
		Want: "9-decisions", Text: "D13"},
	{Ask: "Which metrics does the gateway export?", Doc: "mcp-gateway/user-guide/10-operations.md",
		Path: []string{"Monitoring", "Metrics"}, Want: "metrics", Text: "mcp_"},
	{Ask: "What must I back up?", Query: "backup", Doc: "mcp-gateway/user-guide/10-operations.md",
		Want: "state-and-backup", Text: "/var/lib/mcp-gateway"},
	{Ask: "Calls are denied: where do I look?", Doc: "mcp-gateway/user-guide/10-operations.md",
		Path: []string{"Troubleshooting", "Calls fail"}, Want: "calls-fail", Text: "denied by policy"},
	{Ask: "How do I give a server a secret?", Doc: "mcp-gateway/user-guide/04-mcp-servers.md",
		Path: []string{"Secrets"}, Want: "secrets", Text: "credentials"},
	{Ask: "What is the roadmap step on Landlock?", Query: "**Landlock as a second wall**", Doc: "mcp-gateway/architecture.md",
		Want: "11-roadmap", Text: "Landlock"},
}

type step struct {
	tool  string
	bytes int
}

type outcome struct {
	q        question
	steps    []step
	cost     int
	baseline int64
	got      string // the section read
	ok       bool
	why      string
}

func newServer(t *testing.T) *server.Server {
	t.Helper()
	k := &catalog.Collection{Name: "mcp-gateway", Path: abs(t, "testdata/mcp-gateway"),
		Description: "mcp-gateway's documentation (a snapshot)"}
	return &server.Server{Cat: catalog.New([]*catalog.Collection{k}, []string{".md"}, 10000),
		MaxRead: 32 << 10, MaxFile: 16 << 20, Version: "eval"}
}

func abs(t *testing.T, p string) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd + "/" + p
}

// session calls tools through the server's MCP handler, as a client.
type session struct {
	t     *testing.T
	s     *server.Server
	steps []step
}

type reply struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	IsError    bool            `json:"isError"`
	Structured json.RawMessage `json:"structuredContent"`
}

func (c *session) call(name string, args map[string]any) (string, json.RawMessage, error) {
	params, _ := json.Marshal(map[string]any{"name": name, "arguments": args})
	res, err := c.s.Handle(context.Background(), "tools/call", params)
	if err != nil {
		return "", nil, err
	}
	b, _ := json.Marshal(res)
	var r reply
	if err := json.Unmarshal(b, &r); err != nil {
		return "", nil, err
	}
	text := ""
	if len(r.Content) > 0 {
		text = r.Content[0].Text
	}
	c.steps = append(c.steps, step{name, len(text)})
	if r.IsError {
		return text, nil, fmt.Errorf("%s: %s", name, text)
	}
	return text, r.Structured, nil
}

type match struct {
	Doc          string
	Line         int
	Version      string
	Section      string
	Path         []string
	SectionBytes int64
}

// choose is the match an agent reads: where the term is discussed, the
// section below the document's title with the most matches (the first
// of those), else the first match.
func choose(ms []match) match {
	count := map[string]int{}
	for _, m := range ms {
		if len(m.Path) > 1 {
			count[m.Doc+"#"+m.Section]++
		}
	}
	best := ms[0]
	n := 0
	for _, m := range ms {
		if c := count[m.Doc+"#"+m.Section]; c > n {
			best, n = m, c
		}
	}
	return best
}

type heading struct {
	Level        int
	Title, ID    string
	Line, Lines  int
	Bytes        int64
	Section      string
	SectionLines int
}

func run(t *testing.T, s *server.Server, q question) outcome {
	c := &session{t: t, s: s}
	o := outcome{q: q}
	if fi, err := os.Stat(abs(t, "testdata/"+q.Doc)); err == nil {
		o.baseline = fi.Size()
	}
	text, err := c.answer(q, &o)
	o.steps, o.cost = c.steps, 0
	for _, st := range c.steps {
		o.cost += st.bytes
	}
	switch {
	case err != nil:
		o.why = err.Error()
	case o.got != q.Want:
		o.why = fmt.Sprintf("read #%s, not #%s", o.got, q.Want)
	case !strings.Contains(text, q.Text):
		o.why = fmt.Sprintf("the answer lacks %q", q.Text)
	default:
		o.ok = true
	}
	return o
}

func (c *session) answer(q question, o *outcome) (string, error) {
	if q.Query != "" {
		args := map[string]any{"query": q.Query, "maxResults": 5}
		if q.Doc != "" {
			args["doc"] = q.Doc
		}
		_, st, err := c.call("search", args)
		if err != nil {
			return "", err
		}
		var res struct {
			Matches []match
		}
		if err := json.Unmarshal(st, &res); err != nil || len(res.Matches) == 0 {
			return "", fmt.Errorf("search %q: no matches", q.Query)
		}
		m := choose(res.Matches)
		o.got = m.Section
		if m.SectionBytes > largeSection {
			text, _, err := c.call("read_lines", map[string]any{"doc": m.Doc, "offset": max(m.Line-3, 1),
				"limit": aroundLines, "version": m.Version})
			return text, err
		}
		text, _, err := c.call("read_section", map[string]any{"doc": m.Doc, "section": m.Section})
		return text, err
	}
	args := map[string]any{"doc": q.Doc, "maxLevel": 2}
	var id string
	for i, title := range q.Path {
		_, st, err := c.call("outline", args)
		if err != nil {
			return "", err
		}
		var res struct{ Headings []heading }
		if err := json.Unmarshal(st, &res); err != nil {
			return "", err
		}
		id = ""
		for _, h := range res.Headings {
			if strings.EqualFold(h.Title, title) {
				id = h.ID
				args = map[string]any{"doc": q.Doc, "section": h.ID, "maxLevel": h.Level + 1}
				break
			}
		}
		if id == "" {
			return "", fmt.Errorf("no heading %q in the outline (step %d)", title, i+1)
		}
		if i == len(q.Path)-1 {
			break
		}
	}
	o.got = id
	text, _, err := c.call("read_section", map[string]any{"doc": q.Doc, "section": id})
	return text, err
}

// budget is what an answer may cost: a third of reading its document
// whole, but at least 8 KiB (one moderate read: in a small document a
// section is a large share) and never more than one read and a half.
func budget(o outcome) int {
	return int(min(max(o.baseline/3, 8<<10), 48<<10))
}

func TestCost(t *testing.T) {
	s := newServer(t)
	var rows []string
	var total, whole int64
	failed := 0
	for _, q := range questions {
		o := run(t, s, q)
		var calls []string
		for _, st := range o.steps {
			calls = append(calls, fmt.Sprintf("%s %s", st.tool, size(int64(st.bytes))))
		}
		status := "ok"
		if !o.ok {
			status = "MISSED: " + o.why
		} else if o.cost > budget(o) {
			status = fmt.Sprintf("OVER BUDGET (%s)", size(int64(budget(o))))
		}
		if status != "ok" {
			failed++
			t.Errorf("%s: %s", q.Ask, status)
		}
		total += int64(o.cost)
		whole += o.baseline
		rows = append(rows, fmt.Sprintf("| %s | %s | %s | %s | %.0f%% | %s |", q.Ask, strings.Join(calls, " → "),
			size(int64(o.cost)), size(o.baseline), 100*float64(o.cost)/float64(max(o.baseline, 1)), status))
	}
	report := "| Question | Calls | Cost | Whole document | Share | |\n|---|---|---|---|---|---|\n" +
		strings.Join(rows, "\n") + fmt.Sprintf("\n| **%d questions** | | **%s** | **%s** | **%.0f%%** | %d failed |\n",
		len(questions), size(total), size(whole), 100*float64(total)/float64(whole), failed)
	t.Log("\n" + report)
	if p := os.Getenv("EVAL_REPORT"); p != "" {
		f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = f.Close() }()
		fmt.Fprintf(f, "## What answers cost (mcp-docs eval)\n\n%s\n", report)
	}
}

func size(n int64) string {
	if n >= 1<<10 {
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
