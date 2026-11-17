#
# spec file for package suse-docs-mcp-rancher-2.15
#
# SUSE's documentation of SUSE Rancher Prime 2.15 for agents: the pages of
# documentation.suse.com/cloudnative, converted at build time from their
# AsciiDoc sources (rancher/rancher-product-docs, built with Antora) into a
# collection that mcp-docs serves; see packaging/suse-docs/README.md.
# The source tarball comes from tools/suse-docs/source-tarball-antora
# (or the _service file), at the revision below.
# The version is the product's and the sources' revision.
#

%define rev      0f98c528e8735f7c5dff8e70e8fd44e274e3dcd7
%define shortrev 0f98c52
%define revdate  20261113
%define coll     rancher-2.15
%define repo     rancher-product-docs

Name:           suse-docs-mcp-rancher-2.15
Version:        2.15+git%{revdate}.%{shortrev}
Release:        0
Summary:        SUSE Rancher Prime 2.15 documentation for agents, served by mcp-docs
# The documentation's (LICENSE in its repository).
License:        Apache-2.0
Group:          Documentation/Other
URL:            https://documentation.suse.com/cloudnative/rancher-manager/v2.15/en/
Source0:        %{repo}-%{shortrev}.tar.xz
BuildRequires:  mcp-docs-convert
BuildArch:      noarch

%description
The documentation of SUSE Rancher Prime 2.15 from
documentation.suse.com/cloudnative, as a collection of Markdown documents
for mcp-docs, the MCP server that gives agents documentation a section
at a time: installing and upgrading Rancher, provisioning and managing Kubernetes
clusters, users and authentication, security, observability,
integrations, the API and troubleshooting, one document
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
test "$(find "$d" -name '*.md' | wc -l)" -ge 350
grep -qx 'root: %{_datadir}/mcp-docs/docs/suse/%{coll}' %{buildroot}%{_datadir}/mcp-docs/collections.d/%{coll}.yaml
grep -q '^source: "https://documentation.suse.com/cloudnative/rancher-manager/v2.15/en/installation-and-upgrade/install-rancher.html"' \
	"$d/installation-and-upgrade/install-rancher.md"
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
