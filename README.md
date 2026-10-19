# mcp-docs

An MCP server that gives agents the documentation installed on a
system (Markdown files) a piece at a time: a catalog of documents, the
outline of a document, search that answers with sections, and reads of
one section or a range of lines, all read-only and bounded.

It generalises the `gateway-docs` server of
[mcp-gateway](https://github.com/sdrahn/mcp-gateway) (its roadmap steps
27 and 28: `search_text`, `outline_file`, ranged reads) to any set of
documents.

Status: steps 1 to 5 and 7 are implemented: the core, collection files
and metadata, packaging for openSUSE Tumbleweed, openSUSE Leap 16 and
SLES 16 ([packaging/suse](packaging/suse/README.md)), ranked search
(`find_sections`), list items as sections, and HTTP with a container
image. `mcp-docs-convert` converts SUSE's documentation
(documentation.suse.com) from its DocBook sources: all of SLES 16.0
([tools/suse-docs](tools/suse-docs/README.md)), packaged as
`suse-docs-mcp-sles-16.0` ([packaging/suse-docs](packaging/suse-docs/README.md)); other formats than
Markdown are designed, not built. See [docs/architecture.md](docs/architecture.md).

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

### Over HTTP, and in a container

For a server that many agents reach over the network, `--listen`
serves MCP's Streamable HTTP, statelessly: one JSON-RPC message per
POST to `/mcp`, health at `/healthz`. TLS and authentication belong to
the ingress or proxy in front of it.

```sh
bin/mcp-docs --listen :8080 --root /usr/share/mcp-gateway/docs
claude mcp add --transport http docs http://docs.example:8080/mcp
```

The container image ([packaging/container](packaging/container/README.md))
runs it on port 8080 as any user on a read-only root file system:

```sh
podman build -f packaging/container/Containerfile -t mcp-docs .
podman run --rm -p 8080:8080 --read-only mcp-docs
```

Options:

| Option | Default | |
|---|---|---|
| `--config-dirs LIST` | see below | directories of collection files, comma-separated |
| `--root DIR` | | a collection named after the directory (repeatable) |
| `--collection NAME=DIR[:DESC]` | | a named collection with a description (repeatable) |
| `--instructions TEXT` | | what the documents are; opens the server's instructions |
| `--max-read N` | 16384 | bytes one call returns at most |
| `--structured` | off | also return results as `structuredContent`, with output schemas, for clients that read the data; a model needs only the text ([§15](docs/architecture.md#15-token-cost)) |
| `--max-file N` | 16 MiB | the largest document search reads |
| `--max-entries N` | 10000 | documents a listing or search covers |
| `--ext LIST` | `.md,.markdown` | extensions of documents |
| `--list-items` | off | in the collections of `--root` and `--collection`, take list items and paragraphs that open with a bold title as sections |
| `--listen ADDR` | | serve Streamable HTTP on this address (`:8080`) instead of stdin/stdout |
| `--allow-origin URL` | | with `--listen`, a browser origin requests may come from (repeatable); requests without `Origin` are always served |
| `--max-concurrent N` | 32 | with `--listen`, requests handled at once |
| `--fs-compat` | off | also offer mcp-server-fs's `outline_file`, `search_text`, `read_text_file`, `search_files`, `list_directory` |
| `--tools LIST` | all | offer only these tools, comma-separated; the instructions name only them |

### Keeping token cost down

What an agent spends on documentation is mostly what comes around the
answer ([§15](docs/architecture.md#15-token-cost)). By default mcp-docs
sends each result once, as text, and answers a question in words with
one `find_sections` call that includes the best section. Narrow it
further where you run it:

- **On the server**, for every client: `--tools
  find_sections,read_section,outline` is the smallest useful set (about
  2.5 KB of tool definitions instead of 5.3 KB). Keep `search` for
  documentation looked up by error messages and settings, and
  `read_lines` and `list_docs` for collections without an index.
- **In the client**, where it can filter a server's tools: for example
  Kit (`~/.kit.yml`):

  ```yaml
  mcpServers:
    docs:
      type: local
      command: ["/usr/bin/mcp-docs"]
      allowedTools: ["find_sections", "read_section", "outline"]
  ```

- **Behind mcp-gateway**: bind the documentation server's role only to
  the users who need it; every server a user may reach adds its tool
  definitions to their sessions.

### Collection files

A package makes its documentation readable by agents by installing a
collection file in `/usr/share/mcp-docs/collections.d/`
([example](examples/collections.d/mcp-gateway.yaml)): the directory,
a title and description, the index, include and exclude patterns,
single documents elsewhere (`also`), instructions for the agent, and
`list_items: true` for documents that keep entries (steps, decisions)
as list items under one heading.
A file of the same name in `/etc/mcp-docs/collections.d/` replaces it;
an empty one disables it. A file that cannot be used is logged and left
out.

Without `--root` or `--collection`, mcp-docs serves the collections of
both directories; with either, only those given, unless `--config-dirs`
names directories too (a command-line collection replaces a file's of
the same name).

Documents' titles and descriptions come from their front matter
(`title`, `description`, `tags`), else from the first level-1 heading
and the first paragraph; `list_docs` shows them. `rank: false` keeps a
document (a license, a changelog) out of `find_sections`.

### Resources and prompts

Each document is also a resource, `docs://collection/path`, and
`docs://collection/path#section` reads one section; a document larger
than one read comes as its first part. The prompt `answer_from_docs`
(`question`, optional `collection`) asks the agent to answer from the
documentation the way the instructions teach.

The server only reads. It restricts itself with Landlock to reading
its collections, to no TCP connection, and to listening on its
`--listen` port only (none on stdin/stdout); a symbolic link that leads
out of a collection is refused.

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
| A question in words, document unknown | `find_sections` (the best section's text included; of a large one, its best part and its other parts) |
| A precise term (a setting, an error message) | `search` → `read_section` of the match |
| A known document | `outline` → `read_section` |
| Browsing | the collection's index, or `list_docs` a page at a time |
| A long section | `read_section` with `depth: 0` (intro and sub-outline), then the subsection, or `part: 2` |

## Tools

| Tool | Does |
|---|---|
| `list_docs` | collections, or a collection's documents with titles and sizes, a page at a time, `query` to filter |
| `outline` | a document's headings with section ids, lines and sizes |
| `search` | lines matching a text or RE2 expression, grouped by section |
| `find_sections` | sections ranked for a question in words, across documents: the best one's text, the next ones as `doc#id` |
| `read_section` | one section by id or heading path, in parts when large |
| `read_lines` | lines by number, checked against the document's version |
