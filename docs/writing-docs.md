---
title: Writing Markdown for agents
description: How to write and structure Markdown documentation so that agents find the part they need and read only that, through mcp-docs or any tool that reads by section.
tags: [authoring, guide]
---

# Writing Markdown for agents

An agent pays for every byte it reads, in tokens and in attention. A
person scrolls past what they do not need; an agent that reads a 180 KB
file to answer one question has paid for all of it. mcp-docs lets an
agent read one section, the lines around a search match, or an outline,
instead of whole files. How much that saves depends on how the
documents are written: on the mcp-gateway documentation, the questions
of the cost evaluation (`make eval`) are answered for 5% of what reading
their documents would cost, and the questions that cost most are those
where the documents are hardest to cut.

This guide says how to write documents that cut well. Most of it also
makes them better for people.

## What an agent sees

mcp-docs reads a document as a tree of sections:

- A **heading** opens a section: `#` to `######` at the start of a line
  (ATX headings). The section runs to the next heading of the same or a
  higher level, so a `##` section holds its `###` subsections.
- Each section has an **id**, the anchor GitHub gives its heading
  (`## 6.6 Policy lifecycle` is `#66-policy-lifecycle`), a line number,
  and a size in lines and bytes.
- `outline` lists the headings with ids and sizes; `search` gives each
  match with the section it is in; `read_section` returns one section,
  and a section larger than one call (32 KiB) in parts.
- The document's **title** and **description** come from its front
  matter, or else from its first `#` heading and its first paragraph;
  `list_docs` shows them for every document.

What the scanner does not see as headings: lines in fenced code blocks
(` ``` ` or `~~~`), indented code, `#tag` without a space, and setext
headings (a line underlined with `===` or `---`), since a line of dashes
is as often a rule.

So the unit an agent reads is the section, and the outline is the map
it reads first. Both work when sections are the right size and their
headings say what is in them.

## Structure

### One title, then chapters

Give each document one `#` heading, its title, then `##` for its main
parts and `###` for topics within them. Do not skip levels (`#` then
`###`): the outline then shows a topic without the part it belongs to.

Keep the text between the `#` title and the first `##` short: a
paragraph that says what the document is for and what it covers. That
text belongs to the title's section, which is the whole document. When
a search matches there, the section of the match is everything; an
agent that reads it reads the file. Put the substance under `##`
headings, and a term that is explained further down only in the
section that explains it.

### Sections of the right size

| Section size | What an agent does |
|---|---|
| up to 8 KiB (about 2,000 tokens) | reads the section whole: the cheap case |
| 8 to 32 KiB | reads the lines around a match, or the section's introduction and outline (`depth: 0`), then a subsection |
| over 32 KiB | gets it in parts, with the outline of the rest after each |

Aim for sections of a few hundred bytes to a few KiB, each answering
one question. When a section grows past 8 KiB, split it with `###`
headings rather than letting it grow: a subsection can be read alone,
a paragraph in the middle of a long section cannot.

Very small sections are not a problem in themselves, but a section of
one line under its own heading usually belongs with its neighbours.

### Items that should be read alone get headings

Lists of numbered or bold-titled items (roadmap steps, decisions,
changelog entries, FAQ entries, error messages with long explanations)
are often one section to the outline: mcp-gateway's roadmap is a list
of 30 steps under one `## 11. Roadmap`, 46 KiB that no outline can
split. An agent looking for one step searches for it and reads the
lines around the match, which works but costs a guess at how many lines
the step has.

If readers look items up one at a time, give each its heading:

```markdown
## 11. Roadmap

### 27. Documentation that costs fewer tokens

- the problem: …
```

A changelog does this already with a heading per release. Keep each
release's notes under its own `## v1.2.0 — 2026-10-08` heading, newest
first, so that an agent reads the release it was asked about.

### Close fences, and only use `#` for headings

A code fence that is not closed hides every heading after it from the
outline. Close each fence with the same character (backticks or tildes)
at least as many times as it was opened. Lines in fences are never
headings, so shell comments and Markdown examples are safe inside them.

Outside code, a line starting with `#` and a space is a heading. Do not
start a paragraph line with `#` unless it is one.

### Front matter, if you have metadata

A YAML block at the very start of the file gives the title and
description `list_docs` shows, and tags:

```markdown
---
title: Upgrading from 1.x
description: What changes when you upgrade a 1.x installation to 2.0, and what to do before and after.
tags: [upgrade, migration]
---
```

Without front matter, the first `#` heading is the title and the first
paragraph is the description, cut to 200 characters. Then make that
paragraph a summary: what the document is about and for whom. Badges,
tables and images before it are skipped, but a paragraph of history or
acknowledgements becomes the description.

## Headings

### Say what the section answers

The outline is all an agent sees of a document before it reads. A
heading that names the question or the task lets it choose without
reading:

| Instead of | Write |
|---|---|
| Miscellaneous | Proxies and certificates |
| Details | How approvals expire |
| Notes | Known limits of Landlock on 6.12 kernels |
| Advanced | Running two gateways on one host |

Use the words people search for. If users say "timeout" and the
section is about `approval_timeout`, the heading can say both.

### Keep ids unique and stable

Ids are built from heading titles: lowercase, punctuation removed,
spaces turned into dashes. Two headings with the same title get `#setup`
and `#setup-1`, which tells the agent nothing; give them distinct
titles ("Setup on Tumbleweed", "Setup on SLES 16").

Changing a heading's title changes its id, and links and agents'
memories of it break. For a section other documents link to, give it
an explicit id that survives rewording:

```markdown
## Self-check of the installation {#self-check}
```

Section numbers in titles are fine: `## 6.6 Policy lifecycle` is
`#66-policy-lifecycle`, and a heading path such as
`Policy model > Policy lifecycle` finds it without the numbers.

## Sections an agent can read alone

An agent reads one section, not the pages before it. Write each so that
it stands alone:

- Name the subject in the section, not only in the heading above it:
  "The approval page lists…" rather than "It lists…".
- Avoid "as described above" and "see the previous section". Link to
  the section instead, with its anchor: `[approval scopes](07-approvals.md#scopes-and-grants)`.
  An agent can follow such a link as it is: `read_section` takes
  `user-guide/07-approvals.md#scopes-and-grants`.
- Define a term where it is used, or link to its definition.
- Keep the conditions with the instruction: "On SLES 16, enable the
  repository first:" rather than a condition three paragraphs earlier.

## Writing for search

Most precise questions start with a search for a term. Help it find
the right place first:

- **Write exact strings exactly.** Configuration keys, command-line
  options, file paths and error messages in code spans, as the program
  has them: `agents.no_request_timeout`, `--max-read`,
  `outside the allowed directories`. Agents search for what they saw.
- **Explain each thing in one place.** When a key is explained in one
  section and mentioned in five others, the search has six matches and
  the agent has to guess. Mention it elsewhere with a link to the
  explanation. (The cost evaluation's agent picks the section with the
  most matches; a section that explains a key usually names it more
  than once.)
- **Collect error messages.** A section or table that maps each message,
  verbatim, to its cause and fix is the cheapest answer to "what does
  this error mean?". Keep long explanations in their own sections and
  link to them from the table.
- **Use one name for one thing.** A server called "gateway-docs" in one
  chapter and "the documentation server" in another is two searches.

## The index document

A collection's index (`README.md` or `index.md` at its root, or the
`index` its collection file names) is what agents read first when they
do not know which document answers. Make it a map, not an
introduction:

- a table of documents and what each is for;
- a table of common questions and the `document#section` that answers
  each;
- where error messages are explained;
- what is *not* in the documentation and where to find it instead (the
  configuration on the machine, another server, another package).

Keep it under a few KiB: agents read it whole. mcp-gateway's
`docs/README.md` is an example.

## Tables, code and images

- **Tables** are compact for reference data. Keep cells short; a table
  with paragraphs in its cells is better written as sections.
- **Code blocks** cost what they hold. Show the minimal example; put a
  complete configuration file in its own section or document.
- **Images** are not read by agents through mcp-docs. Say in the text
  what a diagram shows; a sentence such as "requests pass the gateway,
  then OPA, then the server" is what the agent needs.
- **HTML** in Markdown is read as text. Avoid it for structure;
  `<details>` blocks and HTML tables hide headings and cost markup.

## Shipping documentation

For documentation installed on a system and served by mcp-docs:

- Install it where it stays installed: files marked `%doc` in an RPM
  are left out on systems installed without documentation (rpm
  `%_excludedocs`, common in containers). mcp-gateway installs its
  documentation below `/usr/share/mcp-gateway/docs` for that reason.
- Ship a collection file in `/usr/share/mcp-docs/collections.d/`
  (packaging/suse/README.md shows how), with a description that says
  what the documentation covers and its `instructions` for what agents
  should know before reading: which document to start with, which ones
  never to read whole, and where the answers are that the documentation
  does not have.
- Document the installed version. An agent on a machine asks about the
  software there; say which version a document describes when it
  matters, and keep upgrade notes in the changelog.

## Checking a document

See your document as an agent does: its outline, with sizes.

```sh
{ echo '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"outline","arguments":{"doc":"docs/guide.md"}}}'
  sleep 1; } | mcp-docs --root docs
```

(MCP over stdin: the server ends when its input does, without answering
what is still running, hence the pause. It prints JSON; the outline is
the `text` of its `content`.)

Then look for:

- sections over 8 KiB that readers would look up in parts: split them;
- long text under the `#` title before the first `##`: move it down;
- headings that do not say what the section answers;
- duplicate titles (ids ending in `-1`);
- a heading you expected and do not see: an unclosed fence above it, a
  setext heading, or a missing space after `#`.

For a set of documents that matters, write down the questions readers
ask and the section that answers each, and measure what answering them
costs, as `internal/eval` does for mcp-gateway's documentation: the
calls an agent makes, the bytes they return, and whether they reach
the right section. Run it again when the documentation changes.

## Checklist

- [ ] One `#` title per document, then `##` and `###`, no skipped levels.
- [ ] A short introduction under the title; the substance under `##`.
- [ ] Sections answer one question each, mostly under 8 KiB.
- [ ] Items read one at a time (steps, decisions, releases) have headings.
- [ ] Fences closed; ATX headings only.
- [ ] Headings say what the section answers, in the readers' words; no duplicate titles.
- [ ] Explicit `{#id}` on sections other documents link to.
- [ ] Each section readable alone; links with anchors instead of "above".
- [ ] Keys, options, paths and error messages written exactly, explained in one place.
- [ ] A title and a summary first paragraph, or front matter.
- [ ] An index document that maps questions to sections.
- [ ] Diagrams described in text.
- [ ] Installed outside `%doc`, with a collection file.
