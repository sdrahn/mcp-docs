#!/bin/sh
# smoke-test.sh: check an installed documentation package (here
# suse-docs-mcp-sles-16.0) with mcp-docs installed, as root (CI's
# openSUSE containers): its files, and mcp-docs serving it from its
# collection file as an agent asks.
#
#   packaging/suse-docs/smoke-test.sh
set -eu

fail() {
	echo "smoke-test: $*" >&2
	exit 1
}
coll=/usr/share/mcp-docs/collections.d/sles-16.0.yaml
docs=/usr/share/mcp-docs/docs/suse/sles-16.0
for f in "$coll" "$docs/README.md" "$docs/topics/selinux-switching-modes.md"; do
	rpm -qf "$f" | grep -q '^suse-docs-mcp-sles-16.0-' || fail "$f is not owned by suse-docs-mcp-sles-16.0"
done

{
	echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}'
	echo '{"jsonrpc":"2.0","method":"notifications/initialized"}'
	echo '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"find_sections","arguments":{"query":"make a SELinux domain permissive"}}}'
	echo '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"read_section","arguments":{"doc":"sles-16.0/topics/selinux-switching-modes.md#sellinux-temporary-mode-changes"}}}'
	echo '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"find_sections","arguments":{"query":"build an RPM package from a live patch"}}}'
	sleep 2
} | timeout 30 mcp-docs >/tmp/smoke.out 2>/tmp/smoke.err || fail "the session failed: $(cat /tmp/smoke.err)"

expect() {
	grep -q -- "$1" /tmp/smoke.out || fail "no $1 in the answers: $(cat /tmp/smoke.out)"
}
grep -q 'serving sles-16.0=/usr/share/mcp-docs/docs/suse/sles-16.0' /tmp/smoke.err || fail "not served: $(cat /tmp/smoke.err)"
expect 'SUSE Linux Enterprise Server 16.0'
expect 'sles-16.0/topics/selinux-permissive-domains.md'
expect '#setting-permissive-domain'
expect 'Changing the SELinux mode temporarily'
expect 'setenforce'
expect '#ulp-pack-livepatch'

echo "smoke-test: suse-docs-mcp-sles-16.0 is served ($(find "$docs" -name '*.md' | wc -l) documents)"
