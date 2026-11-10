# Changelog

Newest first. Each release's section is its GitHub release notes and its
OBS changelog entry (packaging/suse/README.md).

## Unreleased

- An eval of collections served together (`make eval-together`,
  docs/architecture.md §17.3): the questions of SUSE's documentation
  asked of all its collections at once; CI runs it on the installed
  packages. 53 of 64 questions are answered first.
- `find_sections` ranks the collections of the system the server runs
  on first: a collection's index names its system in its front matter
  (`system: {"os_release": {"ID": "sles", "VERSION_ID": "16.1"}}`),
  the server compares it with `/etc/os-release` (`--os-release`), and
  `--prefer` names collections instead. On a SLES 16.1 system the
  first result is SLES 16.1's for every question of the eval, where
  one in seven was another version's or product's, and half as many
  results repeat a topic. The instructions mark the collection
  ("this system's").
- `mcp-docs-convert --system-os-release KEY=VALUE` and `--system-file`
  write a collection's system into its index; `tools/suse-docs/convert`
  does for each product.
- `search` does not match in a document's front matter, nor show it:
  a title there was the heading again. (`search_text` of `--fs-compat`
  searches files as they are.)
- `find_sections` folds variants: a result whose section has the id of
  an earlier one's and nearly its text (the topic in another product's
  or version's collection) is named with that one ("variants in
  sles-sap-16.1"; `variants` with `--structured`) and leaves its place
  to the next section. With SUSE's seven collections, 1 result of the
  eval's answers repeats a topic, from 38.
- `find_sections` also asks a question in the spelling the documents
  have: "login" finds "log in", "live patch" finds "livepatch", "file
  systems" finds "filesystem", by the collections' own words (two of
  the query's as one where the documents have it; one as two where
  they have both more often).
- The systems of SUSE's collections are told by their products' files
  (`/etc/products.d/SL-Micro.prod`, `SLES.prod`, …): SUSE Linux Micro
  6.2 is `ID=sles`, `VERSION_ID=16.0` in its os-release, which made
  SLES 16.0's collection that system's.

## v0.2.0 — 2026-11-10

The result of a review of the code (docs/architecture.md §16, with what
it measured before and after in §16.4): answers ranked across
collections, reads and search that cost what they return, an index
file in the documentation packages, a converter that keeps to its
sources.

- `mcp-docs-gateway`, a new package: the server `suse-docs` for
  mcp-gateway (`/usr/share/mcp-gateway/servers.d/suse-docs.yaml`),
  mcp-docs over all the documentation collections installed, in
  mcp-gateway's domain for mcp-docs, without network. It supplements
  mcp-docs where mcp-gateway is installed.
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
  document of 64 MiB), and scanning a document without a blank line
  kept all of it as its first paragraph. The server's peak memory on
  that document: 14 MB, from 321 MB.
- `search` for a text is about four times as fast (31 ms for 1,652
  documents, from 133 ms): each document is opened once, and its lines
  are looked at only if it holds the text.
- `find_sections` checks that its index is current without opening the
  documents, once a call; a collection with a document that is no text
  no longer has its index rebuilt on every call. Over HTTP, a
  collection's index being built does not hold up the others.
- With `--fs-compat`, `search_files`, `list_directory` and
  `outline_file` stop at `--max-read` and say so.
- `mcp-docs-convert` keeps to its sources: an include, a resource or
  a file of entities outside `--src` (by `../` or a symbolic link) is
  an error, and so is an `xml:id` that is no file name (`../../x` wrote
  outside `--out`). `--site-url` and `--source-url` may hold percent
  escapes.
- `mcp-docs-convert` renders an index term or remark inside a
  paragraph without splitting it, an inline image as its description
  in the text, table cells that span columns or rows in their columns,
  and a paragraph that starts like a heading or list item as text;
  links in an article's abstract lead to the topics (`../topics/…`;
  SLES 16.1's and SLE HA 16.1's upgrade articles had them broken).
  Entities in CDATA sections are left as written.
- Index files: a collection's directory may hold `mcp-docs.index`, the
  index of `find_sections`, which the server reads where it would
  build the index from every document (four collections, 1,652
  documents: 31 ms, from 289 ms in every session). `mcp-docs
  --write-index` writes it for the server's collections,
  `mcp-docs-convert --index DIR` for a converted one;
  `tools/suse-docs/convert` and `convert-sle-micro` do, so the
  `suse-docs-mcp-*` packages built with this release ship theirs. A file
  that is not of the documents as they are (their paths, sizes and
  checksums) is not used, and the server's log says why. A collection
  without the file has its index built as before, somewhat slower
  than in 0.1.3 (451 ms, from 394 ms): rebuild the documentation
  packages with this release's `mcp-docs-convert`.

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
