#
# spec file for package suse-docs-mcp-suse-virtualization-1.8
#
# SUSE's documentation of SUSE Virtualization 1.8 for agents: the pages of
# documentation.suse.com/cloudnative, converted at build time from their
# AsciiDoc sources (rancher/harvester-product-docs, built with Antora) into a
# collection that mcp-docs serves; see packaging/suse-docs/README.md.
# The source tarball comes from tools/suse-docs/source-tarball-antora
# (or the _service file), at the revision below.
# The version is the product's and the sources' revision.
#

%define rev      5ddaf302dccb4b0634face899ee09ae5f3484289
%define shortrev 5ddaf30
%define revdate  20261117
%define coll     suse-virtualization-1.8
%define repo     harvester-product-docs

Name:           suse-docs-mcp-suse-virtualization-1.8
Version:        1.8+git%{revdate}.%{shortrev}
Release:        0
Summary:        SUSE Virtualization 1.8 documentation for agents, served by mcp-docs
# The documentation's (LICENSE in its repository).
License:        Apache-2.0
Group:          Documentation/Other
URL:            https://documentation.suse.com/cloudnative/virtualization/v1.8/en/
Source0:        %{repo}-%{shortrev}.tar.xz
BuildRequires:  mcp-docs-convert
BuildArch:      noarch

%description
The documentation of SUSE Virtualization 1.8 from
documentation.suse.com/cloudnative, as a collection of Markdown documents
for mcp-docs, the MCP server that gives agents documentation a section
at a time: installing and upgrading SUSE Virtualization (Harvester), hosts,
virtual machines and images, networking, storage, the integration with
Rancher, add-ons and troubleshooting, one document
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
# The license is the package's, not a document of the collection.
rm %{buildroot}%{_datadir}/mcp-docs/docs/suse/%{coll}/LICENSE

%check
d=%{buildroot}%{_datadir}/mcp-docs/docs/suse/%{coll}
test -s "$d/mcp-docs.index"
test "$(find "$d" -name '*.md' | wc -l)" -ge 110
grep -qx 'root: %{_datadir}/mcp-docs/docs/suse/%{coll}' %{buildroot}%{_datadir}/mcp-docs/collections.d/%{coll}.yaml
grep -q '^source: "https://documentation.suse.com/cloudnative/virtualization/v1.8/en/virtual-machines/live-migration.html"' \
	"$d/virtual-machines/live-migration.md"
grep -q 'revision %{shortrev}' "$d/README.md"

%files
%license LICENSE
%dir %{_datadir}/mcp-docs
%dir %{_datadir}/mcp-docs/collections.d
%dir %{_datadir}/mcp-docs/docs
%dir %{_datadir}/mcp-docs/docs/suse
%{_datadir}/mcp-docs/collections.d/%{coll}.yaml
%{_datadir}/mcp-docs/docs/suse/%{coll}

%changelog
