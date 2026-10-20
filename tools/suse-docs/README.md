# SUSE documentation for mcp-docs

`convert` turns the documentation of SUSE Linux Enterprise Server 16.0
on documentation.suse.com into a collection that mcp-docs serves: it
clones [SUSE/doc-modular](https://github.com/SUSE/doc-modular), checks
out a pinned revision of `maintenance/SLE16.0`, and runs
`mcp-docs-convert` on the articles in `sles-16.0`
([docs/architecture.md](../../docs/architecture.md), §14.7).

```sh
tools/suse-docs/convert                    # build/sles-16.0, served from there
tools/suse-docs/convert build/sles-16.0 /usr/share/mcp-docs/docs/suse/sles-16.0
```

In a package build, which has no network, `SRC` names the unpacked
sources instead of a clone, and `REV` their revision (default: the
pinned one); `source-tarball` makes that tarball,
`doc-modular-<rev>.tar.xz`, without images and translations
([packaging/suse-docs](../../packaging/suse-docs/README.md)). Installed with the
package `mcp-docs-convert`, the script is
`/usr/share/mcp-docs-convert/suse-docs/convert` and uses the installed
converter ([packaging/suse](../../packaging/suse/README.md)).

The second argument is where the documents will be installed; the
collection file `sles-16.0.yaml` names it as the root. To try the
collection without installing it:

```sh
mkdir -p build/conf && cp build/sles-16.0/sles-16.0.yaml build/conf/
bin/mcp-docs --config-dirs build/conf
```

The converter prints what it could not convert (with `--strict`, an
error) and the sections over 8 KiB of their own text, which agents read
in parts (`--max-section`): candidates for splitting in the sources.
The collection's `README.md` lists the articles by task; the legal
notices and the articles' indexes carry `rank: false`, out of
`find_sections`' ranking.

## SUSE Linux Micro 6.0, 6.1 and 6.2

`convert-sle-micro` makes one collection, `sle-micro`, of the three
versions, a directory each (`6.0/`, `6.1/`, `6.2/`), each converted on
its own from its branch: `maintenance/SLE_Micro_6.0`,
`maintenance/SLE_Micro_6.1`, and for 6.2 `maintenance/SLE16.0` (the
branch of SLES 16.0, which has no Micro branch of its own), at pinned
revisions. `sle-micro-6.0`, `sle-micro-6.1` and `sle-micro-6.2` list the
articles the site publishes under `/sle-micro/V/html/` (29, 35 and 38).
The versions share most topics under the same ids, so the collection
file tells agents to pass `find_sections` and `search` the version's
directory as `doc` (`sle-micro/6.2`).

```sh
tools/suse-docs/convert-sle-micro          # build/sle-micro
MCP_DOCS_SLE_MICRO=$PWD/build/sle-micro go test -run TestSLEMicro -v ./internal/docbook/
```

## Updating SLES 16.0

`sles-16.0` lists the build definitions of the articles the site
publishes under `/sles/16.0/html/`, from its `sitemap.xml`. To update
it, or the pinned revision in `convert`, run the conversion and the
check of the served collection:

```sh
tools/suse-docs/convert
MCP_DOCS_SLES16=$PWD/build/sles-16.0 go test -run TestSLES16 -v ./internal/docbook/
```

The documents are SUSE's, under the GNU Free Documentation License 1.2
or later with an invariant section; the collection's `README.md` names
the license and links the notices. Distribute them with it.
