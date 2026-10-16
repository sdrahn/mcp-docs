---
title: "Changing the SELinux mode"
description: "The topic covers switching of modes using the setenforce command and by changing the permanent configuration."
tags: ["task"]
source: "https://github.com/SUSE/doc-modular/blob/main/tasks/selinux-switching-modes.xml"
---

# Changing the SELinux mode {#selinux-switching-modes}

The topic covers switching of modes using the setenforce command and by changing the permanent configuration.

You can switch the SELinux mode temporarily or permanently.

## Verifying the active SELinux mode {#selinux-verify-mode}

To verify the mode, run the following command:

```
# getenforce
```

The command should return `enforcing` or `permissive`.

## Changing the SELinux mode temporarily {#sellinux-temporary-mode-changes}

To set SELinux to the `permissive` or `enforcing` mode temporarily, use the command `setenforce`.

The `setenforce` command has the following syntax:

```
# setenforce MODE_ID
```

where *MODE_ID* is *0* for the `permissive` mode or *1* for the `enforcing` mode.

Remember that you cannot disable SELinux using the `setenforce` command.

## Changing the SELinux mode permanently {#selinux-permanent-mode-changes}

To perform changes to the SELinux mode that persists after a reboot, edit the `/etc/selinux/config` configuration file. If SELinux is causing issues on your system, switch to the `permissive` mode and debug your system.

In the file `/etc/selinux/config`, change the value of `SELINUX` to `permissive`, or `enforcing` as follows:

```
SELINUX=permissive
```

The changes in the file are applied after the next reboot.
