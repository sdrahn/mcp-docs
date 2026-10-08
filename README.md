# mcp-docs

An MCP server that gives agents the documentation installed on a
system (Markdown files) a piece at a time: a catalog of documents, the
outline of a document, search that answers with sections, and reads of
one section or a range of lines, all read-only and bounded.

It generalises the `gateway-docs` server of
[mcp-gateway](https://github.com/sdrahn/mcp-gateway) (its roadmap steps
27 and 28: `search_text`, `outline_file`, ranged reads) to any set of
documents.

Status: design. See [docs/architecture.md](docs/architecture.md).

## The workflow it teaches

| Question | Calls |
|---|---|
| Which document? | `list_docs`, or the collection's index |
| A precise term (a setting, an error message) | `search` → `read_section` of the match |
| A broad question | `outline` → `read_section` |
| A long section | `read_section` with `depth: 0` (intro and sub-outline), then the subsection, or `part: 2` |

## Tools

| Tool | Does |
|---|---|
| `list_docs` | collections, or a collection's documents with titles, descriptions and sizes |
| `outline` | a document's headings with section ids, lines and sizes |
| `search` | lines matching a text or RE2 expression, grouped by section |
| `read_section` | one section by id or heading path, in parts when large |
| `read_lines` | lines by number, checked against the document's version |
| `find_sections` | (later) sections ranked for a question in words |
