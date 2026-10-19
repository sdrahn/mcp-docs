# Changelog

Newest first. Each release's section is its GitHub release notes and its
OBS changelog entry (packaging/suse/README.md).

## Unreleased

First release, for openSUSE Tumbleweed, openSUSE Leap 16 and SLES 16.

- The tools `list_docs`, `outline`, `search`, `read_section` and
  `read_lines`: documents read a section or a range of lines at a time,
  search answers with sections, and one call returns at most 32 KiB
  (`--max-read`).
- `--fs-compat` offers mcp-gateway's `mcp-server-fs` tools for reading
  (`outline_file`, `search_text`, `read_text_file`, `search_files`,
  `list_directory`), so that mcp-gateway's `gateway-docs` server can run
  on mcp-docs unchanged.
- Collections from `--root` and `--collection`, or from collection files
  in `/usr/share/mcp-docs/collections.d` (packages) and
  `/etc/mcp-docs/collections.d` (the administrator).
- Titles and descriptions from front matter or the first heading and
  paragraph; resources `docs://collection/path#section`; the prompt
  `answer_from_docs`.
- Read-only, restricted with Landlock to the collections.
- Packaging for openSUSE and SLES (`packaging/suse`), and an evaluation
  of what answers cost on a fixed corpus (`make eval`).
- A guide to writing Markdown for agents (`docs/writing-docs.md`).
- `find_sections`: sections ranked for a question in words, across the
  documents of all collections or one (BM25 over each section's own
  text, a bonus for the words of its title), with the best-matching
  line of each.
- `list_items: true` in a collection file (`--list-items` on the command
  line): list items and paragraphs that open with a bold title are
  sections, for documents that keep steps or decisions as entries under
  one heading.
- `search` results give each section's size in bytes (`sectionBytes`).
- `--listen ADDR`: MCP over Streamable HTTP, stateless (one JSON-RPC
  message per POST to `/mcp`, no sessions, no event streams), with
  `/healthz`, `Origin` checks (`--allow-origin`), protocol version
  checks, bounds on body size, concurrency (`--max-concurrent`) and
  time, and a graceful stop on SIGTERM.
- Landlock also restricts TCP (kernels with ABI 4): no connections, and
  no listening on stdin/stdout or listening on the `--listen` port only.
- A container image (`packaging/container`): BCI micro, port 8080, any
  user, read-only root file system; built and queried in CI.
- Fixed: collection files in directories other than below `/etc`,
  `/usr` or `/tmp` (`--config-dirs`) were not found: mcp-docs reads them
  again after restricting itself with Landlock, which did not allow
  their directories. When no collection is left, the reasons are now
  printed.
- `mcp-docs-convert` (prototype): converts an article of SUSE's DocBook
  sources (SUSE/doc-modular) into a Markdown collection: entities and
  profiling resolved, every xml:id an anchor, one document per topic,
  an index per article, front matter with the source.
- `mcp-docs-convert` converts many articles into one collection
  (`--dc` more than once, `--dcs FILE`): a topic that articles profile
  differently is one document per variant (`topics/ID--ARTICLE.md`),
  article indexes are named by their build definition
  (`articles/SLES-SELinux.md`). `--site-url` makes each document's
  `source` its page and anchor on documentation.suse.com. New:
  `STRUCTID`, XInclude, callouts, glossaries, simple lists, results,
  modules without a topic.
- `tools/suse-docs/convert`: SUSE Linux Enterprise Server 16.0's 48
  articles from SUSE/doc-modular `maintenance/SLE16.0`, at a pinned
  revision, into a collection with its collection file.
- Package `mcp-docs-convert` (from the same spec): the converter and
  `tools/suse-docs` (below `/usr/share/mcp-docs-convert/suse-docs`), a
  build requirement for packages of SUSE's documentation; `convert`
  takes the sources from `SRC=` without network, and uses the installed
  converter outside a checkout. `mcp-docs-convert --version`. CI installs
  and smoke-tests it.
- Package `suse-docs-mcp-sles-16.0` (`packaging/suse-docs`): SUSE Linux
  Enterprise Server 16.0's documentation as the collection `sles-16.0`,
  converted at build time from a source tarball of SUSE/doc-modular
  (`_service`, or `tools/suse-docs/source-tarball`). CI builds it with
  the mcp-docs-convert package, lints, installs and queries it.
  `tools/suse-docs/convert` takes `REV=` with `SRC=`, so that a package
  names the revision it converts.
- Lower token cost for every client (docs/architecture.md, §15):
  - results are their text only; `structuredContent` and output schemas
    come with `--structured` (a client that reads them needs it);
  - `find_sections` gives one line per section (`doc#id`, heading path,
    size) and the words around the match, not the whole line;
  - `list_docs` comes a page at a time (`limit`, default 25; `offset`),
    filters with `query`, counts documents per directory, and shows
    descriptions only on a short or filtered list;
  - `outline` of a large document (over 40 headings) shows its two upper
    levels unless `maxLevel` is given;
  - `--max-read` defaults to 16 KiB (was 32 KiB);
  - the instructions lead with `find_sections` → `read_section`;
  - `make eval` reports the cost as JSON-forwarding clients receive it
    and holds the per-session cost (instructions, tool definitions) to a
    budget.
- One call per answer, and a smaller fixed cost (§15, phase 2):
  - `find_sections` returns the best section's text (up to 4 KiB; a
    larger one as its introduction and sub-outline) and the next two as
    `doc#id` lines; `read: false` returns the list of 5 as before;
  - shorter tool descriptions: 5.3 KB of tool definitions instead of
    7.3 KB (9.6 KB before phase 1);
  - `--tools LIST` offers only the named tools, and the instructions name
    only those (`find_sections,read_section,outline`: 2.5 KB);
  - README: keeping token cost down, on the server, in the client (Kit's
    `allowedTools`) and behind mcp-gateway.
- The best part of a large section (§15, phase 4):
  - `find_sections`, when its best section is over 4 KiB, returns the
    subsection that matches best (or a passage of at most 2 KiB) and a
    line for each of the section's other parts, instead of the
    section's introduction and outline;
  - `mcp-docs-convert` marks the articles' indexes `rank: false`;
  - SLES 16.0: the 16 test questions cost 37.7 KB in 18 calls instead
    of 63.8 KB in 19, and 15 of 16 find the answering section first.
- Documents shaped for sections (§15, phase 3):
  - `rank: false` in front matter keeps a document (a license, a
    changelog) out of `find_sections`;
  - `mcp-docs-convert` writes a compact collection index, the articles'
    titles grouped by task (SLES 16.0: 5.2 KB instead of 9.6 KB), marks
    the legal notices `rank: false`, and reports sections over
    `--max-section` (8 KiB) of their own text.
