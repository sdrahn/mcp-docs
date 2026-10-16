---
title: "Disabling and reenabling SELinux"
description: "The topic covers enabling and disabling SELinux via the kernel command-line."
tags: ["task"]
source: "https://documentation.suse.com/sles/16.0/html/SLES-SELinux/index.html#selinux-enable-disable"
---

# Disabling and reenabling SELinux {#selinux-enable-disable}

The topic covers enabling and disabling SELinux via the kernel command-line.

> **Important: Disabling SELinux is not recommended**
>
> It is not recommended to disable SELinux. If SELinux is possibly causing issues to your system, switch to the `permissive` mode to debug your system.

SELinux is enabled by default in SLES 16.0. To disable or reenable SELinux requires a modification of the kernel command-line. This can be done temporarily for one boot or permanently by modifying the boot loader configuration.

## Modify the kernel command-line via the GRUB boot loader configuration {#selinux-enable-disable-grub}

To disable SELinux, edit the GRUB configuration file to include `selinux=0`:

```
# update-bootloader --add-option "selinux=0"
# update-bootloader --config
```

Verify that `/etc/default/grub` contains `selinux=0`:

```
GRUB_CMDLINE_LINUX_DEFAULT="...... selinux=0"
```

The change will be effective after reboot.

To verify that SELinux is disabled, run:

```
# sestatus
SELinux status:                 disabled
```

To enable SELinux again, edit the GRUB configuration file to remove the `selinux=0` option and relabel during reboot.

```
# update-bootloader --del-option "selinux=0"
# update-bootloader --config
# touch /etc/selinux/.autorelabel
```

SELinux is reenabled after reboot.

> **Note: Relabeling your system after reenabling SELinux**
>
> If you disable SELinux on your system and then enable it later, make sure that you relabel your system. When SELinux is disabled and you perform changes to your file system, the changes are not reflected in the context; for example, new files do not have any context.
>
> You must relabel your system by using the `restorecon` command and the `autorelabel` boot parameter. You can also create a file that will trigger relabeling on the next boot. To create the file, run the following command:
>
> ```
> # touch /etc/selinux/.autorelabel
> ```
>
> After reboot, the file `/etc/selinux/.autorelabel` is replaced with another flag file, `/etc/selinux/.relabelled`, to prevent relabeling on subsequent reboots.
