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
