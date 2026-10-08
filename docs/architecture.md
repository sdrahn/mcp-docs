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

Status: steps 1 and 2 of the plan (section 11) are implemented; the
rest is design. Figures in the examples are from mcp-gateway's docs; elided ones
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
| Text output for the model, `structuredContent` with an `outputSchema` for programs | the same, for every tool |
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
  a budget for the agent (default 32 KiB), and a section larger than it
  comes in parts, with its sub-outline.
- **More than one set of documents**, each with its own description and
  index, found without the agent knowing paths.
- **Ranked search** for broad questions where neither a literal term
  nor the headings help (§5.6, later).

## 3. Overview

```
              agent (Claude Code, Kit, …)
                     │ MCP (stdio; or through mcp-gateway)
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
| `cmd/mcp-docs` | flags, configuration, sandbox, serve on stdio |
| `internal/markdown` | the scanner: headings, fences, front matter, anchors, section extents, in one streamed pass |
| `internal/catalog` | collections, their documents, the cache keyed by file identity |
| `internal/search` | literal and RE2 line search; later the ranked section index |
| `internal/server` | MCP: tools, resources, instructions, limits |
| `internal/mcpserver` | JSON-RPC over stdio, ported from mcp-gateway's (no dependencies, already in use there) |
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
a text for the model and `structuredContent` matching its
`outputSchema`. Each refuses unknown arguments, as mcp-gateway's do.
Every output is bounded by `--max-read` (default 32 KiB) and says when
it was cut and how to get the rest.

### 5.1 `list_docs`

`list_docs(collection?, path?)` — the collections (without arguments),
or the documents of one collection or below a path: address, title,
description, lines, bytes, ~tokens; the index document first.

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

### 5.6 `find_sections` (later)

`find_sections(query, collection?, maxResults?)` — sections ranked for a
question in words, for the broad question where search finds too much
or nothing and the right document is not obvious. BM25 over sections
(heading path weighted above body), built when a collection is first
searched and kept with the cache; no embeddings, no network, no model.
Returns ids, heading paths, sizes and the best-matching line of each,
not text: the agent still chooses what to read.

### 5.7 Compatibility names

With `--fs-compat`, the server also offers mcp-gateway's names with
their arguments: `outline_file`, `search_text`, `read_text_file`,
`search_files`, `list_directory`. The `gateway-docs` server and its
instructions can then move to `mcp-docs` without a change to roles or
to agents' habits, and move to the new names later.

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
- no network: it opens no socket; under mcp-gateway, `network: false`;
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
3. **Packaging and adoption**: an RPM (`mcp-docs`) with the
   `collections.d` convention documented for other packages; a
   collection file for mcp-gateway's docs; mcp-gateway's `gateway-docs`
   definition switched to `mcp-docs --fs-compat`, then to the new tool
   names; the cost evaluation in CI.
4. **Ranked search**: `find_sections` (BM25 over sections).
5. **Long list items as sections**: mcp-gateway keeps its roadmap and
   decisions as list items under one heading (about 34 KB and 22 KB), so
   the outline cannot split them. An option per collection to treat
   top-level list items that open with a bold title or a number
   (`27. **Documentation that costs fewer tokens**`) as pseudo-headings
   one level below their heading.
6. **Other formats**: the scanner behind an interface (headings with
   extents, ids, metadata), so that AsciiDoc, reStructuredText or man
   pages can be collections too. Markdown only until there is a need.

## 12. Decisions

- **D1. A server for documents, not a file server with more tools.**
  Addresses, sections and limits are about documents; a file server
  has to stay general (and writable). `--fs-compat` keeps the way back.
- **D2. Outlines computed on demand, cached by file identity**, not
  generated at build time (mcp-gateway step 28's reason: always
  current, any Markdown file, no build step).
- **D3. Sections addressed by anchor id**, line numbers kept for ranges:
  ids survive edits and match the links documents already contain.
- **D4. A read limit for a context window (32 KiB), with parts**, not
  a file-size limit with refusal: a large section is still readable,
  and the first part teaches its shape.
- **D5. Search answers with sections.** The step from a match to the
  text to read is the server's, not one more call.
- **D6. Collections declared by files in `/usr` and `/etc`**, not
  discovered by walking `/usr/share/doc`: a package says its docs are
  for agents and describes them; nothing is exposed by accident.
- **D7. No embeddings, no network.** Literal, RE2 and (later) BM25
  search run anywhere, explain themselves, and need no model or index
  build step.
- **D8. Go, standard library, ported packages.** The same language and
  packaging as mcp-gateway, whose code the core comes from.
