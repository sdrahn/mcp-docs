# Packages of SUSE's documentation for mcp-docs (OBS)

A package per product version that installs SUSE's documentation of it
as a collection for mcp-docs: the articles of documentation.suse.com,
converted at build time from their DocBook sources by `mcp-docs-convert`
([docs/architecture.md](../../docs/architecture.md), §14). Offline and
air-gapped, updated with the package.

| Package | Product | Sources |
|---|---|---|
| `suse-docs-mcp-sles-16.0` ([sles-16.0](sles-16.0)) | SUSE Linux Enterprise Server 16.0, its 48 articles (`tools/suse-docs/sles-16.0`) | SUSE/doc-modular, `maintenance/SLE16.0` |

CI builds it in the openSUSE Tumbleweed and Leap 16.0 containers after
mcp-docs (job `opensuse`), with the `mcp-docs-convert` package just
built, runs rpmlint, installs it and runs `smoke-test.sh`: mcp-docs
serves it from its collection file and answers questions from it.

## The package

| Path | What |
|---|---|
| `/usr/share/mcp-docs/collections.d/sles-16.0.yaml` | the collection file: mcp-docs serves the collection `sles-16.0` once the package is installed |
| `/usr/share/mcp-docs/docs/suse/sles-16.0/` | `README.md` (the articles, source and license), `articles/` (an index per article), `topics/` (a document per topic) |
| `/usr/share/licenses/suse-docs-mcp-sles-16.0/` | the GNU Free Documentation License and SUSE's legal notice, the documentation's invariant section: symlinks to the collection's documents of them (`topics/doc-gfdl-license.md`, `topics/legal-disclaimer.md`), which its README links |

It does not require mcp-docs: without it, the files do nothing. The
version is the product's and the sources' revision,
`16.0+git<date>.<revision>`; the license is the documentation's, GFDL
1.2 or later.

## Setting up the OBS package

In the project that builds mcp-docs (which provides `mcp-docs-convert`,
the build requirement), for the SUSE Linux Enterprise Server 16.0
repository (and Leap 16.0, Tumbleweed where wanted):

```bash
osc mkpac suse-docs-mcp-sles-16.0
cd suse-docs-mcp-sles-16.0
cp <repo>/packaging/suse-docs/sles-16.0/{suse-docs-mcp-sles-16.0.spec,_service} .
osc service manualrun            # doc-modular-<rev>.tar.xz, without images and translations
osc vc -m "Initial package"
osc add *
osc commit
```

Without OBS services, `tools/suse-docs/source-tarball` makes the same
tarball from a clone.

## Updating to a newer revision of the sources

1. Pick the revision of `maintenance/SLE16.0` and convert it in a
   checkout: set `rev` in `tools/suse-docs/convert`, run it, and check
   the report (`--strict`: nothing unknown, unresolved, broken or in
   conflict) and the served collection (`TestSLES16`,
   [tools/suse-docs](../../tools/suse-docs/README.md)). Update the list
   of articles if the site's changed.
2. Set the revision in `_service` and `rev`, `shortrev`, `revdate` in
   the spec; `osc service manualrun`, `osc vc`, commit.

The package passes its revision to the converter (`REV=`), so a new
revision of the documentation needs no new mcp-docs-convert, unless the
sources use DocBook the converter does not know yet.
