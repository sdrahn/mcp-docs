# mcp-docs architecture

`mcp-docs` is an MCP server that presents documentation installed on a
system (Markdown files below `/usr/share/doc`, `/usr/share/<app>/docs`,
a project's `docs/` tree, …) to an agent, so that the agent answers a
question by reading **the part of a document it needs**, not the whole
file.

It generalises what the `gateway-docs` server of
[mcp-gateway](https://github.com/sdrahn/mcp-gateway) does for the
gateway's own documentation (that project's `docs/architecture.md`,
roadmap steps 27 and 28) into a server for any set of Markdown
documents.

Status: steps 1 to 5 and 7 of the plan (section 11) are implemented;
steps 6 and 8 are design. Figures in the examples are from mcp-gateway's docs; elided ones
are marked `…`.

## 1. The problem

An agent that reads documentation through a plain file server pays for
whole files. In mcp-gateway, before steps 27 and 28, one answer cost the
index (about 2k tokens) and a whole chapter (6k to 12k), or
`architecture.md` (about 41k), or the changelog (about 23k), most of it
beside the question. Documentation is written for people who scroll;
an agent pays for every line it is given.

Two kinds of question need two ways in:

- a **precise** question has a term to look for (a setting, an error
  message, a decision number such as "D17"): search for it, read the
  lines around the match;
- a **broad** question ("how do approvals work?") has no such term,
  matches in many places or nowhere: read the document's outline (its
  headings and their sizes), then the one section that answers it.

## 2. What mcp-gateway already proved

The `gateway-docs` server is `mcp-server-fs --read-only --root
/usr/share/mcp-gateway/docs --instructions "…"`. What it does, and what
`mcp-docs` keeps:

| mcp-gateway (`mcp-server-fs`) | Kept in mcp-docs as |
|---|---|
| `search_text`: lines that contain a text or match an RE2 expression, case-insensitive by default, with file, line number and context lines; capped by `maxResults` and bytes, `truncated` when there is more | `search` (§5.3), plus the enclosing section of each match |
| `outline_file`: ATX headings with line number, lines and bytes of each section (to the next heading of the same or a higher level); fenced code is not headings; setext headings not recognised; `maxLevel`; streamed, so its size is not bounded by the read limit | `outline` (§5.2), plus a stable section id |
| `read_text_file` with `offset` and `limit`; the result says `[lines a-b of n]` | `read_lines` (§5.5), and `read_section` (§5.4) by id |
| An index file (`README.md`) that names the file for each question and where each error message is explained | the collection's `index` (§4.2), shown in the instructions |
| Server instructions that teach the workflow: search first, read around the match; outline for a broad question; never read the big files whole | generated instructions (§6) |
| Text output for the model, `structuredContent` with an `outputSchema` for programs | text only by default; `structuredContent` and output schemas with `--structured` (§15) |
| Read-only, `os.Root` per directory (no symbolic link out), Landlock read-only on the roots, no network, a dynamic user, its own SELinux domain (`mcpsrv_docs_t`) | §8 |
| Outlines computed when asked, not generated into the docs at build time: always current, no build step | kept, with a cache (§7) |
| Tests that measure cost: the outline is under 1/20 of the file, a section read is under 1/20, a search for 5 matches under 4 KB | the token-cost evaluation (§10) |

What a file server cannot do, and why a server for documents is worth
having:

- **Addresses that survive edits.** A line number from an outline is
  only right until the file changes; a package update can change it
  mid-session. Headings have anchors (`04-mcp-servers.md#landlock`),
  which the index already uses as links.
- **A search result that says where it is.** `search_text` gives a line
  number; the agent still needs an outline to know which section to
  read. A docs server knows the section of every line.
- **A read limit sized for a context window.** `mcp-server-fs` reads up
  to 10 MiB per call, right for a file server, so nothing stops an
  agent from reading `architecture.md` whole. A docs server's limit is
  a budget for the agent (default 16 KiB, §15), and a section larger than it
  comes in parts, with its sub-outline.
- **More than one set of documents**, each with its own description and
  index, found without the agent knowing paths.
- **Ranked search** for broad questions where neither a literal term
  nor the headings help (§5.6, later).

## 3. Overview

```
              agent (Claude Code, Kit, …)
                     │ MCP (stdio, Streamable HTTP §13; or through mcp-gateway)
              ┌──────▼───────────────────────────────┐
              │ mcp-docs                              │
              │                                       │
              │  tools ── resources ── instructions   │
              │     │                                 │
              │  ┌──▼────────┐   ┌─────────────────┐  │
              │  │ catalog   │──▶│ document cache  │  │
              │  │ (collec-  │   │ (outline, ids,  │  │
              │  │  tions)   │   │  line offsets)  │  │
              │  └──┬────────┘   └───────┬─────────┘  │
              │     │       ┌────────────▼─────────┐  │
              │     │       │ markdown scanner     │  │
              │     │       └────────────┬─────────┘  │
              │  ┌──▼────────────────────▼─────────┐  │
              │  │ os.Root per collection (r/o)    │  │
              │  └─────────────────────────────────┘  │
              └──────────────┬────────────────────────┘
                 Landlock: read the roots only; no network
                             │
          /usr/share/doc/…   /usr/share/foo/docs   ~/project/docs
```

Components (one Go module, no dependencies beyond the standard library
and `golang.org/x/sys` for Landlock):

| Package | Does |
|---|---|
| `cmd/mcp-docs` | flags, configuration, sandbox, serve on stdio or HTTP |
| `internal/markdown` | the scanner: headings, fences, front matter, anchors, section extents, in one streamed pass |
| `internal/catalog` | collections, their documents, the cache keyed by file identity |
| `internal/search` | literal and RE2 line search; later the ranked section index |
| `internal/server` | MCP: tools, resources, instructions, limits |
| `internal/mcpserver` | JSON-RPC over stdio, ported from mcp-gateway's (no dependencies, already in use there) |
| `internal/mcphttp` | the same handler over Streamable HTTP, stateless (§13) |
| `internal/landlock` | ported from mcp-gateway |

Ported rather than imported: mcp-gateway's packages are `internal/`.
If both projects keep them, a later step can move the shared ones
(scanner, mcpserver, landlock) to a small module both import.

## 4. Documents and collections

### 4.1 Documents

A document is a regular file with a Markdown extension (`.md`,
`.markdown`; configurable) below a collection's root. Its **address**
is `<collection>/<path relative to the root>`, e.g.
`mcp-gateway/user-guide/04-mcp-servers.md`. With one collection the
prefix may be left out. An address may carry a section:
`…/04-mcp-servers.md#landlock`, the form links in an index already
have, so an agent can follow an index link as it is.

Files that are not text (NUL bytes, as `isBinary` in mcp-gateway) are
not documents. Symbolic links are followed only when they stay inside
the root (`os.Root` refuses the others).

### 4.2 Collections

A collection is a named directory of documents with a description:

```yaml
# /usr/share/mcp-docs/collections.d/mcp-gateway.yaml
name: mcp-gateway
title: mcp-gateway documentation
root: /usr/share/mcp-gateway/docs
description: >
  The MCP gateway this server runs behind, for the installed version:
  user guide (13 chapters), architecture and decisions, changelog.
index: README.md          # read first: which file answers which question
include: ["**/*.md"]      # default
exclude: []               # glob patterns
also: ["/usr/share/doc/packages/mcp-gateway/CHANGELOG.md"]  # single files elsewhere
list_items: true          # entries under one heading as sections (§5.8)
instructions: >
  The machine's configuration is not here: the gateway-admin server
  shows it (show_config) and checks it (check_config, doctor).
```

Where collections come from, later ones replacing earlier ones of the
same name (the `/usr` then `/etc` order of mcp-gateway's `servers.d`):

1. `/usr/share/mcp-docs/collections.d/*.yaml`: shipped by the packages
   whose documentation they describe. A package that wants its docs
   readable by agents installs one small file.
2. `/etc/mcp-docs/collections.d/*.yaml`: the administrator's; an empty
   file of a shipped name disables that collection.
3. `--collection name=/path[:description]` on the command line, and
   `--root /path` (one unnamed collection; what `mcp-server-fs --root`
   does today). With either, the directories above are not read unless
   `--config-dirs` is given.

A collection without `index` gets one generated: its documents with
their titles and descriptions (§4.3), at most `--max-entries`. (As
built, the generated index is `list_docs` of the collection, which the
instructions name for a collection without an index document.)

`also` documents are opened by the absolute paths the file gives, not
through the collection's `os.Root` (which the Landlock rule for a single
file would not let the server open); an agent names them by their base
names only, and one hides a file of the same name at the collection's
root. The disabling empty file works by file name: an empty
`/etc/…/gateway.yaml` disables what `/usr/…/gateway.yaml` defined,
whatever `name` it gave.

### 4.3 Document metadata

From the document itself, in this order:

- **title**: front matter `title`, else the first level-1 heading, else
  the file name;
- **description**: front matter `description`, else the first paragraph
  after the title, cut to 200 characters;
- **size**: lines, bytes, and an estimate of tokens (bytes / 4; said to
  be an estimate), so the agent can judge before reading.

Front matter is a YAML block between `---` lines at the very start of
the file. Only `title`, `description` and `tags` are read; the rest is
ignored. Its lines count in line numbers but are no section's content.

## 5. Tools

All tools are read-only, idempotent, closed-world
(`readOnlyHint`, `idempotentHint`, `openWorldHint: false`). Each returns
a text for the model; with `--structured`, also `structuredContent`
matching its `outputSchema` (§15). Each refuses unknown arguments, as
mcp-gateway's do. Every output is bounded by `--max-read` (default
16 KiB) and says when it was cut and how to get the rest.

### 5.1 `list_docs`

`list_docs(collection?, path?, query?, offset?, limit?)` — the
collections (without arguments), or the documents of one collection or
below a path: address, title, size; the index document first, and the
count per directory. A page at a time (`limit`, default 25);
descriptions on a list that fits one page or is filtered by `query`
(all its words in the path, title, description or tags). See §15.

```
mcp-gateway: mcp-gateway documentation — 15 documents, 600 KB (~150k tokens)
  README.md                         Start here (index)            95 lines  10.4 KB
  user-guide/04-mcp-servers.md      4. MCP servers              1203 lines  59.6 KB
  architecture.md                   Architecture                3125 lines 182.6 KB
  …
```

### 5.2 `outline`

`outline(doc, maxLevel?, section?)` — the headings of a document, each
with its **id**, level, line, and the lines, bytes and ~tokens of its
section (to the next heading of the same or a higher level). `section`
gives only that section's subtree; `maxLevel` leaves out deeper
headings (their lines stay in their parent's extent). The result
carries the document's `version` (§7).

```
mcp-gateway/architecture.md: 3125 lines, 182.6 KB (~46k tokens), version 9f3a…
   1  # Architecture                     #architecture          [3125 lines, 182.6 KB]
  28  ## 1. Problem statement            #1-problem-statement   [ …]
1662  ### 6.6 Policy lifecycle          #66-policy-lifecycle   [ …]
 …
(read a section: read_section with doc and its id)
```

Heading recognition is mcp-gateway's `outline_file`, unchanged:
ATX headings `#` to `######` with up to 3 spaces of indent; closing
`#`s removed; empty headings ignored; nothing inside a fenced code block
(backticks or tildes, closed by a fence of the same character at least
as long) is a heading; setext headings are not recognised (a line of
dashes is as often a rule). Its tests are ported with it.

**Section ids** are the anchors GitHub and most renderers give
headings, so that links in the documents work as addresses: the title
lowercased, characters other than letters, digits, spaces, `-` and `_`
removed, spaces turned to `-`; a repeated id gets `-1`, `-2`, … in
document order. An explicit id wins: `## Title {#id}` or an
`<a id="…">`/`<a name="…">` on the heading's line.

### 5.3 `search`

`search(query, collection?, doc?, regexp?, caseSensitive?, context?,
maxResults?)` — mcp-gateway's `search_text` (same arguments, defaults,
RE2, caps, `truncated`, binary files skipped), over documents rather
than paths, and with **each match placed in its section**: the
section's id, its heading path, and its size. Matches are grouped by
section, so ten hits in one section read as one place.

```
mcp-gateway/user-guide/03-configuration.md
  § Agents › Timeouts  #timeouts  [38 lines, 1.9 KB]
   212- agents:
   213:   no_request_timeout: 30s
   214-   # how long an agent may go without asking anything
(read the section: read_section; or the lines: read_lines)
```

The agent goes from a match straight to `read_section`, without an
outline call between.

### 5.4 `read_section`

`read_section(doc, section, depth?, part?)` — the text of one section,
from its heading to its end. `section` is an id from `outline` or
`search`, or a heading path (`"Operations › Self-check"`), or given in
`doc` after `#`.

- `depth`: how many levels of subsections to include whole (default:
  all). With `depth: 0` the section's own text up to its first
  subsection is returned, followed by the outline of its subsections,
  so a long chapter is read as an introduction and a table of contents.
- A section larger than `--max-read` is never refused: the result holds
  the part that fits, cut at a line (at a subsection boundary when one
  is close), then the outline of the rest and `part: 2` to continue.
  The agent learns the shape of a large section on the first call,
  which a refusal with "too large" does not teach.
- The result names what it holds: `[§ #66-policy-lifecycle, lines
  1662-1709 of 3125, part 1 of 1]`.

### 5.5 `read_lines`

`read_lines(doc, offset, limit, version?)` — lines by number, as
mcp-gateway's `read_text_file` with `offset` and `limit`; `head` and
`tail` too. For the lines around a search match, and for documents
without headings. With `version` (from `outline` or `search`) a
document changed since then is an error that says so, rather than the
wrong lines.

### 5.6 `find_sections`

`find_sections(query, collection?, read?, maxResults?)` — sections
ranked for a question in words, for the broad question where search
finds too much or nothing and the right document is not obvious
(`internal/rank`). By default (`read: true`, 3 results) it returns the
best section's text, up to 4 KiB (a larger one as its introduction and
the outline of its subsections), then the next sections as `doc#id`
lines with the words around their best-matching line: one call answers
(§15). With `read: false` (5 results) it returns the list only, and the
agent chooses what to read.

- **What is ranked:** each section's own text, from its heading to the
  next heading of any level, so that a chapter does not outrank its
  subsections by holding their text; the text before a document's
  first heading is a unit too, with the document's title.
- **Score:** BM25 (k1 1.2, b 0.75) over the own text and the titles of
  the headings above it, plus a bonus for each query word in the
  section's own title (twice the word's idf). The bonus is outside
  BM25's length normalisation: a section titled "Instances do not
  start" ranks above a short one that says "instance" five times, which
  folding the title into the text did not achieve.
- **Words:** lowercased, split at anything but letters, digits and `_`;
  an identifier with `_` counts whole and in its parts
  (`no_request_timeout`, `request`, `timeout`); a short list of English
  stopwords; a light stemmer (plurals, `-ing`, `-ed`, a final `e`), so
  that "instances" meets "instance" and "configuring" "configure".
- **Index:** per collection, built when first asked and again when a
  document changes, is added or goes (checked with the cache's versions
  on each call); documents larger than `--max-file` are left out.
  Across collections, results are merged by score.
- **Limits:** it matches words, not meanings: "how long does an
  approval last" does not find a section about approval scopes that
  never says "last". Headings in the readers' words are what makes it
  work (docs/writing-docs.md).

On the evaluation corpus, the 7 questions in words of §10 find their
section first. Probing with 13 natural questions put the answering
section first for 11 and second for one ("servers that speak HTTP with
OAuth sign-in": a related decision first); it missed one whose words the
documents do not use ("how long does an approval last").

### 5.7 Compatibility names

With `--fs-compat`, the server also offers mcp-gateway's names with
their arguments: `outline_file`, `search_text`, `read_text_file`,
`search_files`, `list_directory`. The `gateway-docs` server and its
instructions can then move to `mcp-docs` without a change to roles or
to agents' habits, and move to the new names later.

### 5.8 List items as sections

Documents often keep entries as list items under one heading: steps of
a roadmap, decisions, FAQ entries. mcp-gateway's roadmap is 30 steps in
46 KiB under `## 11. Roadmap`; no outline could split it, and an agent
read 40 lines around a match and hoped the step ended there. With
`list_items: true` in a collection file (or `--list-items` for the
collections of the command line), the scanner takes as sections, one
level below the heading they are under:

- top-level list items that open with a bold title, numbered or not:
  `30. **Landlock as a second wall** (0.18, D19; …)` is
  `#30-landlock-as-a-second-wall`;
- paragraphs that open with a bold title after a blank line:
  `**D19 — Landlock as a second wall around instances.**`.

Not items: a bold label ending in a colon (`**Note:**`), nested list
items, anything in code, and items of one line (a list of short bold
terms is no list of entries; a one-line entry is read with its
neighbours). Items get their ids after the headings, so that an item
never changes a heading's id (its anchor, as links have it). The
outline marks them `(item)`; search, `find_sections` and `read_section`
treat them as sections; `outline_file` of `--fs-compat` keeps to
headings, as mcp-server-fs's does.

It is an option because it guesses: a document that uses bold-led
paragraphs for emphasis gets sections it did not mean.

## 6. Instructions

The server's `instructions` are generated, not a fixed text:

1. the collections: name, title, description, the index document, and
   each collection's own `instructions`;
2. the workflow, as mcp-gateway's `gateway-docs` instructions teach it:
   - start with the index of the collection when you do not know the
     document;
   - for a precise term, `search`, then `read_section` of the match (or
     `read_lines` around it);
   - for a broad question, `outline` of the document (`maxLevel: 2`
     for a large one), then `read_section`;
   - read a document whole only when the question needs all of it, and
     never one larger than the read limit;
3. the limits of one call.

A test checks that the instructions name only tools the server offers
(as mcp-gateway's does), so that `--fs-compat` and later tools cannot
leave them stale.

## 7. Cache and versions

Scanning a Markdown file is one streamed pass; the result (headings,
ids, extents, byte offset of every 256th line, metadata) is small,
a few percent of the file. It is kept per document, keyed by
`(device, inode, size, mtime)`, and checked with one `stat` per call:
a package update is seen on the next call, without inotify. The key's
hash is the document's `version`.

`read_section` and `read_lines` seek to the nearest stored line offset
rather than reading from the start. Search reads files (bounded by
`--max-read` per file, as mcp-gateway's); for large collections an
in-memory copy of documents up to `--cache-bytes` (default 32 MiB)
avoids reading them on every search.

The catalog (which documents a collection has) is listed at start and
again when a call finds a document missing or after
`--rescan-interval` (default 60 s).

## 8. Security

The server only reads, and only its roots:

- read-only by construction: no tool writes; the process opens files
  `O_RDONLY` through one `os.Root` per root, so `..` and symbolic links
  out of a root are refused;
- **Landlock**: after reading its configuration, the process restricts
  itself to reading its roots (and the `also` files), as
  `mcp-server-fs` does; a kernel without Landlock is logged, not fatal;
- no network on stdio: it opens no socket, and Landlock (ABI 4) allows
  it no TCP connection and no listening; under mcp-gateway,
  `network: false`. Over HTTP (§13) it listens on its one port, which
  Landlock allows it to bind, and still connects nowhere;
- runs as anyone: under mcp-gateway, `run_as: dynamic`; under systemd,
  `DynamicUser=yes`, `ProtectHome=yes` unless a root is in a home;
- SELinux: the `mcpsrv_docs_t` domain of mcp-gateway reads `usr_t` and
  `usr_share_t`; a collection in a home directory needs the domain to
  read `user_home_t`, which is a separate, opt-in type
  (`mcpsrv_docs_home_t`);
- bounded: every output is capped (§5); a scan or search ends when the
  client cancels the request;
- documents are data: their text goes to the model as tool results.
  The server does not interpret instructions in them; the one place a
  collection's own words reach the instructions is the `description`
  and `instructions` of its configuration file, which is
  administrator- or package-owned (`/usr`, `/etc`), never read from
  the documents.

## 9. Resources and prompts

For clients that browse resources rather than call tools:

- each document is a resource `docs://<collection>/<path>`, `text/markdown`,
  with its title and description; listing is paged;
- a resource template `docs://{collection}/{path}#{section}` reads one
  section (the same as `read_section`, within the read limit);
- a prompt `answer_from_docs(question, collection?)` that states the
  workflow of §6 for one question.

Tools stay the main way in: a resource read cannot carry `depth`,
`part` or a version check.

## 10. Testing and the token-cost evaluation

- unit tests of the scanner, ported from mcp-gateway's
  `outline_test.go` and `search_test.go` (fences of both kinds, a fence
  inside one of the other kind, `#tag` and indented code not headings,
  `maxLevel`, no headings, binary files, links out of the root), and
  new ones for ids (duplicates, explicit ids, punctuation, non-ASCII),
  front matter, section parts and the version check;
- a test that the instructions name only offered tools;
- **an evaluation of cost**: a set of questions over a fixed corpus
  (mcp-gateway's docs to start), each with the section that answers it.
  For each, the tool calls the instructions lead to are run and their
  output bytes counted; the test fails when an answer costs more than
  its budget (e.g. 1/20 of the document, as mcp-gateway's end-to-end
  tests require) or misses the section. It is the regression test for
  every change to tools, defaults or instructions;
- under mcp-gateway, its end-to-end tests `TestDocsLookup` and
  `TestDocsOutline` run against `mcp-docs --fs-compat` unchanged.

## 11. Plan

1. **Core** (done): the scanner with ids, front matter skipped and
   line offsets, ported with mcp-gateway's tests; collections from
   `--root` and `--collection name=/path[:description]`; `outline`,
   `search` with sections (context kept to the match's section),
   `read_section` with heading paths (section numbers ignored),
   `depth` and parts, `read_lines` with versions; the cache; generated
   instructions; Landlock; `--fs-compat`, checked against the
   assertions of mcp-gateway's `TestDocsLookup` and `TestDocsOutline`
   on its documentation. Taken from step 2 early: a basic `list_docs`
   (titles from the first heading, sizes), since without it an agent
   has no way to find documents but the index. Left for later: the
   in-memory copy for search (`--cache-bytes`): documents are read
   from disk on each search, which is fast enough for documentation of
   a few MB; the catalog is walked on each listing or search, so no
   `--rescan-interval` is needed.
2. **Catalog** (done): `collections.d` in `/usr` and `/etc` (and
   `--config-dirs`), with title, index, include and exclude patterns,
   `also` documents and instructions; front matter (`title`,
   `description`, `tags`) and first-paragraph descriptions in
   `list_docs`; the instructions per collection; resources with paging
   and a section template; the prompt `answer_from_docs`.
3. **Packaging and adoption** (done): an RPM (`packaging/suse`) for
   openSUSE Tumbleweed, openSUSE Leap 16 and SLES 16, built, rpmlinted,
   installed and smoke-tested in Tumbleweed and Leap 16 containers in
   CI (SLES 16 as an OBS target: Leap 16 is built from its sources), a
   release workflow and `_service` as mcp-gateway's; the
   `collections.d` convention documented for other packages; the cost
   evaluation (`internal/eval`, §10) on a snapshot of mcp-gateway's
   documentation, in CI with its table as the job summary; a guide to
   writing documentation for agents (`docs/writing-docs.md`). In
   mcp-gateway: its documentation as a collection, `/usr/bin/mcp-docs`
   labelled as the entry point of `mcpsrv_docs_t`, the doctor's checks,
   and a `gateway-docs` definition on `mcp-docs --fs-compat` that the
   administrator enables. Deviation: the definition is not the default
   yet; making it so needs mcp-docs in the distributions the gateway
   is built for, and then a `Requires` of `mcp-gateway-fs-server`.
   Moving the gateway's instructions to the new tool names follows.
4. **Ranked search** (done): `find_sections` (§5.6), with 7 questions
   in words in the evaluation.
5. **Long list items as sections** (done): `list_items` (§5.8). In the
   evaluation, the decision question costs 1.4 KiB instead of 2.7 (the
   decision whole instead of 40 lines around the match); the roadmap
   question 7.1 KiB instead of 3.1, for the whole step instead of a
   window that cut it short.
6. **Other formats**: the scanner behind an interface (headings with
   extents, ids, metadata), so that AsciiDoc, reStructuredText or man
   pages can be collections too. Markdown only until there is a need.
7. **HTTP and containers** (done): `--listen`, stateless Streamable
   HTTP (§13); a container image (`packaging/container`), built and
   queried in CI.
8. **SUSE documentation** (§14; SLES 16.0 converted, §14.7): a
   converter, `mcp-docs-convert`, from SUSE's DocBook sources to
   Markdown collections, so that the documentation of
   documentation.suse.com is served offline, a section at a time. All 48
   SLES 16.0 articles convert (`tools/suse-docs/convert`), every source
   link checked against the site, and are packaged
   (`suse-docs-mcp-sles-16.0`, packaging/suse-docs); next, the container
   image, SLES for SAP and SLE HA 16.0, SLE 15 guides after.
9. **Token cost** (§15; phases 1 to 3 done, §15.3 to §15.5, measured in §15.6): a result
   once and compact (`--structured` opt-in), `list_docs` paged, large
   outlines at two levels, `--max-read` 16 KiB; `find_sections` answers
   in one call; shorter tool descriptions and `--tools`; the eval
   measuring both ways clients forward results; `rank: false`, the
   converter's compact index and its report of large sections.

## 12. Decisions

- **D1. A server for documents, not a file server with more tools.**
  Addresses, sections and limits are about documents; a file server
  has to stay general (and writable). `--fs-compat` keeps the way back.
- **D2. Outlines computed on demand, cached by file identity**, not
  generated at build time (mcp-gateway step 28's reason: always
  current, any Markdown file, no build step).
- **D3. Sections addressed by anchor id**, line numbers kept for ranges:
  ids survive edits and match the links documents already contain.
- **D4. A read limit for a context window (16 KiB since §15), with parts**, not
  a file-size limit with refusal: a large section is still readable,
  and the first part teaches its shape.
- **D5. Search answers with sections.** The step from a match to the
  text to read is the server's, not one more call.
- **D6. Collections declared by files in `/usr` and `/etc`**, not
  discovered by walking `/usr/share/doc`: a package says its docs are
  for agents and describes them; nothing is exposed by accident.
- **D7. No embeddings, no network.** Literal, RE2 and BM25 search run
  anywhere, explain themselves, and need no model or index build step.
  The cost is recall for questions in words the documents do not use;
  headings in the readers' words make up for most of it.
- **D8. Go, standard library, ported packages.** The same language and
  packaging as mcp-gateway, whose code the core comes from.
- **D9. Rank a section by its own text, its title as a bonus.** A
  chapter that holds its subsections' text would outrank them; a title
  folded into the text loses to length normalisation.
- **D10. List items as sections by choice, per collection.** The rule
  guesses at authors' intent; a collection says when it fits.
- **D11. Stateless HTTP.** No sessions, no event streams: the tools
  need no state between calls and send nothing unasked, so every
  request stands alone, any number of instances serve one endpoint, and
  a restart loses nothing. Authentication and TLS belong to what is in
  front of it.
- **D12. SUSE documentation converted from its sources, ahead of time.**
  Not fetched per call (network in a server that has none, a round trip
  per read, no index) and not scraped from the published HTML (its
  layout is not an interface): the DocBook sources carry the structure,
  the ids and the metadata, and the conversion runs once per release.
- **D13. Results for a model by default.** A result is its text, once;
  `structuredContent` is opt-in (`--structured`), because clients
  differ in what they forward and one forwards both (§15). Nothing
  large comes unasked: listings page, large outlines start at two
  levels.

## 13. HTTP and containers

For an agent on the same machine, stdio is the transport: the agent
starts the server, and no port, address or authentication is involved.
That holds for a container too (`podman run -i --rm image` as the
command). For one server that many agents reach over a network (the
documentation of a product, in a container on a cluster), mcp-docs
speaks MCP's Streamable HTTP with `--listen ADDR`:

- **One endpoint**, `/mcp`. A client POSTs one JSON-RPC message per
  request (`Content-Type: application/json`); a request gets its
  response as JSON (`200`), a notification or response `202` with no
  body. `GET` and `DELETE` are `405`: there is no stream for the server
  to send on and no session to end. Batches (removed in 2025-06-18) are
  refused.
- **Stateless** (D11): `initialize` is answered without
  `Mcp-Session-Id`; every later request stands alone. A cancellation
  is the client closing its request, which cancels the handler's
  context; `notifications/cancelled` has nothing to do.
- **Checks the specification asks for:** an `Origin` header (from a
  browser) must be one of `--allow-origin` (none by default; clients
  that are not browsers send none and are served), against DNS
  rebinding; `MCP-Protocol-Version` must be a version spoken (none means
  2025-03-26).
- **Bounds:** a body of 1 MiB, `--max-concurrent` requests at once (32;
  more wait while their client does), 60 s per request, and the HTTP
  server's header, read, write and idle timeouts.
- **`/healthz`** answers `200` for health checks and load balancers.
- **Stopping:** SIGTERM or SIGINT stops accepting and lets requests in
  progress finish (up to 30 s), as a container runtime stops it.
- **Not its job:** TLS and authentication, which an ingress or reverse
  proxy in front of it does (mTLS, OAuth, tokens). The documents are
  read-only and often public; MCP's own authorization flow is worth
  building only for a server exposed on its own.
- **Landlock:** binding only the port of `--listen`, connecting nowhere
  (a port the kernel chooses, `:0`, cannot be named: binding is then
  not restricted).

### The container image

`packaging/container/Containerfile` builds a static binary in SUSE
BCI's Go image and runs it on BCI micro (or `scratch`): `ENTRYPOINT
mcp-docs --listen :8080`, user 65532, nothing written, so the root file
system can be read-only. It serves mcp-docs' own documentation as the
collection `mcp-docs`; an image for a product's documentation builds on
it and adds its documents and collection file (the converted SUSE
documentation, for one), so that the image's tag names the
documentation's version and updating it is a rebuild.

## 14. SUSE documentation (step 8)

documentation.suse.com publishes SUSE's product documentation as HTML
and PDF, built from DocBook sources on GitHub. An agent on a SUSE
system asks about that documentation most of all. Step 8 makes it a
collection: converted ahead of time from the sources into Markdown that
mcp-docs serves like any other (D12), so that all its tools work on it
and the server stays offline and read-only.

### 14.1 The sources

| Products | Repository | Shape |
|---|---|---|
| SLE 16 family, SUSE Linux Micro | `SUSE/doc-modular`: `main` is the version in development (16.1 at the time of writing), `maintenance/SLE16.0` the released 16.0 | "Smart Docs": about 1,400 DocBook 5.2 files; task, concept, reference and glue topics (`tasks/`, `concepts/`, `references/`, `glues/`) assembled into about 108 articles (`articles/*.asm.xml`) |
| SLE 15, SLE 12, SLE Micro, openSUSE Leap | `SUSE/doc-sle`, `main` and `maintenance/SLE15SPx` | DocBook guides (`xml/`, about 470 files), one book per guide |

Both are under the GNU Free Documentation License 1.2 or later, with an
invariant section (the copyright notice): converted copies may be
distributed with the license text and that notice.

What the converter reads in doc-modular:

- **Build definitions**, `DC-*`: `MAIN` (the assembly), `SRC_DIR`, and
  the profiling values (`PROFOS="sles"`) that select a product's text
  from shared sources.
- **Assemblies**: `<resources>` maps resource ids to topic files;
  `<structure renderas="article" xml:id="…">` is the article, whose
  `<merge>` gives its title and `<meta>` fields (`title`,
  `description`, `productname`, `architecture`, `task`, `series`); its
  nested `<module resourceref="…" renderas="section">` give the topics'
  order and nesting, a `<merge>` in a module overriding a title.
- **Topics**: `<topic xml:id="…">` with `<info>` (title, meta) and
  `<section xml:id="…">`; a few KB each (3.3 KB for "Changing the
  SELinux mode").
- **Entities**: `common/generic-entities.ent`,
  `product-entities.ent`, `network-entities.ent` (`&selnx;`,
  `&prompt.root;`, product names).

### 14.2 The converter

`mcp-docs-convert` (in this repository, Go, `encoding/xml`): source
tree and DC file in, Markdown documents and a collection file out.

1. **Entities** resolved from the `.ent` files the sources declare;
   an unresolved one is an error, not text left as `&x;`.
2. **Profiling**: elements whose `os`, `arch` and other profiling
   attributes do not match the DC's values are left out.
3. **Structure** from the assembly: the article's modules in order, a
   module's nesting as heading levels, merged titles applied.
4. **Elements**: titles to ATX headings; `para` to paragraphs;
   `screen` and `programlisting` to fenced code (with the language when
   given); `itemizedlist`, `orderedlist`, `procedure`/`step` to lists;
   `variablelist` to bold terms with their text; simple tables to pipe
   tables, others to lists of rows; `note`, `tip`, `important`,
   `warning` to quoted paragraphs that open with their kind;
   `command`, `option`, `filename`, `literal` to code spans,
   `replaceable` to `<NAME>` in them; images to their alt text or
   caption only. An element it does not know is reported, so that none
   is dropped silently.
5. **Ids**: every `xml:id` of a topic or section becomes an explicit
   anchor (`## Changing the SELinux mode {#selinux-switching-modes}`),
   so that mcp-docs' section ids are SUSE's ids; `xref` and `link`
   become links to `document#id`, or to documentation.suse.com when the
   target is outside the collection.
6. **Front matter** per document: `title`, `description` (the
   article's or topic's meta), `tags` (the topic type, the article's
   `task` values), and `source`, the page on documentation.suse.com.

### 14.3 Documents and collections

- **One document per topic**, named by its id
  (`selinux-switching-modes.md`), so that a topic that several articles
  reuse is found once by `search` and `find_sections`;
- **one index document per article** (`articles/selinux.md`): its title,
  description, and its topics in order as links, so that an agent who
  wants the article reads it in order;
- **one collection per product version** (`sles-16.0`), its index a
  generated list of the articles with their descriptions, and
  instructions that name the product version and the license.

The open choice is the other one, one document per article: closer to
the site's pages, duplicate matches for reused topics. Topics first;
the evaluation (§14.5) decides if that is wrong.

### 14.4 Delivery

- **A package per product version**, built on OBS:
  `suse-docs-mcp-sles-16.0` ([packaging/suse-docs](../packaging/suse-docs/README.md),
  built, linted, installed and served in CI); the converter runs at build time on a
  pinned source revision; the package installs the documents below
  `/usr/share/mcp-docs/docs/suse/sles-16.0`, the collection file, and
  the license. Offline, air-gapped, updated with maintenance updates.
  It does not require mcp-docs (without it, the files do nothing). The
  converter comes from mcp-docs' spec as its own package,
  `mcp-docs-convert`, the documentation package's build requirement,
  with `tools/suse-docs` (`SRC=` an unpacked tarball of the sources:
  OBS builds have no network).
- **A container image** on the image of §13, with the same documents:
  its tag names the product and documentation version.
- **Later, a sync timer** (`mcp-docs-sync`) for systems that want the
  current documentation between releases: it fetches the sources (the
  network is the timer's, never the server's), converts the product and
  version of `/etc/os-release` into `/var/lib/mcp-docs`, and the server
  reads collection files from `/var/lib/mcp-docs/collections.d` too.

What mcp-docs itself needs: the `source` front matter shown by
`outline`, `list_docs` and `find_sections`, so that agents cite the
official page; the third directory of collection files for the sync.

### 14.5 Testing

- **Golden tests**: a few topics and an assembly from doc-modular as
  fixtures (with the license), converted and compared with the expected
  Markdown.
- **Checks on a whole conversion**: every `xml:id` an anchor, no
  unresolved entity, no unknown element, every internal link resolving.
- **Evaluation questions** on the converted SLES 16 documentation, as
  §10 has for mcp-gateway's: "how do I switch SELinux to permissive
  until the next boot?", with the section that answers each.
- **The site**: every `source` checked against documentation.suse.com,
  its page fetched and its anchor found (§14.7).

### 14.6 The prototype

`cmd/mcp-docs-convert` and `internal/docbook` convert one article (a
build definition) of doc-modular:

```sh
mcp-docs-convert --src doc-modular --dc DC-SLES-SELinux --out /usr/share/mcp-docs/docs/suse/sles-16.1 \
    --name sles-16.1 --title "SUSE Linux Enterprise Server 16.1" --revision c66ed55
```

It writes `topics/ID.md` per topic, `articles/ID.md` for the article and
`README.md` for the collection, and reports what it could not convert
(`--strict` makes that an error) and the sections larger than agents
read whole (`--max-section`, §15.5). On "Understanding SELinux Basics"
(`internal/docbook/testdata`, doc-modular `main` at c66ed55, under the
GFDL): 26 topics and the legal notices, no unknown element, no
unresolved entity, no broken link; SLE Micro's and SAP's text left out
by `PROFOS="sles"`. Served by mcp-docs, seven questions in words find
the answering section first ("make a domain permissive" →
`#setting-permissive-domain`), and `read_section` takes SUSE's ids.

What it showed:

- `main` documents the version in development: `&productnumber;` is
  16.1. Released documentation comes from the maintenance branches.
- Ids on elements that are not headings (an example, a variable list)
  are link targets too: a link to one goes to the section that holds
  it, under the element's own title (`[Common options](#sec-semanage-command)`).
- An assembly's resources can include topics its structure does not
  use; only those in the structure are converted.
- Entities hold markup that profiling must see (`&productname;` is a
  phrase per product), so they are expanded before parsing.
- A fix in mcp-docs itself: `find_sections` ranked front matter as the
  text before a document's first heading; front matter is metadata now,
  not ranked.

Not yet: `source` points to the source file on GitHub until the site's
page names are checked; topics' titles overridden by an article are
shown in the article's index only; tables, figures and other formats of
doc-sle are written but untested on real sources.

### 14.7 SLES 16.0

The site names an article's page by its build definition: `DC-NAME`
is `https://documentation.suse.com/PRODUCT/VERSION/html/NAME/index.html`
(`/sles/16.0/html/SLES-SELinux/index.html`), and a PDF beside it
(`/sles/16.0/pdf/NAME_en.pdf`). An article is one page, except for its
appendices and glossaries, which are pages of their own named by their
id (`doc-gfdl-license.html`, `vt-glossary.html`). The anchors are the
sources' `xml:id`s. Which product and version an article is published
under is the site's configuration, not the sources': the converter is
told (`--site-url https://documentation.suse.com/sles/16.0/html/%s/`)
and the list of articles is the site's (`tools/suse-docs/sles-16.0`,
from its `sitemap.xml`: 48 build definitions in `maintenance/SLE16.0`,
of its 167). The site's pages carry `cache-control: max-age=300` and no
`Last-Modified` or `ETag`, only a `dcterms.modified` meta.

`tools/suse-docs/convert` clones doc-modular, checks out a pinned
revision of `maintenance/SLE16.0` (a9f7084) and converts the 48
articles into one collection with its collection file:

```sh
tools/suse-docs/convert build/sles-16.0 /usr/share/mcp-docs/docs/suse/sles-16.0
```

The result: 48 articles, 386 topics, 435 documents, 2.8 MB, in ten
seconds; no unknown element, unresolved entity, broken link or
conflict. Every one of the 434 `source` URLs is a page of the site with
the anchor in it; every section heading of the site's pages is an
anchor of the collection, except one the branch has removed since the
site was built (the site lags the branch) and the article's groups
below. Served, 14 of 16 questions across the articles find the
answering section first (`TestSLES16`, run with `MCP_DOCS_SLES16` set);
in the other two the article's index comes first and the answer next.

What the whole product needed beyond the prototype:

- **Several articles, one collection**: `--dc` more than once or
  `--dcs FILE`; an article's index is `articles/NAME.md`, by its build
  definition, because build definitions share assemblies (the Agama
  installation article is one assembly for x86-64, POWER and IBM Z).
- **Topic variants**: a topic that articles profile differently (an
  architecture's steps) is one document per variant,
  `topics/ID--ARTICLE.md`, named by the first article that has it; one
  they profile alike is one document. A topic profiled alike whose links
  lead to different variants is split by its rendered text, until no
  document differs between articles. Two different topics with one id
  (`task-example` in two articles) come out as variants too.
- **`STRUCTID`**: an assembly can hold several articles; the build
  definition names the one it builds.
- **Groups**: a module without a topic is a heading of its own in the
  article (a title, an abstract, an id over its topics); it is an item
  of the article's index, and a link to its id goes to the index.
- **XInclude**: snippets (`snippets/*.xml`, with their own entities)
  are included before profiling.
- **Callouts**: a listing's `co` marks are numbered `(1)`, `(2)` in the
  code, the `calloutlist` after it renders as `(1) …` paragraphs.
- **Glossaries**, simple lists and a procedure's result.
- **Profiling values as given**: the converter matches values
  exactly. Some build definitions select `PROFARCH="x86-64"` where most
  sources mark `arch="x86_64"`; in SLES 16.0 the one article that does
  (the PXE client) has no text for either, so it does not matter yet.
  Whether to treat them as one is the build tool's call (DAPS), to
  check before an article needs it.

## 15. Token cost

The server finds the section that answers in one call; what an answer
costs a client is mostly what comes around it. This section is the plan
to lower that cost for any client, without a RAG pipeline (D7 stands),
and records phase 1.

### 15.1 Where a client spends tokens

1. **Once per session:** the server's instructions and the tool
   definitions, sent with every turn. Clients that cache prompts (Kit
   and Claude Code do, with Anthropic models) pay a cache read after the
   first turn, others the full price. Behind mcp-gateway every server's
   tools add up.
2. **Per call:** the result. Clients differ in what they pass to the
   model: the text, the `structuredContent`, or both. Kit
   (mark3labs/kit) JSON-encodes the whole result into one string, so a
   model got mcp-docs' text and the same data again as JSON, the
   Markdown escaped.
3. **Per turn:** every earlier result stays in the conversation and is
   sent again. A large result early in a session costs again on every
   later turn; every extra call is another turn.

So: fewer calls per answer, one compact copy of each result, a small
fixed surface, nothing large unasked.

### 15.2 The plan

| Item | What | Phase |
|---|---|---|
| 1 | **One compact copy.** `structuredContent` and output schemas opt-in (`--structured`); `find_sections` one line per hit (`doc#id`, heading path, size) and the words around the match (about 160 bytes) instead of the whole line | 1 |
| 2 | **One call per answer.** `find_sections` returns the best section's text (bounded) with two alternatives as one-liners; 3 results by default | 2 |
| 3 | **Nothing large by default.** `list_docs` a page at a time with filters; large outlines start at two levels; `--max-read` 16 KiB | 1 |
| 4 | **A small fixed surface.** Shorter tool descriptions with the guidance once in the instructions; `--tools LIST` to offer fewer, the instructions naming only those; client and gateway filters documented (Kit's `allowedTools`, gateway roles) | 2 |
| 5 | **Documents shaped for sections.** A compact collection index (article titles grouped by task); legal topics out of the ranking; the converter reports sections over a size; the writing guide's section sizes | 3 (sizes: 1) |
| 6 | **Measured.** `make eval` reports the cost as text and as JSON-forwarding clients receive it, with budgets for the fixed cost per session | 1 |
| 7 | **Upstream, optional.** Propose to Kit that it forward a result's text content, unescaped, when there is one: every MCP server would gain. Not ours to change; an issue for the owner to post | — |

The footer of a result ("read one: read_section doc#id") stays,
shortened: it costs about 15 tokens and helps clients that do not pass
the server's instructions to the model.

### 15.3 Phase 1, as built

- **`--structured`** (default off): with it, tools have output schemas
  and results carry `structuredContent`, as before; without it a result
  is its text, once. The eval and the converter's tests run with it, to
  steer by the data.
- **`find_sections`** prints `N. doc#id  Heading > path  (size)` and
  the matching line cut to about 160 bytes around the first word of
  the question.
- **`list_docs`** takes `query`, `offset` and `limit` (default 25, at
  most 200); its header counts the documents per directory
  (`articles/ 48, topics/ 386`); descriptions only on a list that fits
  a page or is filtered; the last line says how to get the next page.
  Without arguments and with one collection, or with a query, it lists
  the (first) collection's documents.
- **`outline`** without `maxLevel`, on more than 40 headings in what it
  shows, shows the two upper levels and says so.
- **`--max-read`** defaults to 16 KiB (was 32): a stray whole-document
  read costs half; `read_section` continues in parts.
- **Instructions** lead with `find_sections` → `read_section` for a
  question in words, and name `list_docs` for browsing, a page at a
  time.
- **`make eval`** reports per question the text, the result as JSON
  (default) and as JSON with `--structured`; `TestSessionCost` holds the
  instructions to 2 KiB and the tool definitions to 7 KiB (10 KiB with
  `--structured`).

Measured. The eval's 20 questions over mcp-gateway's documentation, all
answered from the right section:

| | Before | Phase 1 |
|---|---:|---:|
| Text the agent reads | 71.3 KiB | 70.2 KiB |
| As a JSON-forwarding client receives it | 103.1 KiB | 74.2 KiB |
| Tool definitions | 8.9 KiB | 6.9 KiB (9.4 KiB with `--structured`) |

A session on the SLES 16.0 collection (435 documents), each call's
result as a JSON-forwarding client receives it:

| Call | Before | Phase 1 |
|---|---:|---:|
| `list_docs` | 74,850 B | 2,899 B |
| `find_sections` | 4,607 B | 1,613 B |
| `search` | 3,513 B | 1,556 B |
| `outline` (a large topic) | 6,180 B | 2,727 B |
| `read_section` | 1,364 B | 1,364 B |
| tool definitions | 9,646 B | 7,349 B |
| **the session (initialize, tools, 8 calls)** | **115.6 KB, ~29k tokens** | **32.3 KB, ~8k tokens** |

The collection's index (`README.md`, 9.8 KB) is the largest result
left: item 5, phase 3.

### 15.4 Phase 2, as built

- **`find_sections` answers.** `read` (default true) puts the best
  section's text, up to 4 KiB, after its line; a larger section comes as
  its introduction and the outline of its subsections (`read_section`
  with `depth: 0`). The other sections follow under "Also:" as `doc#id`
  lines with their matching words. Without `maxResults` it returns 3
  sections when reading, 5 with `read: false` (the list, as in phase 1).
  With `--structured`, `read` holds the document, section and text.
- **Shorter tool descriptions.** One or two sentences each, argument
  descriptions of a few words, no title repeated in the annotations:
  how to work belongs in the instructions, sent once.
- **`--tools LIST`** (default: all) offers only the named tools, among
  them the `--fs-compat` ones; an unknown name is an error at start. The
  instructions name only the tools offered, so an agent is never told to
  use one it does not have. The plan's fixed sets (`core`, `full`) became
  a list: removing `read_lines` or `list_docs` by default would leave
  collections without an index, and documents without headings, without
  a way in, and a list lets each deployment choose.
- **Instructions** say that `find_sections` returns the best section's
  text.
- **Eval:** a question in words is one call when the best section is
  within 4 KiB; `TestSessionCost` holds the tool definitions to 5 KiB
  (8 KiB with `--structured`, 2.5 KiB with `--tools
  find_sections,read_section,outline`).

Measured. The eval's 20 questions, all still answered from the right
section; the seven in words in one call each:

| | Before | Phase 1 | Phase 2 |
|---|---:|---:|---:|
| Text the agent reads | 71.3 KiB | 70.2 KiB | 67.2 KiB |
| As a JSON-forwarding client receives it | 103.1 KiB | 74.2 KiB | 70.6 KiB |
| Calls | 45 | 45 | 38 |
| Instructions + tool definitions | 10.0 KiB | 8.0 KiB | 6.0 KiB (3.1 KiB with the smallest `--tools`) |

On the SLES 16.0 collection, as a JSON-forwarding client receives it:

| | Before | Phase 1 | Phase 2 |
|---|---:|---:|---:|
| "make a SELinux domain permissive" | 5,971 B, 2 calls | 2,977 B, 2 calls | 2,174 B, 1 call |
| Instructions + tool definitions | 11,378 B | 9,067 B | 7,053 B (3,959 B with the smallest `--tools`) |

### 15.5 Phase 3, as built

- **`rank: false`** in a document's front matter keeps it out of
  `find_sections`' ranking: licenses, legal notices, changelogs, text
  that matches many questions in words and answers none. It is still
  listed, searched (`search`) and read, and the index stays current
  across it. A front matter field, not a pattern in the collection file:
  it travels with the document, and a converter, which knows what a
  document is, sets it.
- **The converter's collection index** lists the articles' titles under
  their first task ("Administration", "Security", from the sources'
  `meta name="task"`; "Other" without one), without their descriptions,
  which stay in each article's front matter for `list_docs`. Read whole,
  it is the way into the collection.
- **The converter's legal topics** (the sources' `common/`: the GFDL and
  the legal notice) carry `rank: false`.
- **`--max-section N`** (default 8 KiB, the writing guide's limit for a
  section read whole) reports the sections whose own text, to the next
  heading, is larger, the legal topics aside. They are the sources'
  authors' to split; the report is a note, not a loss, so `--strict`
  does not fail on it.

Measured on the SLES 16.0 collection (still 14 of the 16 `TestSLES16`
questions answered first):

| | Phase 2 | Phase 3 |
|---|---:|---:|
| The collection's index, read whole (as a JSON-forwarding client receives it) | 9,802 B | 5,392 B |
| Sections over 8 KiB of their own text | — | 4, of which `vm-host-network-default` 17.0 KiB |

### 15.6 Before and after, measured

The three phases together, on SLES 16.0: mcp-docs and mcp-docs-convert
before phase 1 (8fde1d6, the collection converted with that converter)
against the release with phase 3 (mcp-docs 0.1.0 and
`suse-docs-mcp-sles-16.0` as packaged), both from doc-modular a9f7084.
One session: `initialize` and `tools/list`; orientation (`list_docs`,
then the collection's index read whole); the 16 `TestSLES16` questions,
each the way the version's instructions teach: before, `find_sections`
then `read_section` of the first section; now, `find_sections` alone,
and where the best section is over 4 KiB a second call that reads it
whole (an agent that reads only the subsection it needs spends less).
Results as a JSON-forwarding client (Kit) receives them; tokens
estimated at 4 bytes each.

| | Before | Now | |
|---|---:|---:|---:|
| Instructions and tool definitions | 11,152 B | 6,824 B | −39% |
| Orientation (`list_docs`, the index) | 84,652 B | 8,291 B | −90% |
| 16 questions | 123,350 B, 32 calls | 63,845 B, 19 calls | −48% |
| A question, on average | 7,709 B | 3,990 B | −48% |
| The session's input, every turn re-sending the fixed part and the results before it | 5.48 MB, ~1.37M tokens | 1.02 MB, ~256k tokens | −81% |
| Questions answered from the right section first | 14 of 16 | 14 of 16 | |

Thirteen questions take one call instead of two and cost 55 to 75%
less ("open a port in a firewalld zone": 4,482 B → 1,472 B; "make a
SELinux domain permissive": 5,971 B → 2,174 B). Three have a best
section over 4 KiB and take two calls still: "configure GRUB 2 for PXE
boot" 27.8 KB → 18.5 KB, "systemd timer does not run" 10.2 KB →
7.4 KB, "create a snapshot of a VM with virsh" 8.6 KB → 7.9 KB.

For a client that passes the model only the text, the session's input
falls from ~820k to ~245k tokens (−70%): the copy as JSON never reached
it. With prompt caching, the re-sent part of each turn is a cache read
and costs less; the proportion stays.

### 15.7 Compatibility

A client that reads `structuredContent` needs `--structured` from this
release on (CHANGELOG). The text of every tool is unchanged in what it
says; `find_sections` and `list_docs` changed their layout, which
agents read, not parse. `find_sections` returns text by default; an
agent that wants the list alone passes `read: false`.

