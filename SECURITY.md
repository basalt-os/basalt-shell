# Security policy

The Basalt shell lets AI agents ask for changes to a person's desktop, and
its main promise is that nothing an agent asks for happens without the
person's confirmation. A way around that confirmation, the SELinux
boundary or the audit log is a security problem. Please report it
privately and give us time to fix it before it is disclosed.

## Reporting a vulnerability

Use GitHub's private vulnerability reporting for this repository:
<https://github.com/basalt-os/basalt-shell/security/advisories/new>
(the "Report a vulnerability" button under the Security tab). This is the
preferred channel. If you cannot use GitHub, write to openbasalt@openbasalt.org
with "security" in the subject and without the details of the problem; we
will reply with a private way to share them.

Do not open a public issue, pull request or discussion for a security
problem. If the problem is in another part of Basalt OS (the system
assistant, basalt-agent, the installer), report it in
[basalt-os/basalt-os](https://github.com/basalt-os/basalt-os), or here if
you are not sure: we move reports between repositories.

Please include what you can of:

- the version (`basalt-shell version` or the package version), the
  compositor (sway, niri, headless) and the distribution;
- whether the `basalt_shell` SELinux module was installed and enforcing;
- steps to reproduce, and what an attacker gains (who the attacker is: an
  agent through MCP, another process of the same user, a remote party,
  a local user);
- whether you want to be credited, and how.

We aim to acknowledge a report within 7 days and to agree on a disclosure
date with you, normally within 90 days of the report or when a fix is
released, whichever comes first. Fixes are published as a new release with
a GitHub security advisory (and a CVE when it applies).

## Supported versions

The shell is a pre-release prototype. Until 1.0, only the latest release
receives security fixes.

## Scope

In scope: the daemon (`basalt-shelld`), the MCP server and command line
(`basalt-shell`), the shell UI launcher and QML, the session scripts, the
SELinux module in `selinux/`, the polkit policy and the packaging in this
repository.

Out of scope, report upstream: Quickshell, sway, niri, xdg-desktop-portal
and the applications that run in the session. Known limits documented in
[docs/selinux.md](docs/selinux.md) (for example, code running unconfined as
the same user without the policy) are not vulnerabilities by themselves,
but a way to cross a boundary that document says holds is.

## Verifying releases

Packages in the Basalt OS repository (<https://obpkg.org/basalt>) are signed
with the OpenBasalt release key, published at
<https://obpkg.org/keys/openbasalt-release-key.asc>. Check its fingerprint
before trusting it:

- primary key `3601 7348 42BD 4E48 2D19  DE4A E4EE D5EC A395 B302`
- packages signing subkey `3024 61D2 6520 E077 D07F  FCA9 AA27 C62C 36CC FC4B`
