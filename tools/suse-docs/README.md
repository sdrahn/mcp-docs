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
sources of the pinned revision instead of a clone; installed with the
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
