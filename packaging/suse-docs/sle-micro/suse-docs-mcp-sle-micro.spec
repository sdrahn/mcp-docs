#
# spec file for package suse-docs-mcp-sle-micro
#
# SUSE's documentation of SUSE Linux Micro 6.0, 6.1 and 6.2 for agents:
# the articles of documentation.suse.com, converted at build time from
# their DocBook sources into one collection that mcp-docs serves, a
# directory per version; see packaging/suse-docs/README.md. The source
# tarballs, one per version, come from the _service file (or
# tools/suse-docs/source-tarball), at the revisions below.
#

%define rev60      7440233ef92838854c8966b87896efccdcee1c58
%define rev61      32a0b5d6b5d5ecf57941f3882ae74cfd8c99414f
%define rev62      a9f708401da072b024e66994c8f15d91250b836e
%define shortrev60 7440233
%define shortrev61 32a0b5d
%define shortrev62 a9f7084
# The newest of the three revisions' dates: a new revision of any version
# makes a newer package.
%define revdate    20261016
%define coll       sle-micro

Name:           suse-docs-mcp-sle-micro
Version:        6.2+git%{revdate}
Release:        0
Summary:        SUSE Linux Micro 6.0, 6.1 and 6.2 documentation for agents, served by mcp-docs
# The documentation's; its Invariant Section is the copyright notice and
# license (topics/legal-disclaimer.md, topics/doc-gfdl-license.md).
License:        GFDL-1.2-or-later
Group:          Documentation/Other
URL:            https://documentation.suse.com/sle-micro/
Source0:        doc-modular-%{shortrev60}.tar.xz
Source1:        doc-modular-%{shortrev61}.tar.xz
Source2:        doc-modular-%{shortrev62}.tar.xz
BuildRequires:  mcp-docs-convert
BuildArch:      noarch

%description
The documentation of SUSE Linux Micro 6.0, 6.1 and 6.2 from
documentation.suse.com, as one collection of Markdown documents for
mcp-docs, the MCP server that gives agents documentation a section at a
time: deployment, transactional updates, containers, security and system
administration, a directory per version, one document per topic under
the ids of the site's sections, each linked to its page there.

Converted at build time from SUSE's DocBook sources (SUSE/doc-modular:
6.0 from maintenance/SLE_Micro_6.0 at %{shortrev60}, 6.1 from
maintenance/SLE_Micro_6.1 at %{shortrev61}, 6.2 from maintenance/SLE16.0
at %{shortrev62}). Without mcp-docs the files do nothing.

%prep
%setup -q -c -T
mkdir -p src/6.0 src/6.1 src/6.2
tar -xf %{SOURCE0} -C src/6.0 --strip-components=1
tar -xf %{SOURCE1} -C src/6.1 --strip-components=1
tar -xf %{SOURCE2} -C src/6.2 --strip-components=1

%build
SRC=$PWD/src REV_60=%{rev60} REV_61=%{rev61} REV_62=%{rev62} \
	%{_datadir}/mcp-docs-convert/suse-docs/convert-sle-micro \
	build/%{coll} %{_datadir}/mcp-docs/docs/suse/%{coll}

%install
install -d %{buildroot}%{_datadir}/mcp-docs/docs/suse %{buildroot}%{_datadir}/mcp-docs/collections.d
cp -a build/%{coll} %{buildroot}%{_datadir}/mcp-docs/docs/suse/%{coll}
mv %{buildroot}%{_datadir}/mcp-docs/docs/suse/%{coll}/%{coll}.yaml %{buildroot}%{_datadir}/mcp-docs/collections.d/%{coll}.yaml
# The license and legal notice are documents of the collection, which
# links them; the license directory points to them (6.2's) rather than
# holding copies.
install -d %{buildroot}%{_licensedir}/%{name}
for f in doc-gfdl-license.md legal-disclaimer.md; do
	ln -s ../../mcp-docs/docs/suse/%{coll}/6.2/topics/$f %{buildroot}%{_licensedir}/%{name}/$f
done

%check
d=%{buildroot}%{_datadir}/mcp-docs/docs/suse/%{coll}
for v in 6.0 6.1 6.2; do
	test "$(find "$d/$v" -name '*.md' | wc -l)" -ge 250
done
grep -qx 'root: %{_datadir}/mcp-docs/docs/suse/%{coll}' %{buildroot}%{_datadir}/mcp-docs/collections.d/%{coll}.yaml
grep -q '^source: "https://documentation.suse.com/sle-micro/6.1/html/Micro-selinux/index.html#selinux-switching-modes"' \
	"$d/6.1/topics/selinux-switching-modes.md"
grep -q 'revision %{shortrev60}' "$d/6.0/README.md"
grep -q 'revision %{shortrev61}' "$d/6.1/README.md"
grep -q 'revision %{shortrev62}' "$d/6.2/README.md"
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
