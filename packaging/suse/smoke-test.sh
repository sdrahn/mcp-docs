#!/bin/sh
# smoke-test.sh: check the installed mcp-docs and mcp-docs-convert
# packages, as root, on a system it may change (CI's openSUSE
# containers): the program, the directories it owns, and an MCP session
# on a collection file that serves mcp-docs' documentation (from the
# checkout it runs in: container images may install packages without
# their %doc files); then the converter on the article the checkout's
# tests use, served.
#
#   packaging/suse/smoke-test.sh <version>
set -eu

version=${1:?usage: smoke-test.sh <version>}
fail() {
	echo "smoke-test: $*" >&2
	exit 1
}
docdir=/tmp/smoke-docs
src=$(cd "$(dirname "$0")/../.." && pwd)
confdir=$src/.smoke-collections.d
cleanup() {
	rm -f /usr/share/mcp-docs/collections.d/mcp-docs.yaml /etc/mcp-docs/collections.d/broken.yaml
	rm -rf "$docdir" "$confdir" /tmp/smoke-converted
}
cleanup
trap cleanup EXIT

mcp-docs --version | grep -qx "mcp-docs $version" || fail "mcp-docs --version: $(mcp-docs --version)"
for d in /usr/share/mcp-docs/collections.d /etc/mcp-docs/collections.d; do
	test -d "$d" || fail "$d is missing"
	rpm -qf "$d" | grep -q '^mcp-docs-' || fail "$d is not owned by mcp-docs"
done

# Without collections it refuses to start, and says why.
if mcp-docs </dev/null 2>/tmp/smoke.err; then
	fail "mcp-docs started without collections"
fi
grep -q 'no collections' /tmp/smoke.err || fail "without collections: $(cat /tmp/smoke.err)"

# mcp-docs' documentation as a collection, as a package would ship one;
# a broken file beside it is logged and left out.
mkdir -p "$docdir"
cp "$src/README.md" "$src/docs/architecture.md" "$docdir/"
cat >/usr/share/mcp-docs/collections.d/mcp-docs.yaml <<EOT
title: mcp-docs documentation
root: $docdir
description: The documentation of mcp-docs.
EOT
echo 'root: relative' >/etc/mcp-docs/collections.d/broken.yaml

{
	echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}'
	echo '{"jsonrpc":"2.0","method":"notifications/initialized"}'
	echo '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_docs","arguments":{}}}'
	echo '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"outline","arguments":{"doc":"mcp-docs/architecture.md","maxLevel":2}}}'
	echo '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"read_section","arguments":{"doc":"mcp-docs/architecture.md#12-decisions"}}}'
	echo '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"search","arguments":{"query":"Landlock","maxResults":3}}}'
	echo '{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"read_lines","arguments":{"doc":"/etc/passwd"}}}'
	echo '{"jsonrpc":"2.0","id":7,"method":"resources/read","params":{"uri":"docs://mcp-docs/README.md"}}'
	sleep 2
} | timeout 30 mcp-docs >/tmp/smoke.out 2>/tmp/smoke.err || fail "the session failed: $(cat /tmp/smoke.err)"

expect() {
	grep -q -- "$1" /tmp/smoke.out || fail "no $1 in the answers: $(cat /tmp/smoke.out)"
}
expect '"serverInfo":{"name":"mcp-docs"'
expect 'mcp-docs (mcp-docs documentation): The documentation of mcp-docs.'
expect 'mcp-docs/architecture.md — mcp-docs architecture'
expect '#12-decisions'
expect '\*\*D1. A server for documents'
expect '§ '
expect 'outside the documentation'
expect '"uri":"docs://mcp-docs/README.md"'
grep -q 'broken.yaml: root "relative"' /tmp/smoke.err || fail "the broken file was not reported: $(cat /tmp/smoke.err)"
[ "$(grep -c 'broken.yaml' /tmp/smoke.err)" = 1 ] || fail "reported more than once: $(cat /tmp/smoke.err)"
grep -q 'serving mcp-docs=' /tmp/smoke.err || fail "no start line: $(cat /tmp/smoke.err)"

# Collection files outside the trees Landlock always allows (/etc, /usr,
# /tmp): mcp-docs reads them again after restricting itself.
mkdir -p "$confdir"
cp /usr/share/mcp-docs/collections.d/mcp-docs.yaml "$confdir/"
{
	echo '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"find_sections","arguments":{"query":"read only the part you need"}}}'
	sleep 2
} | timeout 30 mcp-docs --config-dirs "$confdir" >/tmp/smoke.out 2>/tmp/smoke.err || fail "--config-dirs $confdir: $(cat /tmp/smoke.err)"
expect 'section(s) for'

# Over HTTP: one POST per message, health at /healthz.
mcp-docs --config-dirs "$confdir" --listen 127.0.0.1:18080 2>/tmp/smoke-http.err &
pid=$!
for _ in 1 2 3 4 5 6 7 8 9 10; do
	curl -sf http://127.0.0.1:18080/healthz >/dev/null 2>&1 && break
	sleep 0.5
done
curl -sS -H 'Content-Type: application/json' \
	-d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_docs","arguments":{}}}' \
	http://127.0.0.1:18080/mcp >/tmp/smoke.out || fail "HTTP: $(cat /tmp/smoke-http.err)"
kill -TERM "$pid"
wait "$pid" || fail "HTTP: did not stop cleanly: $(cat /tmp/smoke-http.err)"
expect 'mcp-docs/architecture.md'
grep -q 'listening on http://127.0.0.1:18080/mcp' /tmp/smoke-http.err || fail "HTTP: $(cat /tmp/smoke-http.err)"

# The converter: its files, and SUSE's SELinux article (the checkout's
# test sources) converted and served.
mcp-docs-convert --version | grep -qx "mcp-docs-convert $version" || fail "mcp-docs-convert --version: $(mcp-docs-convert --version)"
for f in /usr/bin/mcp-docs-convert /usr/share/mcp-docs-convert/suse-docs/convert /usr/share/mcp-docs-convert/suse-docs/sles-16.0 \
	/usr/share/mcp-docs-convert/suse-docs/sle-ha-16.0 /usr/share/mcp-docs-convert/suse-docs/sles-sap-16.0 \
	/usr/share/mcp-docs-convert/suse-docs/convert-sle-micro /usr/share/mcp-docs-convert/suse-docs/sle-micro-6.2; do
	rpm -qf "$f" | grep -q '^mcp-docs-convert-' || fail "$f is not owned by mcp-docs-convert"
done
mcp-docs-convert --strict --src "$src/internal/docbook/testdata/doc-modular" --dc DC-SLES-SELinux \
	--out /tmp/smoke-converted --name sles-16.0 --site-url 'https://documentation.suse.com/sles/16.0/html/%s/' \
	2>/tmp/smoke.err || fail "mcp-docs-convert: $(cat /tmp/smoke.err)"
{
	echo '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"find_sections","arguments":{"query":"make a domain permissive"}}}'
	sleep 2
} | timeout 30 mcp-docs --collection sles-16.0=/tmp/smoke-converted >/tmp/smoke.out 2>/tmp/smoke.err || fail "the converted article: $(cat /tmp/smoke.err)"
expect 'sles-16.0/topics/selinux-permissive-domains.md'
expect '#setting-permissive-domain'

echo "smoke-test: mcp-docs $version works ($(grep -o 'Landlock.*$' /tmp/smoke.err | head -n 1 || echo 'Landlock: not reported'))"
