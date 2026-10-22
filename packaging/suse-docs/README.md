# Packages of SUSE's documentation for mcp-docs (OBS)

A package per product version that installs SUSE's documentation of it
as a collection for mcp-docs: the articles of documentation.suse.com,
converted at build time from their DocBook sources by `mcp-docs-convert`
([docs/architecture.md](../../docs/architecture.md), §14). Offline and
air-gapped, updated with the package.

| Package | Product | Sources |
|---|---|---|
| `suse-docs-mcp-sles-16.0` ([sles-16.0](sles-16.0)) | SUSE Linux Enterprise Server 16.0, its 48 articles (`tools/suse-docs/sles-16.0`) | SUSE/doc-modular, `maintenance/SLE16.0` |
| `suse-docs-mcp-sle-ha-16.0` ([sle-ha-16.0](sle-ha-16.0)) | SUSE Linux Enterprise High Availability 16.0, its 18 articles (`tools/suse-docs/sle-ha-16.0`) | SUSE/doc-modular, `maintenance/SLE16.0` (the same tarball as SLES 16.0's) |
| `suse-docs-mcp-sles-sap-16.0` ([sles-sap-16.0](sles-sap-16.0)) | SUSE Linux Enterprise Server for SAP applications 16.0, its 42 articles (`tools/suse-docs/sles-sap-16.0`) | SUSE/doc-modular, `maintenance/SLE16.0` (the same tarball as SLES 16.0's) |
| `suse-docs-mcp-sle-micro` ([sle-micro](sle-micro)) | SUSE Linux Micro 6.0, 6.1 and 6.2, their 29, 35 and 38 articles (`tools/suse-docs/sle-micro-V`), one collection, a directory per version | SUSE/doc-modular, `maintenance/SLE_Micro_6.0`, `maintenance/SLE_Micro_6.1`, `maintenance/SLE16.0` (6.2) |

CI builds them in the openSUSE Tumbleweed and Leap 16.0 containers after
mcp-docs (job `opensuse`), with the `mcp-docs-convert` package just
built, runs rpmlint, installs them and runs `smoke-test.sh PACKAGE`:
mcp-docs serves each from its collection file and answers questions
from it.

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

`suse-docs-mcp-sle-ha-16.0` and `suse-docs-mcp-sles-sap-16.0` are built
like the SLES package, from the same source tarball and revision
(`PRODUCT=sle-ha-16.0` or `PRODUCT=sles-sap-16.0` for `convert`), and
install the collections `sle-ha-16.0` and `sles-sap-16.0` the same way.

`suse-docs-mcp-sle-micro` installs the same way: the collection file
`/usr/share/mcp-docs/collections.d/sle-micro.yaml`, the documents below
`/usr/share/mcp-docs/docs/suse/sle-micro/` (`README.md`, then `6.0/`,
`6.1/`, `6.2/`, each with its own index, `articles/` and `topics/`), the
license symlinks to 6.2's. Its version is the newest product version and
the newest of the three revisions' dates, `6.2+git<date>`; the
revisions are in its description and the versions' indexes. It needs
three source tarballs, one per branch; its `_service` makes them.

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

For `suse-docs-mcp-sle-micro`, the same per version: the revisions in
`tools/suse-docs/convert-sle-micro` (`TestSLEMicro`), then the version's
revision in `_service` and `revN`, `shortrevN` in the spec, and
`revdate` to the newest of the three revisions' dates. 6.2 shares its
branch, and usually its revision, with SLES 16.0.

The package passes its revision to the converter (`REV=`), so a new
revision of the documentation needs no new mcp-docs-convert, unless the
sources use DocBook the converter does not know yet.
