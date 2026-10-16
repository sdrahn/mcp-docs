package docbook

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/sdrahn/mcp-docs/internal/catalog"
	"github.com/sdrahn/mcp-docs/internal/server"
)

// A whole converted collection, served (tools/suse-docs): questions in
// words across its articles find the answering section first.
// MCP_DOCS_SLES16 names the converted SLES 16.0 collection; without it
// the test is skipped.
func TestSLES16(t *testing.T) {
	dir := os.Getenv("MCP_DOCS_SLES16")
	if dir == "" {
		t.Skip("MCP_DOCS_SLES16 is not set (tools/suse-docs/README.md)")
	}
	s := &server.Server{Cat: catalog.New([]*catalog.Collection{{Name: "sles-16.0", Path: dir}}, []string{".md"}, 5000),
		MaxRead: 32 << 10, MaxFile: 1 << 20, Version: "test"}
	found := 0
	for _, c := range sles16Questions {
		p, _ := json.Marshal(map[string]any{"name": "find_sections", "arguments": map[string]any{"query": c.q}})
		res, err := s.Handle(context.Background(), "tools/call", p)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(res)
		var r struct {
			Structured struct {
				Sections []struct{ Doc, Section string }
			} `json:"structuredContent"`
		}
		if err := json.Unmarshal(b, &r); err != nil {
			t.Fatal(err)
		}
		var got []string
		for i, sec := range r.Structured.Sections {
			if i < 5 {
				got = append(got, sec.Doc+"#"+sec.Section)
			}
		}
		if len(got) > 0 && slices.ContainsFunc(c.want, func(w string) bool { return strings.HasSuffix(got[0], "#"+w) }) {
			found++
			continue
		}
		t.Logf("%q: %v, want #%s first", c.q, got, strings.Join(c.want, " or #"))
	}
	t.Logf("%d of %d questions answered first", found, len(sles16Questions))
	if found*10 < len(sles16Questions)*8 {
		t.Errorf("fewer than 80%% of the questions answered first")
	}
}

// sles16Questions name the sections that answer them; an article's
// index is no answer.
var sles16Questions = []struct {
	q    string
	want []string
}{
	{"open a port in a firewalld zone", []string{"firewalld-adding-zones-cli-add-port"}},
	{"resize the disk of a virtual machine image", []string{"sec-guestfs-tools-virt-resize", "pro-guestfs-tools-virt-resize"}},
	{"create a snapshot of a VM with virsh", []string{"sec-libvirt-managing-snapshots-virsh", "sec-libvirt-managing-snapshots-virsh-add"}},
	{"enable and configure Yama", []string{"enable-yama-lsm"}},
	{"configure GRUB 2 for PXE boot", []string{"sles-pxe-server-configure-grub2"}},
	{"create a software RAID in Cockpit", []string{"cockpit-creating-raids"}},
	{"build an RPM package from a live patch", []string{"ulp-pack-livepatch"}},
	{"systemd timer does not run, troubleshooting", []string{"systemd-timer-troubleshoot"}},
	{"set up multiple monitors in GNOME", []string{"task-GNOME-settings-configure-screen-multiple"}},
	{"change the memory of a running VM in the QEMU monitor", []string{"sec-qemu-monitor-memory"}},
	{"which file systems are supported by SLES 16", []string{"migration-guide-sle16-kernel-storage", "migration-filesystem-support"}},
	{"NetworkManager network is not running", []string{"networkmanager-nmcli-troubleshoot-verify-status"}},
	{"make a SELinux domain permissive", []string{"setting-permissive-domain"}},
	{"configure the opensm InfiniBand subnet manager", []string{"task-example"}},
	{"switch SELinux to permissive until the next boot", []string{"sec-getenforce-setenforce", "concept-SELinux"}},
	{"migrate from Redis to Valkey", []string{"valkey-migrate"}},
}
