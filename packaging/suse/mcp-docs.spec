#
# spec file for package mcp-docs
#
# Built on the Open Build Service for openSUSE Tumbleweed, openSUSE Leap 16
# and SLES 16; see packaging/suse/README.md. Sources: the tarball and
# vendor.tar.gz (Go modules, OBS builds are offline) come from the _service
# file.
#

Name:           mcp-docs
Version:        0.1.1
Release:        0
Summary:        MCP server that gives agents on-system documentation a section at a time
# MIT for mcp-docs; the others for the vendored Go modules linked in
# (golang.org/x/sys: BSD-3-Clause; gopkg.in/yaml.v3: MIT AND Apache-2.0).
License:        Apache-2.0 AND BSD-3-Clause AND MIT
Group:          Development/Tools/Other
URL:            https://github.com/sdrahn/mcp-docs
Source0:        %{name}-%{version}.tar.gz
Source1:        vendor.tar.gz
# PIE on architectures where Go links externally.
BuildRequires:  gcc
BuildRequires:  golang(API) >= 1.24
BuildRequires:  make

%description
mcp-docs is an MCP (Model Context Protocol) server for the Markdown
documentation installed on a system. An agent lists the documents of
the collections, reads a document's outline, searches with each match
placed in its section, and reads one section or a range of lines,
instead of whole files. The server only reads, restricts itself with
Landlock to its collections, and bounds what one call returns.

Packages make their documentation available to agents by installing a
collection file in %{_datadir}/mcp-docs/collections.d; the administrator
overrides or disables one in %{_sysconfdir}/mcp-docs/collections.d.

%package convert
Summary:        Converter of SUSE's DocBook documentation into collections for mcp-docs
# Only the Go standard library is linked in.
License:        MIT
Group:          Development/Tools/Doc Generators

%description convert
mcp-docs-convert converts articles of SUSE's DocBook documentation
sources (SUSE/doc-modular) into Markdown collections that mcp-docs
serves a section at a time: entities and profiling resolved, one
document per topic under the sources' ids, each linked to its page on
documentation.suse.com.

It is for building packages of SUSE's documentation, as a build
requirement: %{_datadir}/mcp-docs-convert/suse-docs/convert converts
the documentation of SUSE Linux Enterprise Server 16.0 from a source
tree of the pinned revision.

%prep
%autosetup -p1 -a1

%build
export GOFLAGS="-mod=vendor"
%make_build build VERSION=%{version}

%install
%make_install PREFIX=%{_prefix} BINDIR=%{_bindir} DATADIR=%{_datadir} SYSCONFDIR=%{_sysconfdir}

%check
export GOFLAGS="-mod=vendor"
go test ./...
./bin/mcp-docs --version | grep -qx 'mcp-docs %{version}'
./bin/mcp-docs-convert --version | grep -qx 'mcp-docs-convert %{version}'

%files
%license LICENSE
%doc README.md CHANGELOG.md docs/architecture.md docs/writing-docs.md examples
%{_bindir}/mcp-docs
%dir %{_datadir}/mcp-docs
%dir %{_datadir}/mcp-docs/collections.d
%dir %{_sysconfdir}/mcp-docs
%dir %{_sysconfdir}/mcp-docs/collections.d

%files convert
%license LICENSE
%{_bindir}/mcp-docs-convert
%{_datadir}/mcp-docs-convert

%changelog
