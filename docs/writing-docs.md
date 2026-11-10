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
  match with the section it is in; `find_sections` ranks sections for a
  question in words, a section's own title counting most, and returns
  the best one's text with the list; `read_section` returns one
  section, and a section larger than one call (16 KiB by default) in
  parts.
- The document's **title** and **description** come from its front
  matter, or else from its first `#` heading and its first paragraph;
  `list_docs` shows them for every document. Other front matter (a product, a version, dates) is in the
  file but not in what the tools show: what an agent must know to
  answer, such as the version a page is for, belongs in the text too.

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
| up to 4 KiB (about 1,000 tokens) | gets it whole with the `find_sections` that finds it: one call, the cheapest case |
| 4 to 8 KiB | `find_sections` gives its best-matching subsection, or a passage of at most 2 KiB around the matching words, and lists the other parts; the agent reads the section whole in a second call if it needs to |
| 8 to 16 KiB | reads the lines around a match, or the section's introduction and outline (`depth: 0`), then a subsection |
| over 16 KiB (the default `--max-read`) | gets it in parts, with the outline of the rest after each |

Aim for sections of a few hundred bytes to a few KiB, each answering
one question: under 4 KiB, the question is answered in the call that
finds the section. When a section grows past 8 KiB, split it with `###`
headings rather than letting it grow: a subsection can be read alone,
a paragraph in the middle of a long section cannot. Between 4 and
8 KiB, subsections whose headings name what they answer are what
`find_sections` can choose from; without them it can only cut a
passage.

### A task split into steps needs an overview

Splitting has a limit: the question has to be answered somewhere. When
a long procedure is split into a section per step, no step answers
"how do I do the whole thing", and a search for it finds one step or
none first. SLES for SAP 16.1's manual installation is a dozen step
topics (booting, product selection, registration, storage, …): "install
SLES for SAP with Agama" is answered by none of them alone. Give such a
task a short section of its own, under the task's name, that lists the
steps in order with a sentence and a link each: it answers the question
whole, and each link leads to a step's detail.

Very small sections are not a problem in themselves, but a section of
one line under its own heading usually belongs with its neighbours.

### Items that should be read alone get headings

Lists of numbered or bold-titled items (roadmap steps, decisions,
changelog entries, FAQ entries, error messages with long explanations)
are often one section to the outline: mcp-gateway's roadmap is a list
of 30 steps under one `## 11. Roadmap`, 46 KiB that an outline of its
headings cannot split. An agent looking for one step searches for it
and reads the lines around the match, which works but costs a guess at
how many lines the step has.

If readers look items up one at a time, give each its heading:

```markdown
## 11. Roadmap

### 27. Documentation that costs fewer tokens

- the problem: …
```

A changelog does this already with a heading per release. Keep each
release's notes under its own `## v1.2.0 — 2026-10-08` heading, newest
first, so that an agent reads the release it was asked about.

Where the documents cannot change (another project's, or a format
everyone knows), the collection can take such items as sections:
`list_items: true` in its collection file. Then a top-level list item or
a paragraph that opens with a bold title over more than one line is a
section one level below its heading (`30. **Landlock as a second wall**
…` is `#30-landlock-as-a-second-wall`). Write such entries with the
title in bold at the start, and a bold label that ends in a colon
(`**Note:**`) for anything that is not an entry.

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

`rank: false` keeps a document out of the ranking of `find_sections`
(it is still listed, searched and read): for a license, a legal notice
or a changelog, text that shares many words with questions and answers
none of them, and for navigation, such as an index or a page that only
lists other pages' titles, which shares the words of every question it
links to and should not come before the page that answers.

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
`find_sections` matches words, not meanings, and a word in a section's
title counts most: "How approvals expire" ranks first for "how long
does an approval last" only if the section also says "last", or the
reader asks how approvals "expire". A sentence near the top of the section that names the
question in other words ("how long an approval lasts") helps as well.

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
  `user-guide/07-approvals.md#scopes-and-grants`. Link to the Markdown
  document, relative to the linking one, not to a published `.html`
  page or a file in `images/`: the agent can read only what is in the
  collection.
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
  The same goes for spellings: "roll back" and "rollback", "set up" and
  "setup" are different words to `find_sections`. Use one, and where
  readers use both (the noun and the verb, a command and the task),
  write the other once near the top of the section.
- **Write names with ordinary spaces.** Agents type spaces. A name
  written with no-break spaces (`SLES&nbsp;for&nbsp;SAP`, as typesetting
  does to keep it on one line) is another string to tools that match
  text. mcp-docs' `search` matches no-break spaces as spaces (from
  0.2.0); other tools may not, and `find_sections` ranks words either
  way.
- **Keep one copy of each text.** Two copies of a text, such as a
  glossary in every guide or a shared topic included in several, are
  two matches of the same weight: the agent reads both or guesses
  (`find_sections` folds sections whose text is the same to the byte;
  `search`, and copies that differ in a word, show each).
  Keep one copy and link to it. Where versions differ, keep each
  version in its own collection or directory, and let the agent choose
  it (`doc` scopes `search` and `find_sections` to a directory) rather
  than mixing versions in one text.

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
`docs/README.md` is an example. For a large collection, list titles
grouped by task, one line each, rather than a table with a
description per document: SLES 16.0's index of 48 articles went from
9.6 KB to 5.2 KB that way, and the descriptions are still in
`list_docs`. Give the index `rank: false` (see [front
matter](#front-matter-if-you-have-metadata)).

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
- Say which system the documentation is of, where it is of one: in
  the front matter of the collection's index,
  `system: {"os_release": {"ID": "sles", "VERSION_ID": "16.1"}}`
  (fields of the system's `/etc/os-release`; `"file"` for a file only
  such a system has). A server on that system ranks the collection
  before those of other versions and products, and tells the agent
  which it is.
- Ship the collection's index file. Without one, every session's first
  `find_sections` reads and indexes all documents (0.4 s for 1,650 of
  them); with `mcp-docs.index` in the collection's directory it reads
  the file (36 ms). Write it as the last step of the package's build,
  after the last change to a document: `mcp-docs --write-index` with
  the collection's own settings. It is used only while every document
  is as it was then, so a document patched afterwards costs the gain,
  not the answer: the server's log says when a file is not used.

## Collections that are served together

An agent that does not know where the answer is asks all collections at
once, and they are ranked as one. What that asks of documentation that
comes in several collections (a product family, versions, a product
and its add-ons):

- **Keep shared text the same to the byte.** `find_sections` returns a
  section that several collections hold once, and names the others
  ("also in sles-sap-16.1"), when its id and its text are the same. A
  shared topic that differs in one word, a product name filled in for
  each collection, is a result of its own in each: of three results,
  two are then the same topic. Write shared topics without the product
  name where the sentence allows it, and say which product a document
  is for in its front matter, which is not compared, and in the
  collection's index.
- **Give what differs a title that says so.** Where two collections
  answer the same question differently, both sections come back, and
  the agent chooses by their titles and paths: "Installing on IBM Z",
  not a second "Installing".
- **Test the questions against all of them together.** A question a
  collection answers first when asked alone can come second among its
  neighbours: SUSE's High Availability documentation answers "install
  a three-node cluster" first alone, and not beside the collections of
  the products it runs on. The collection's `instructions` can say
  what it is the place for ("cluster questions: collection sle-ha"),
  so that the agent names it.

## Checking a document

See your document as an agent does: its outline, with sizes. mcp-docs
answers in JSON; `jq` prints the outline's text.

```sh
{ echo '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"outline","arguments":{"doc":"docs/guide.md"}}}'
  sleep 1; } | mcp-docs --root docs 2>/dev/null | jq -r '.result.content[0].text'
```

(MCP over stdin: the server ends when its input does, without answering
what is still running, hence the pause.)

For a guide to a backup tool with the usual problems, the outline is:

```text
docs/guide.md: 238 lines, 14.9 KiB (~3.8k tokens), version caefcbf3812c
  1  # Backup tool  #backup-tool  [238 lines, 14.9 KiB]
 28  ## Installing  #installing  [14 lines, 288 B]
 36  ### Example  #example  [6 lines, 218 B]
 42  ## Configuring  #configuring  [11 lines, 349 B]
 46  ### Example  #example-1  [7 lines, 286 B]
 53  ## Notes  #notes  [9 lines, 419 B]
 62  ## Reference  #reference  [163 lines, 11.8 KiB]
225  ## Restoring  #restoring  [14 lines, 435 B]
(read a section: read_section with doc and its id)
```

Each column: the line a section starts on, its heading, its id, and its
size to the next heading of the same or a higher level. What this one
shows:

- **A long introduction.** The title is on line 1 and the first `##` on
  line 28: 26 lines that belong to the title's section, the whole
  document. A search that matches there gives the agent all 14.9 KiB.
  Keep a short paragraph under the title and move the rest under `##`
  headings.
- **Duplicate titles.** Two `### Example` sections: the second is
  `#example-1`, and neither says what it shows. Name them ("Example: a
  nightly backup", "Example: excluding caches").
- **A heading that does not say what the section answers.** `## Notes`
  tells the agent nothing before it reads. Say what the notes are
  about, or move each into the section it belongs to.
- **A section too large to read whole.** `## Reference` is 11.8 KiB
  without subsections, over the 8 KiB an agent reads at once. Split it
  with `###` headings (one per group of options), or a table per group.
- **A heading missing.** The guide has a `## Troubleshooting` section
  that the outline does not list, and `## Restoring` runs to the end of
  the file. The code fence under "Restoring" is never closed, so
  everything after it is code. Close the fence. Other causes of a
  missing heading: a setext heading (underlined with `---`) or no space
  after `#`.

For a set of documents that matters, write down the questions readers
ask and the section that answers each, and measure what answering them
costs, as `internal/eval` does for mcp-gateway's documentation: the
calls an agent makes, the bytes they return, and whether they reach
the right section. Run it again when the documentation changes, and
with the other collections it is served with (see [collections that
are served together](#collections-that-are-served-together)).

### Sources in another format

Documentation written in DocBook or another format and converted to
Markdown (as SUSE's is, by `mcp-docs-convert`) is checked as converted:
the outline above is of the Markdown. What the conversion cannot keep
is better not written:

- **Table cells that span rows or columns.** A Markdown table has
  none: the cell is in its first place and the others it covers are
  empty. A table that reads only with its spans is better two tables,
  or a list.
- **Images without a description.** An icon in a sentence ("click the
  … icon") is its description in the Markdown, and nothing without
  one. Give every image a text alternative that says what it shows.
- **Ids that are not names.** A topic's id is its document's file name
  and its section's anchor: letters, digits, `-`, `_` and `.`, stable
  across versions, so that links and agents' memories of them hold.

## Checklist

- [ ] One `#` title per document, then `##` and `###`, no skipped levels.
- [ ] A short introduction under the title; the substance under `##`.
- [ ] Sections answer one question each, mostly under 4 KiB, none over 8 KiB without subsections.
- [ ] A task split into steps has an overview section that answers it whole.
- [ ] Items read one at a time (steps, decisions, releases) have headings.
- [ ] Fences closed; ATX headings only.
- [ ] Headings say what the section answers, in the readers' words; no duplicate titles.
- [ ] Entries kept as list items open with a bold title (or get headings).
- [ ] Explicit `{#id}` on sections other documents link to.
- [ ] Each section readable alone; links with anchors instead of "above".
- [ ] Keys, options, paths and error messages written exactly, explained in one place.
- [ ] One copy of each text; versions in their own collections or directories.
- [ ] One spelling per term; names with ordinary spaces.
- [ ] Links to Markdown documents and anchors, not to `.html` pages.
- [ ] A title and a summary first paragraph, or front matter.
- [ ] Product, version and dates in the text, not only in front matter.
- [ ] `rank: false` on legal notices, changelogs and navigation pages.
- [ ] A compact index document that maps questions to sections.
- [ ] Diagrams described in text.
- [ ] Installed outside `%doc`, with a collection file and the index file, written last.
- [ ] Topics shared between collections the same to the byte; the questions tested with the collections together.
