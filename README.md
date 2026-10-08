# mcp-docs

An MCP server that gives agents the documentation installed on a
system (Markdown files) a piece at a time: a catalog of documents, the
outline of a document, search that answers with sections, and reads of
one section or a range of lines, all read-only and bounded.

It generalises the `gateway-docs` server of
[mcp-gateway](https://github.com/sdrahn/mcp-gateway) (its roadmap steps
27 and 28: `search_text`, `outline_file`, ranged reads) to any set of
documents.

Status: step 1 (the core) is implemented; collections from
configuration files, resources, ranked search and packaging are to come.
See [docs/architecture.md](docs/architecture.md).

## Running it

```sh
make build
bin/mcp-docs --root /usr/share/mcp-gateway/docs
bin/mcp-docs --collection 'gateway=/usr/share/mcp-gateway/docs:The MCP gateway' \
             --collection 'kit=/usr/share/doc/packages/kit'
```

It speaks MCP on stdin/stdout. In Claude Code:

```sh
claude mcp add docs -- /usr/bin/mcp-docs --root /usr/share/mcp-gateway/docs
```

Options:

| Option | Default | |
|---|---|---|
| `--root DIR` | the current directory | a collection named after the directory (repeatable) |
| `--collection NAME=DIR[:DESC]` | | a named collection with a description (repeatable) |
| `--instructions TEXT` | | what the documents are; opens the server's instructions |
| `--max-read N` | 32768 | bytes one call returns at most |
| `--max-file N` | 16 MiB | the largest document search reads |
| `--max-entries N` | 10000 | documents a listing or search covers |
| `--ext LIST` | `.md,.markdown` | extensions of documents |
| `--fs-compat` | off | also offer mcp-server-fs's `outline_file`, `search_text`, `read_text_file`, `search_files`, `list_directory` |

The server only reads. It restricts itself with Landlock to reading
its collections, and a symbolic link that leads out of a collection is
refused.

### Behind mcp-gateway

To serve the gateway's documentation with mcp-docs, replace the
command of `gateway-docs` (`/etc/mcp-gateway/servers.d/gateway-docs.yaml`):

```yaml
command: ["/usr/bin/mcp-docs", "--fs-compat", "--collection",
  "mcp-gateway=/usr/share/mcp-gateway/docs:The documentation of mcp-gateway, for the installed version"]
```

With `--fs-compat`, the existing role `gateway-docs-reader` and its
instructions keep working; the new tools are offered beside them.

On a system with SELinux enforcing, the gateway's policy must also
allow `/usr/bin/mcp-docs` as an entry point of `mcpsrv_docs_t`: today
the domain's only entry point is `mcp-server-fs` (`mcpsrv_fs_exec_t`).
mcp-docs executes itself once more to apply Landlock to all of its
threads, so the domain also needs `can_exec` on its type. That policy
change belongs in mcp-gateway (plan step 3).

## Development

```sh
make check   # gofmt, go vet, go test -race
```

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
