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
