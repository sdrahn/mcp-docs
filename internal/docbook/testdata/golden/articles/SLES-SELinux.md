---
title: "Understanding SELinux Basics"
description: "Learn about the basic components and functionality of SELinux"
tags: ["article","Security","Configuration"]
rank: false
source: "https://documentation.suse.com/sles/16.0/html/SLES-SELinux/index.html"
---

# Understanding SELinux Basics {#selinux-article-sles}

Security-Enhanced Linux (SELinux) is a robust security framework in SUSE Linux Enterprise Server that implements Mandatory Access Control to provide granular access policies beyond standard Linux permissions. By using a strict labeling system and security contexts, administrators can effectively confine system processes, mitigate zero-day exploits and maintain comprehensive system integrity.

The article's topics, in order:

- [About SELinux](../topics/selinux-about.md)
- [Why use SELinux?](../topics/selinux-benefits.md)
- [SELinux modes](../topics/selinux-modes.md)
  - [Changing the SELinux mode](../topics/selinux-switching-modes.md)
    - [Switching from permissive to enforcing mode](../topics/task-verification.md)
- [SELinux policy overview](../topics/selinux-policy.md)
  - [SELinux security context](../topics/selinux-context.md)
  - [SELinux Rules](../topics/selinux-rules.md)
  - [Design principles of the targeted SELinux policy](../topics/selinux-policy-targeted.md)
  - [Switching from `targeted` to `minimum` policy](../topics/switch-minimum-policy.md)
- [Understanding SELinux logs](../topics/selinux-logs.md)
  - [Reading the audit log](../topics/selinux-search-audit-log.md)
- [SELinux packages](../topics/selinux-packages.md)
- [SELinux administration](../topics/selinux-administration.md)
  - [SELinux Booleans](../topics/selinux-booleans.md)
    - [Working with Booleans](../topics/selinux-working-with-booleans.md)
  - [Managing custom file contexts](../topics/modify-context-type.md)
  - [Managing SELinux port contexts](../topics/selinux-modify-port.md)
  - [Setting permissive domains](../topics/selinux-permissive-domains.md)
  - [Working with SELinux modules](../topics/selinux-switching-modules.md)
    - [Creating a custom module (advanced)](../topics/selinux-creating-custom-module.md)
- [SELinux troubleshooting](../topics/selinux-troubleshooting.md)
- [Tools for managing SELinux](../topics/selinux-tools.md)
- [Disabling and reenabling SELinux](../topics/selinux-enable-disable.md)
- [Legal Notice](../topics/legal-disclaimer.md)
- [GNU Free Documentation License](../topics/doc-gfdl-license.md)
