#
# spec file for package suse-docs-mcp-sles-16.0
#
# SUSE's documentation of SUSE Linux Enterprise Server 16.0 for agents:
# the articles of documentation.suse.com, converted at build time from
# their DocBook sources into a collection that mcp-docs serves; see
# packaging/suse-docs/README.md. The source tarball comes from the
# _service file (or tools/suse-docs/source-tarball), at the revision
# below.
#

%define rev      a9f708401da072b024e66994c8f15d91250b836e
%define shortrev a9f7084
%define revdate  20261016
%define coll     sles-16.0

Name:           suse-docs-mcp-sles-16.0
Version:        16.0+git%{revdate}.%{shortrev}
Release:        0
Summary:        SUSE Linux Enterprise Server 16.0 documentation for agents, served by mcp-docs
# The documentation's; its Invariant Section is the copyright notice and
# license (topics/legal-disclaimer.md, topics/doc-gfdl-license.md).
License:        GFDL-1.2-or-later
Group:          Documentation/Other
URL:            https://documentation.suse.com/sles/16.0/
Source0:        doc-modular-%{shortrev}.tar.xz
BuildRequires:  mcp-docs-convert
BuildArch:      noarch

%description
The documentation of SUSE Linux Enterprise Server 16.0 from
documentation.suse.com, as a collection of Markdown documents for
mcp-docs, the MCP server that gives agents documentation a section at a
time: installation, administration, security, virtualization and
containers, one document per topic under the ids of the site's sections,
each linked to its page there.

Converted at build time from SUSE's DocBook sources (SUSE/doc-modular,
branch maintenance/SLE16.0, revision %{shortrev}). Without mcp-docs the
files do nothing.

%prep
%autosetup -n doc-modular-%{shortrev}

%build
SRC=$PWD REV=%{rev} %{_datadir}/mcp-docs-convert/suse-docs/convert \
	build/%{coll} %{_datadir}/mcp-docs/docs/suse/%{coll}

%install
install -d %{buildroot}%{_datadir}/mcp-docs/docs/suse %{buildroot}%{_datadir}/mcp-docs/collections.d
cp -a build/%{coll} %{buildroot}%{_datadir}/mcp-docs/docs/suse/%{coll}
mv %{buildroot}%{_datadir}/mcp-docs/docs/suse/%{coll}/%{coll}.yaml %{buildroot}%{_datadir}/mcp-docs/collections.d/%{coll}.yaml
# The license and legal notice are documents of the collection, which
# links them; the license directory points to them rather than holding
# copies.
install -d %{buildroot}%{_licensedir}/%{name}
for f in doc-gfdl-license.md legal-disclaimer.md; do
	ln -s ../../mcp-docs/docs/suse/%{coll}/topics/$f %{buildroot}%{_licensedir}/%{name}/$f
done

%check
d=%{buildroot}%{_datadir}/mcp-docs/docs/suse/%{coll}
test "$(find "$d" -name '*.md' | wc -l)" -ge 400
grep -qx 'root: %{_datadir}/mcp-docs/docs/suse/%{coll}' %{buildroot}%{_datadir}/mcp-docs/collections.d/%{coll}.yaml
grep -q '^source: "https://documentation.suse.com/sles/16.0/html/SLES-SELinux/index.html#selinux-switching-modes"' \
	"$d/topics/selinux-switching-modes.md"
grep -q 'revision %{shortrev}' "$d/README.md"
for f in doc-gfdl-license.md legal-disclaimer.md; do
	test -s "%{buildroot}%{_licensedir}/%{name}/$f"
done

%files
%dir %{_licensedir}/%{name}
%license %{_licensedir}/%{name}/doc-gfdl-license.md
%license %{_licensedir}/%{name}/legal-disclaimer.md
%dir %{_datadir}/mcp-docs
%dir %{_datadir}/mcp-docs/collections.d
%dir %{_datadir}/mcp-docs/docs
%dir %{_datadir}/mcp-docs/docs/suse
%{_datadir}/mcp-docs/collections.d/%{coll}.yaml
%{_datadir}/mcp-docs/docs/suse/%{coll}

%changelog
