# mcp-docs

An MCP server that gives agents the documentation installed on a
system (Markdown files) a piece at a time: a catalog of documents, the
outline of a document, search that answers with sections, and reads of
one section or a range of lines, all read-only and bounded.

It generalises the `gateway-docs` server of
[mcp-gateway](https://github.com/sdrahn/mcp-gateway) (its roadmap steps
27 and 28: `search_text`, `outline_file`, ranged reads) to any set of
documents.

Status: steps 1 to 3 are implemented: the core, collection files and
metadata, and packaging for openSUSE Tumbleweed, openSUSE Leap 16 and
SLES 16 ([packaging/suse](packaging/suse/README.md)); ranked search and
long list items are to come. See
[docs/architecture.md](docs/architecture.md).

How to write documentation that agents read cheaply, through mcp-docs
or anything that reads by section:
[docs/writing-docs.md](docs/writing-docs.md).

## Running it

```sh
make build
bin/mcp-docs            # the collections of /usr/share/mcp-docs/collections.d and /etc/mcp-docs/collections.d
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
| `--config-dirs LIST` | see below | directories of collection files, comma-separated |
| `--root DIR` | | a collection named after the directory (repeatable) |
| `--collection NAME=DIR[:DESC]` | | a named collection with a description (repeatable) |
| `--instructions TEXT` | | what the documents are; opens the server's instructions |
| `--max-read N` | 32768 | bytes one call returns at most |
| `--max-file N` | 16 MiB | the largest document search reads |
| `--max-entries N` | 10000 | documents a listing or search covers |
| `--ext LIST` | `.md,.markdown` | extensions of documents |
| `--fs-compat` | off | also offer mcp-server-fs's `outline_file`, `search_text`, `read_text_file`, `search_files`, `list_directory` |

### Collection files

A package makes its documentation readable by agents by installing a
collection file in `/usr/share/mcp-docs/collections.d/`
([example](examples/collections.d/mcp-gateway.yaml)): the directory,
a title and description, the index, include and exclude patterns,
single documents elsewhere (`also`), and instructions for the agent.
A file of the same name in `/etc/mcp-docs/collections.d/` replaces it;
an empty one disables it. A file that cannot be used is logged and left
out.

Without `--root` or `--collection`, mcp-docs serves the collections of
both directories; with either, only those given, unless `--config-dirs`
names directories too (a command-line collection replaces a file's of
the same name).

Documents' titles and descriptions come from their front matter
(`title`, `description`, `tags`), else from the first level-1 heading
and the first paragraph; `list_docs` shows them.

### Resources and prompts

Each document is also a resource, `docs://collection/path`, and
`docs://collection/path#section` reads one section; a document larger
than one read comes as its first part. The prompt `answer_from_docs`
(`question`, optional `collection`) asks the agent to answer from the
documentation the way the instructions teach.

The server only reads. It restricts itself with Landlock to reading
its collections, and a symbolic link that leads out of a collection is
refused.

### Behind mcp-gateway

mcp-gateway (from the release after 0.18.0) ships a definition of its
`gateway-docs` server on mcp-docs, for the administrator to enable:

```sh
zypper in mcp-docs
cp /usr/share/mcp-gateway/profiles/gateway-docs-mcp-docs.yaml /etc/mcp-gateway/servers.d/gateway-docs.yaml
```

With `--fs-compat`, the existing role `gateway-docs-reader` and the
agents' habits keep working; the new tools are offered beside them.
`mcp-gateway-selinux` labels `/usr/bin/mcp-docs` as the entry point of
the server's domain `mcpsrv_docs_t`. mcp-gateway also installs its
documentation as the collection `mcp-gateway`, for agents that run
mcp-docs themselves.

## Development

```sh
make check   # gofmt, go vet, go test -race
make eval    # what answers cost on a fixed corpus (internal/eval)
make build && make install DESTDIR=/tmp/root
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
