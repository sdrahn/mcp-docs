#
# spec file for package suse-docs-mcp-rancher-ai-1.2
#
# SUSE's documentation of Rancher AI 1.2 for agents: the pages of
# documentation.suse.com/cloudnative, converted at build time from their
# AsciiDoc sources (rancher/rancher-ai-product-docs, built with Antora) into a
# collection that mcp-docs serves; see packaging/suse-docs/README.md.
# The source tarball comes from tools/suse-docs/source-tarball-antora
# (or the _service file), at the revision below.
# The documentation is licensed under the Apache License 2.0; at the
# revision below its repository does not have the LICENSE file yet (a
# pull request adds it). Once it has: set the revision to one with the
# file, add "%%license LICENSE" and remove the collection's copy in
# %install, as the other cloud-native products' packages do.
#

%define rev      6c872577a6db7b3da64af6d15809f4c1b4651231
%define shortrev 6c87257
%define revdate  20261105
%define coll     rancher-ai-1.2
%define repo     rancher-ai-product-docs

Name:           suse-docs-mcp-rancher-ai-1.2
# Not released yet: the sources mark the version a prerelease (-dev).
Version:        1.2.0~dev+git%{revdate}.%{shortrev}
Release:        0
Summary:        Rancher AI 1.2 documentation for agents, served by mcp-docs
License:        Apache-2.0
Group:          Documentation/Other
URL:            https://documentation.suse.com/cloudnative/rancher-ai/1.2.0/en/
Source0:        %{repo}-%{shortrev}.tar.xz
BuildRequires:  mcp-docs-convert
BuildArch:      noarch

%description
The documentation of Rancher AI 1.2 from
documentation.suse.com/cloudnative, as a collection of Markdown documents
for mcp-docs, the MCP server that gives agents documentation a section
at a time: Liz, the Rancher AI assistant: installing and configuring it, models,
how-tos for administrators and users, and release notes, one document
per page under the ids of the site's sections, each linked to its page
there.

Converted at build time from the documentation's AsciiDoc sources
(rancher/%{repo}, revision %{shortrev}). Without mcp-docs the files
do nothing.

%prep
%autosetup -n %{repo}-%{shortrev}

%build
PRODUCT=%{coll} SRC=$PWD REV=%{rev} %{_datadir}/mcp-docs-convert/suse-docs/convert-antora \
	build/%{coll} %{_datadir}/mcp-docs/docs/suse/%{coll}

%install
install -d %{buildroot}%{_datadir}/mcp-docs/docs/suse %{buildroot}%{_datadir}/mcp-docs/collections.d
cp -a build/%{coll} %{buildroot}%{_datadir}/mcp-docs/docs/suse/%{coll}
mv %{buildroot}%{_datadir}/mcp-docs/docs/suse/%{coll}/%{coll}.yaml %{buildroot}%{_datadir}/mcp-docs/collections.d/%{coll}.yaml

%check
d=%{buildroot}%{_datadir}/mcp-docs/docs/suse/%{coll}
test -s "$d/mcp-docs.index"
test "$(find "$d" -name '*.md' | wc -l)" -ge 10
grep -qx 'root: %{_datadir}/mcp-docs/docs/suse/%{coll}' %{buildroot}%{_datadir}/mcp-docs/collections.d/%{coll}.yaml
grep -q '^source: "https://documentation.suse.com/cloudnative/rancher-ai/1.2.0/en/quick-start.html"' \
	"$d/quick-start.md"
grep -q 'revision %{shortrev}' "$d/README.md"

%files
%dir %{_datadir}/mcp-docs
%dir %{_datadir}/mcp-docs/collections.d
%dir %{_datadir}/mcp-docs/docs
%dir %{_datadir}/mcp-docs/docs/suse
%{_datadir}/mcp-docs/collections.d/%{coll}.yaml
%{_datadir}/mcp-docs/docs/suse/%{coll}

%changelog
