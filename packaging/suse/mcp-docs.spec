#
# spec file for package mcp-docs
#
# Built on the Open Build Service for openSUSE Tumbleweed, openSUSE Leap 16
# and SLES 16; see packaging/suse/README.md. Sources: the tarball and
# vendor.tar.gz (Go modules, OBS builds are offline) come from the _service
# file.
#

Name:           mcp-docs
Version:        0.1.0
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

%files
%license LICENSE
%doc README.md CHANGELOG.md docs/architecture.md docs/writing-docs.md examples
%{_bindir}/mcp-docs
%dir %{_datadir}/mcp-docs
%dir %{_datadir}/mcp-docs/collections.d
%dir %{_sysconfdir}/mcp-docs
%dir %{_sysconfdir}/mcp-docs/collections.d

%changelog
