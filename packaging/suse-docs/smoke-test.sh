#!/bin/sh
# smoke-test.sh: check an installed documentation package with mcp-docs
# installed, as root (CI's openSUSE containers): its files, and mcp-docs
# serving it from its collection file as an agent asks.
#
#   packaging/suse-docs/smoke-test.sh [PACKAGE]
#
# PACKAGE: suse-docs-mcp-sles-16.0 (the default), suse-docs-mcp-sle-ha-16.0,
# suse-docs-mcp-sles-sap-16.0 or suse-docs-mcp-sle-micro.
set -eu

pkg=${1:-suse-docs-mcp-sles-16.0}
fail() {
	echo "smoke-test: $*" >&2
	exit 1
}
call() { # id, tool, arguments
	echo "{\"jsonrpc\":\"2.0\",\"id\":$1,\"method\":\"tools/call\",\"params\":{\"name\":\"$2\",\"arguments\":$3}}"
}
case $pkg in
suse-docs-mcp-sles-16.0)
	coll=sles-16.0
	title='SUSE Linux Enterprise Server 16.0'
	owned=topics/selinux-switching-modes.md
	calls() {
		call 2 find_sections '{"query":"make a SELinux domain permissive"}'
		call 3 read_section '{"doc":"sles-16.0/topics/selinux-switching-modes.md#sellinux-temporary-mode-changes"}'
		call 4 find_sections '{"query":"build an RPM package from a live patch"}'
	}
	answers='sles-16.0/topics/selinux-permissive-domains.md
#setting-permissive-domain
Changing the SELinux mode temporarily
setenforce
#ulp-pack-livepatch'
	;;
suse-docs-mcp-sle-ha-16.0)
	coll=sle-ha-16.0
	title='SUSE Linux Enterprise High Availability 16.0'
	owned=topics/ha-sbd-setting-up-diskless.md
	calls() {
		call 2 find_sections '{"query":"set up diskless SBD","collection":"sle-ha-16.0"}'
		call 3 find_sections '{"query":"put a node into maintenance mode","collection":"sle-ha-16.0"}'
		call 4 read_section '{"doc":"sle-ha-16.0/topics/ha-sbd-setting-up-diskless.md#ha-sbd-setting-up-diskless"}'
	}
	answers='sle-ha-16.0/topics/ha-sbd-setting-up-diskless.md
#ha-maintenance-putting-node-into-maintenance-mode
Setting up diskless SBD'
	;;
suse-docs-mcp-sles-sap-16.0)
	coll=sles-sap-16.0
	title='SUSE Linux Enterprise Server for SAP applications 16.0'
	owned=topics/task-convert-sles-to-sap.md
	calls() {
		call 2 find_sections '{"query":"revert a saptune solution","collection":"sles-sap-16.0"}'
		call 3 find_sections '{"query":"convert a SLES system to SLES for SAP","collection":"sles-sap-16.0"}'
		call 4 read_section '{"doc":"sles-sap-16.0/topics/task-convert-sles-to-sap.md#task-convert-sles-to-sap"}'
	}
	answers='#sec-saptune-revert-solution
sles-sap-16.0/topics/task-convert-sles-to-sap.md
Migrate_SLES_to_SLES-for-SAP.sh'
	;;
suse-docs-mcp-sle-micro)
	coll=sle-micro
	title='SUSE Linux Micro 6.0, 6.1 and 6.2'
	owned=6.2/topics/selinux-switching-modes.md
	calls() {
		call 2 find_sections '{"query":"apply updates with transactional-update without rebooting","doc":"sle-micro/6.2"}'
		call 3 find_sections '{"query":"set up keylime remote attestation","doc":"sle-micro/6.0"}'
		call 4 read_section '{"doc":"sle-micro/6.1/topics/selinux-switching-modes.md#selinux-permanent-mode-changes"}'
	}
	answers='sle-micro/6.2/topics/transactional-update-no-reboot.md
sle-micro/6.0/topics/keylime-remote-attestation.md
Changing the SELinux mode permanently
/etc/selinux/config'
	;;
*)
	fail "unknown package $pkg"
	;;
esac

yaml=/usr/share/mcp-docs/collections.d/$coll.yaml
docs=/usr/share/mcp-docs/docs/suse/$coll
for f in "$yaml" "$docs/README.md" "$docs/$owned"; do
	rpm -qf "$f" | grep -q "^$pkg-" || fail "$f is not owned by $pkg"
done

{
	echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}'
	echo '{"jsonrpc":"2.0","method":"notifications/initialized"}'
	calls
	echo "{\"jsonrpc\":\"2.0\",\"id\":5,\"method\":\"resources/read\",\"params\":{\"uri\":\"docs://$coll/README.md\"}}"
	sleep 2
} | timeout 30 mcp-docs >/tmp/smoke.out 2>/tmp/smoke.err || fail "the session failed: $(cat /tmp/smoke.err)"

expect() {
	grep -q -- "$1" /tmp/smoke.out || fail "no $1 in the answers: $(cat /tmp/smoke.out)"
}
grep -q "serving .*$coll=$docs" /tmp/smoke.err || fail "not served: $(cat /tmp/smoke.err)"
expect "$title"
echo "$answers" | while read -r a; do
	expect "$a"
done

echo "smoke-test: $pkg is served ($(find "$docs" -name '*.md' | wc -l) documents)"
