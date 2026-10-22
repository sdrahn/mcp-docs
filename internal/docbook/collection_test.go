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
	answeredFirst(t, "sles-16.0", dir, sles16Questions)
}

// SUSE Linux Enterprise High Availability 16.0 (tools/suse-docs/convert,
// PRODUCT=sle-ha-16.0). MCP_DOCS_SLE_HA names the converted collection.
func TestSLEHA16(t *testing.T) {
	dir := os.Getenv("MCP_DOCS_SLE_HA")
	if dir == "" {
		t.Skip("MCP_DOCS_SLE_HA is not set (tools/suse-docs/README.md)")
	}
	answeredFirst(t, "sle-ha-16.0", dir, sleHA16Questions)
}

// SUSE Linux Enterprise Server for SAP applications 16.0
// (tools/suse-docs/convert, PRODUCT=sles-sap-16.0). MCP_DOCS_SLES_SAP
// names the converted collection.
func TestSLESSAP16(t *testing.T) {
	dir := os.Getenv("MCP_DOCS_SLES_SAP")
	if dir == "" {
		t.Skip("MCP_DOCS_SLES_SAP is not set (tools/suse-docs/README.md)")
	}
	answeredFirst(t, "sles-sap-16.0", dir, slesSAP16Questions)
}

// SUSE Linux Micro 6.0, 6.1 and 6.2, one collection, a directory per
// version (tools/suse-docs/convert-sle-micro): questions asked of one
// version (doc: its directory) find the answering section of that
// version first. MCP_DOCS_SLE_MICRO names the converted collection.
func TestSLEMicro(t *testing.T) {
	dir := os.Getenv("MCP_DOCS_SLE_MICRO")
	if dir == "" {
		t.Skip("MCP_DOCS_SLE_MICRO is not set (tools/suse-docs/README.md)")
	}
	answeredFirst(t, "sle-micro", dir, sleMicroQuestions)
}

type question struct {
	q    string
	want []string
	doc  string // only this document or directory
}

// answeredFirst serves a collection and asks find_sections each
// question: at least 80% must have an answering section first.
func answeredFirst(t *testing.T, name, dir string, questions []question) {
	t.Helper()
	s := &server.Server{Cat: catalog.New([]*catalog.Collection{{Name: name, Path: dir}}, []string{".md"}, 5000),
		MaxRead: 32 << 10, MaxFile: 1 << 20, Structured: true, Version: "test"}
	found := 0
	for _, c := range questions {
		args := map[string]any{"query": c.q}
		if c.doc != "" {
			args["doc"] = c.doc
		}
		p, _ := json.Marshal(map[string]any{"name": "find_sections", "arguments": args})
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
		if len(got) > 0 && strings.HasPrefix(got[0], c.doc) &&
			slices.ContainsFunc(c.want, func(w string) bool { return strings.HasSuffix(got[0], "#"+w) }) {
			found++
			continue
		}
		t.Logf("%q: %v, want #%s first", c.q, got, strings.Join(c.want, " or #"))
	}
	t.Logf("%d of %d questions answered first", found, len(questions))
	if found*10 < len(questions)*8 {
		t.Errorf("fewer than 80%% of the questions answered first")
	}
}

// sles16Questions name the sections that answer them; an article's
// index is no answer.
var sles16Questions = []question{
	{"open a port in a firewalld zone", []string{"firewalld-adding-zones-cli-add-port"}, ""},
	{"resize the disk of a virtual machine image", []string{"sec-guestfs-tools-virt-resize", "pro-guestfs-tools-virt-resize"}, ""},
	{"create a snapshot of a VM with virsh", []string{"sec-libvirt-managing-snapshots-virsh", "sec-libvirt-managing-snapshots-virsh-add"}, ""},
	{"enable and configure Yama", []string{"enable-yama-lsm"}, ""},
	{"configure GRUB 2 for PXE boot", []string{"sles-pxe-server-configure-grub2"}, ""},
	{"create a software RAID in Cockpit", []string{"cockpit-creating-raids"}, ""},
	{"build an RPM package from a live patch", []string{"ulp-pack-livepatch"}, ""},
	{"systemd timer does not run, troubleshooting", []string{"systemd-timer-troubleshoot"}, ""},
	{"set up multiple monitors in GNOME", []string{"task-GNOME-settings-configure-screen-multiple"}, ""},
	{"change the memory of a running VM in the QEMU monitor", []string{"sec-qemu-monitor-memory"}, ""},
	{"which file systems are supported by SLES 16", []string{"migration-guide-sle16-kernel-storage", "migration-filesystem-support"}, ""},
	{"NetworkManager network is not running", []string{"networkmanager-nmcli-troubleshoot-verify-status"}, ""},
	{"make a SELinux domain permissive", []string{"setting-permissive-domain"}, ""},
	{"configure the opensm InfiniBand subnet manager", []string{"task-example"}, ""},
	{"switch SELinux to permissive until the next boot", []string{"sec-getenforce-setenforce", "concept-SELinux"}, ""},
	{"migrate from Redis to Valkey", []string{"valkey-migrate"}, ""},
}

// sleMicroQuestions name the sections that answer them, in the version
// asked about.
var sleMicroQuestions = []question{
	{"apply updates with transactional-update without rebooting", []string{"transactional-update-no-reboot", "transactional-update-apply"}, "sle-micro/6.2"},
	{"perform a system rollback", []string{"tr-up-rollback", "task-performing-rollback"}, "sle-micro/6.2"},
	{"running containers with podman", []string{"podman-running-containers"}, "sle-micro/6.2"},
	{"change the SELinux mode permanently", []string{"selinux-permanent-mode-changes"}, "sle-micro/6.2"},
	{"create a network connection with nmcli", []string{"networkmanager-nmcli-configure"}, "sle-micro/6.2"},
	{"deploy a selfinstall ISO image", []string{"deployment-selfinstall-procedure"}, "sle-micro/6.2"},
	{"set up keylime remote attestation", []string{"keylime-remote-attestation"}, "sle-micro/6.2"},
	{"manage a software RAID", []string{"raid-management"}, "sle-micro/6.2"},
	{"set up zram", []string{"zram-setup"}, "sle-micro/6.2"},
	{"upgrade SLE Micro to 6.2", []string{"upgrading-micro-procedure"}, "sle-micro/6.2"},
	{"collect system information with supportconfig", []string{"supportconfig-collecting-information"}, "sle-micro/6.2"},
	{"filter logs with journalctl", []string{"logging-filtering-logs"}, "sle-micro/6.2"},
	{"register the system with transactional-update", []string{"transactional-update-registering-product"}, "sle-micro/6.2"},
	{"network is not running", []string{"networkmanager-nmcli-troubleshoot-verify-status"}, "sle-micro/6.2"},
	{"set up keylime remote attestation", []string{"keylime-remote-attestation"}, "sle-micro/6.0"},
	{"manage a software RAID", []string{"raid-management"}, "sle-micro/6.1"},
}

// sleHA16Questions name the sections that answer them.
var sleHA16Questions = []question{
	{"configure disk-based SBD", []string{"ha-sbd-setting-up-diskbased"}, ""},
	{"set up diskless SBD", []string{"ha-sbd-setting-up-diskless"}, ""},
	{"connect QDevice to the QNetd server", []string{"ha-qdevice-connecting-to-qnetd"}, ""},
	{"install a three-node cluster", []string{"ha-installing-cluster-overview"}, ""},
	{"add a location constraint for a resource", []string{"ha-resources-adding-location-constraints"}, ""},
	{"move a resource to another node manually", []string{"ha-resources-adding-relocation-constraints", "ha-resources-relocation-constraints-what-is"}, ""},
	{"put a node into maintenance mode", []string{"ha-maintenance-putting-node-into-maintenance-mode"}, ""},
	{"configure a fencing device", []string{"ha-fencing-creating-resources-for-physical-device"}, ""},
	{"log in to Hawk", []string{"ha-hawk-logging-in"}, ""},
	{"start the cluster services for the whole cluster", []string{"ha-maintenance-starting-cluster-services-whole-cluster"}, ""},
	{"clean up a failed resource", []string{"ha-resources-cleaning-up"}, ""},
	{"change the SBD timeout", []string{"ha-sbd-changing-timeout-settings"}, ""},
	{"what is a primitive resource", []string{"ha-resources-what-is-primitive"}, ""},
	{"put the whole cluster into maintenance mode", []string{"ha-maintenance-enabling-maintenance-mode"}, ""},
	{"resolve cluster issues after an upgrade", []string{"ha-upgrade-resolving-cluster-issues"}, ""},
	{"remove an SBD device", []string{"ha-sbd-removing-device"}, ""},
}

// slesSAP16Questions name the sections that answer them.
var slesSAP16Questions = []question{
	{"tune the system for SAP HANA with saptune", []string{"task-saptune-tuning-perform"}, ""},
	{"apply a saptune solution", []string{"task-saptune-manage-sap-sols-perform"}, ""},
	{"revert a saptune solution", []string{"sec-saptune-revert-solution"}, ""},
	{"edit a custom SAP Note", []string{"sec-saptune-sap-notes-edit"}, ""},
	{"verify system tuning with saptune", []string{"sap-verify-saptune"}, ""},
	{"convert a SLES system to SLES for SAP", []string{"task-convert-sles-to-sap"}, ""},
	{"what is SUSE Linux Enterprise Server for SAP applications", []string{"sles-sap-what-is"}, ""},
	{"deploy SAP S/4HANA on AWS with Ansible", []string{"deploy-sap-ansible-aws"}, ""},
	{"deploy SAP BW/4HANA on Google Cloud", []string{"deploy-sap-ansible-gcp"}, ""},
	{"deploy an SAP HANA high availability cluster with Ansible", []string{"deploy-sap-hana-ansible-hosts-ha"}, ""},
	{"install SLES for SAP with Agama", []string{"deployment-pxe-agama", "agama-manual-installation"}, ""},
	{"install SAP software patterns", []string{"sap-install-patterns"}, ""},
	{"SELinux support with SAP software", []string{"selinux-sap-support"}, ""},
	{"troubleshoot the SAP SELinux configuration", []string{"selinux-sap-troubleshooting"}, ""},
	{"how is high availability achieved for SAP HANA", []string{"sles-sap-about-ha"}, ""},
	{"what is the sles_sap_automation pattern", []string{"about-install-sap-pattern-ansible"}, ""},
}
