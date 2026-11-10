# Changelog

Newest first. Each release's section is its GitHub release notes and its
OBS changelog entry (packaging/suse/README.md).

## Unreleased

- `search` matches no-break spaces as spaces: a query "SLES for SAP"
  finds the lines that write it with no-break spaces, as SUSE's
  documents do.
- `find_sections` ranks the collections it searches as one: a word
  weighs by how rare it is in all of them together. Ranked each by its
  own index, a collection where a word is rare put its passing mention
  above another collection's section on it. Results of one collection
  are as before.
- `find_sections` returns a section that several documents hold with
  the same text (a topic two products' collections share) once, naming
  the others: "also in sles-sap-16.1" (`alsoIn` with `--structured`).
- `search` across collections divides `maxResults` among them and
  names those that have more matches, where it searched the first
  until the limit.
- Benchmarks on real collections (`MCP_DOCS_BENCH`,
  docs/architecture.md §16).
- Reads keep to what one call returns in memory, too: `read_lines`
  without a range, `read_section` of a large section and a `docs://`
  resource read every line asked for before cutting (155 MB for a
  document of 64 MiB).
- `search` for a text is about four times as fast (31 ms for 1,652
  documents, from 133 ms): each document is opened once, and its lines
  are looked at only if it holds the text.
- `find_sections` checks that its index is current without opening the
  documents, once a call; a collection with a document that is no text
  no longer has its index rebuilt on every call. Over HTTP, a
  collection's index being built does not hold up the others.
- With `--fs-compat`, `search_files`, `list_directory` and
  `outline_file` stop at `--max-read` and say so.

## v0.1.3 — 2026-11-09

- The 16.1 family: SLES, SLE HA and SLES for SAP 16.1, their 87, 19 and
  71 articles as the collections `sles-16.1`, `sle-ha-16.1` and
  `sles-sap-16.1` (`PRODUCT=sles-16.1 tools/suse-docs/convert`, …),
  from `maintenance/SLE16.1`, where the sources are below `en/`;
  packaged as `suse-docs-mcp-sles-16.1`, `suse-docs-mcp-sle-ha-16.1`
  and `suse-docs-mcp-sles-sap-16.1`.
- `mcp-docs-convert` renders question-and-answer sets (`qandaset`).

## v0.1.2 — 2026-10-22

- SUSE Linux Enterprise Server for SAP applications 16.0: its 42
  articles as the collection `sles-sap-16.0`
  (`PRODUCT=sles-sap-16.0 tools/suse-docs/convert`), packaged as
  `suse-docs-mcp-sles-sap-16.0`.

## v0.1.1 — 2026-10-22

- `mcp-docs-convert` writes the product and version into every
  document's front matter (`--product`, `--product-version`), and into
  each article's index the architectures it applies to (also as tags)
  and the date of its first revision (`created`), with a line in the
  text: "SUSE Linux Enterprise High Availability 16.0. Architectures:
  AMD64/Intel 64, POWER, IBM Z. Created 2026-01-15, last revised
  2026-09-18." The metadata documentation.suse.com shows in its pages.

## v0.1.0 — 2026-10-21

First release: an MCP server that gives agents the Markdown documentation
on a system a section at a time, for openSUSE Tumbleweed, openSUSE Leap
16 and SLES 16, and a converter of SUSE's documentation into such
collections.

Server (`mcp-docs`):

- Tools:
  - `find_sections`: the sections that answer a question in words,
    ranked across documents (BM25 over each section's own text, a bonus
    for the words of its title). It returns the best section's text, up
    to 4 KiB. Of a larger section, it returns the subsection or passage
    that matches best, with a line for each of the section's other
    parts. `doc` limits it to a document or directory, `collection` to a
    collection; `read: false` returns only the list.
  - `search`: lines matching a text or an RE2 expression, grouped by
    section, with each section's size.
  - `read_section`: one section by id or heading path, in parts when
    large; `depth: 0` returns its introduction and the outline of its
    subsections.
  - `outline`: a document's headings with ids, lines and sizes; a large
    document's two upper levels.
  - `read_lines`: lines by number, checked against the document's
    version.
  - `list_docs`: collections, or a collection's documents with titles
    and sizes, a page at a time, filtered with `query`.
- Resources `docs://collection/path#section` and the prompt
  `answer_from_docs`.
- Small results:
  - each result once, as text; `structuredContent` and output schemas
    come with `--structured`;
  - one call returns at most 16 KiB (`--max-read`);
  - `--tools LIST` offers only the named tools, and the instructions
    name only those;
  - `make eval` measures what answers cost, and holds the per-session
    cost to a budget (docs/architecture.md, §15).
- Collections:
  - from `--root` and `--collection`, or from collection files in
    `/usr/share/mcp-docs/collections.d` (packages) and
    `/etc/mcp-docs/collections.d` (the administrator), with an index,
    include and exclude patterns, instructions and `list_items`;
  - titles and descriptions come from front matter, or else from the
    first heading and paragraph;
  - `rank: false` in front matter keeps a document (a license, an index)
    out of `find_sections`.
- `--list-items`: list items and paragraphs that open with a bold title
  are sections.
- `--fs-compat`: mcp-gateway's `mcp-server-fs` reading tools
  (`outline_file`, `search_text`, `read_text_file`, `search_files`,
  `list_directory`), so that mcp-gateway's `gateway-docs` server runs
  on mcp-docs.
- `--listen ADDR`: stateless MCP over Streamable HTTP. It has
  `/healthz`, `Origin` checks (`--allow-origin`), bounds on body size,
  concurrency (`--max-concurrent`) and time, and a graceful stop on
  SIGTERM.
- Read-only. Landlock restricts it to reading its collections, to no
  TCP connection, and to listening on its `--listen` port only.

Converter (`mcp-docs-convert`):

- Converts the articles of SUSE's DocBook sources (SUSE/doc-modular) into
  a Markdown collection:
  - entities, XInclude and profiling resolved;
  - one document per topic, under the sources' ids;
  - a variant per article where articles profile a topic differently;
  - an index per article, dated with its latest revision;
  - a collection index grouped by task;
  - each document's source is its page and anchor on
    documentation.suse.com.
- `--strict` fails on anything it could not convert; `--max-section`
  reports sections larger than an agent reads whole.
- `tools/suse-docs`: the articles of SUSE Linux Enterprise Server 16.0,
  SUSE Linux Enterprise High Availability 16.0 and SUSE Linux Micro 6.0,
  6.1 and 6.2, from pinned revisions.

Packages and images:

- `mcp-docs` and `mcp-docs-convert` (`packaging/suse`).
- Documentation packages, converted at build time from source tarballs
  (`packaging/suse-docs`):
  - `suse-docs-mcp-sles-16.0`;
  - `suse-docs-mcp-sle-ha-16.0`;
  - `suse-docs-mcp-sle-micro` (6.0, 6.1 and 6.2, a directory each).
- A container image (`packaging/container`): BCI micro, port 8080, any
  user, read-only root file system.
- Behind mcp-gateway 1.2.0 and later, the `gateway-docs` server can run
  on mcp-docs; mcp-gateway's SELinux policy labels `/usr/bin/mcp-docs`.

Documentation: README, docs/architecture.md, and a guide to writing
Markdown that agents read cheaply (docs/writing-docs.md).
