# Packaging for openSUSE and SLES (OBS)

`mcp-docs.spec` and `_service` are ready to be used in an Open Build
Service project. CI builds the same spec in openSUSE Tumbleweed and
openSUSE Leap 16.0 containers (job `opensuse` in
`.github/workflows/ci.yml`), runs rpmlint, installs the package and runs
`smoke-test.sh` on it.

Build targets:

| Distribution | Status | Notes |
|---|---|---|
| SLES 16.0 | supported | an OBS build target; Go from the distribution (`golang(API) >= 1.24`) |
| openSUSE Leap 16.0 | supported | built from the SLES 16 sources; built and tested in CI |
| openSUSE Tumbleweed | development | built and tested in CI |

SLES 15 and Leap 15 are not targets, as for mcp-gateway: Landlock and
the SELinux setup the gateway integration relies on are those of the 16
series.

On all three, SELinux is enforcing by default (SLES 16 and Leap 16 moved
from AppArmor to SELinux). mcp-docs started by a user's agent (Claude
Code, Kit) runs in the user's domain and needs no policy of its own.
Started by mcp-gateway, it runs in the gateway's `mcpsrv_docs_t`, whose
policy (package `mcp-gateway-selinux`) labels `/usr/bin/mcp-docs` as its
entry point; see "Behind mcp-gateway" below. The kernels of all three
have Landlock, which mcp-docs applies to itself.

## Setting up the OBS package

```bash
osc mkpac mcp-docs               # in your project, e.g. home:<you>
cd mcp-docs
cp <repo>/packaging/suse/{mcp-docs.spec,_service} .
osc service manualrun            # fetch sources, set version, vendor Go modules
osc vc -m "Initial package"      # creates mcp-docs.changes
osc add *
osc commit
```

Add the repositories in the project's settings (web interface:
Repositories → Add from a distribution): openSUSE Tumbleweed, openSUSE
Leap 16.0 and SUSE Linux Enterprise Server 16.0, for x86_64 and aarch64
(and s390x, ppc64le where wanted; the spec builds position-independent
executables, with gcc for the architectures where Go links externally).
To build next to mcp-gateway, use the same project, so that its packages
and mcp-docs can recommend each other.

The services run in `manual` mode: `tar_scm` fetches `main` from GitHub
and names the tarball `mcp-docs-0.1.0+git<date>.<hash>.tar.gz`,
`set_version` puts that version into the spec, and `go_modules` creates
`vendor.tar.gz`, because OBS builds have no network access. Re-run
`osc service manualrun` to update, then `osc vc` and commit.

### Releases

Each release has a branch `release-X.Y` and tags `vX.Y.Z`. Pushing a tag
publishes a GitHub release with `mcp-docs-X.Y.Z.tar.gz`, `vendor.tar.gz`
and `SHA256SUMS` (`.github/workflows/release.yml`). On a release branch,
`_service` fetches that branch (`<param name="revision">release-X.Y</param>`)
and takes the version from its latest tag
(`<param name="versionformat">@PARENT_TAG@</param>` with
`<param name="versionrewrite-pattern">v(.*)</param>`), so the package
version is `X.Y.Z`. For an OBS project that follows a release, use the
`_service` of that branch, run `osc service manualrun`, then
`osc vc -m "Update to X.Y.Z"` with the release's CHANGELOG.md section,
and commit.

Before tagging, check the release branch: `tools/check-release vX.Y.Z`
(the spec's `Version`, the first CHANGELOG.md section and `_service` must
name the release, and the tree must hold no binary file and nothing over
1 MiB, `tools/check-tree`, which CI runs on every change). The Release
workflow runs the same check before it builds anything.

## The package

| Path | What |
|---|---|
| `/usr/bin/mcp-docs` | the server (MCP on stdin/stdout) |
| `/usr/share/mcp-docs/collections.d/` | collection files shipped by packages (owned by mcp-docs; packages that ship one own it too, with `%dir`) |
| `/etc/mcp-docs/collections.d/` | the administrator's collection files: one of the same name replaces a package's, an empty one disables it |
| `/usr/share/doc/packages/mcp-docs/` | README, CHANGELOG, architecture, `examples/collections.d/` |

## Making a package's documentation available to agents

A package that installs Markdown documentation ships a collection file,
without depending on mcp-docs (the file does nothing without it):

```spec
%install
install -Dm0644 mcp-docs-collection.yaml %{buildroot}%{_datadir}/mcp-docs/collections.d/%{name}.yaml

%files
%dir %{_datadir}/mcp-docs
%dir %{_datadir}/mcp-docs/collections.d
%{_datadir}/mcp-docs/collections.d/%{name}.yaml
```

```yaml
# mcp-docs-collection.yaml
title: Foo documentation
root: /usr/share/doc/packages/foo
description: The user guide and reference of foo, for the installed version.
include: ["**/*.md"]
```

How to write documentation that agents read a section at a time:
`docs/writing-docs.md` (installed in `/usr/share/doc/packages/mcp-docs/`).

Point `root` at documentation that is installed whatever the system's
settings: files marked `%doc` are left out on systems installed with
`--excludedocs` (rpm `%_excludedocs`, common in containers), so a
collection on `/usr/share/doc/packages` is empty there. mcp-gateway
installs its documentation below `/usr/share/mcp-gateway/docs` for that
reason. A collection whose root is missing is logged and left out.

## Behind mcp-gateway

mcp-gateway's packages (from the release that ships them) provide:

- `/usr/share/mcp-docs/collections.d/mcp-gateway.yaml` (package
  `mcp-gateway`): the gateway's documentation as the collection
  `mcp-gateway`, for an agent that runs mcp-docs itself;
- the SELinux label of `/usr/bin/mcp-docs` as the entry point of
  `mcpsrv_docs_t` (package `mcp-gateway-selinux`);
- `/usr/share/mcp-gateway/profiles/gateway-docs-mcp-docs.yaml` (package
  `mcp-gateway-fs-server`): a definition of the `gateway-docs` server
  that runs mcp-docs with `--fs-compat` instead of `mcp-server-fs`. To
  use it:

  ```bash
  zypper in mcp-docs
  cp /usr/share/mcp-gateway/profiles/gateway-docs-mcp-docs.yaml /etc/mcp-gateway/servers.d/gateway-docs.yaml
  ```

  The gateway picks the definition up while it runs; the role
  `gateway-docs-reader` allows the new tools as well.

## Before submitting to openSUSE:Factory or SLE

- **Licenses:** mcp-docs is MIT. The binary also contains the vendored
  Go modules golang.org/x/sys (BSD-3-Clause) and gopkg.in/yaml.v3 (MIT
  and Apache-2.0); the `License:` tag names all three.
- **Review:** mcp-docs is not a daemon, opens no network socket and
  installs no setuid program, polkit rule or system user; it reads the
  directories its collection files name, restricted to them with
  Landlock.
