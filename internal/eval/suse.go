package eval

// The questions of SUSE's documentation, by product: each with the ids
// of the sections that answer it. The collection tests ask them of one
// collection (internal/docbook); the eval of collections served
// together asks them of all, without naming one (together_test.go).

// Question is a question in words and the sections that answer it.
type Question struct {
	Ask string
	// Want are the ids of the sections that answer.
	Want []string
	// Doc, if set, is the document or directory the question is asked
	// of (a version of a collection that holds several).
	Doc string
}

// SLES name the sections that answer them; an article's
// index is no answer.
var SLES = []Question{
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

// SLEMicro name the sections that answer them, in the version
// asked about.
var SLEMicro = []Question{
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

// SLEHA name the sections that answer them.
var SLEHA = []Question{
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

// SLESSAP name the sections that answer them.
var SLESSAP = []Question{
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
