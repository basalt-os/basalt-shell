#!/bin/bash
# Prepare the lab VM's packages (run as root in the VM; lab only): the
# published Basalt OS repositories (the OpenBasalt release key and
# https://obpkg.org/basalt; the lab disk was installed from a lab
# repository), then the current basalt-release, assistant and shell (the
# shell from basalt-testing), so the development build sits on the same
# policy and helpers. The originals are kept in /root/upd-orig.
set -euo pipefail
mkdir -p /root/upd-orig
curl -fsS -o /tmp/rk.asc https://obpkg.org/keys/openbasalt-release-key.asc
gpg --show-keys --with-colons /tmp/rk.asc 2>/dev/null | grep -q '^fpr:::::::::3601734842BD4E482D19DE4AE4EED5ECA395B302:'
[ -e /root/upd-orig/RPM-GPG-KEY-basalt ] || cp -a /etc/pki/rpm-gpg/RPM-GPG-KEY-basalt /etc/dnf/vars/basalt_repo_url /root/upd-orig/
install -m644 /tmp/rk.asc /etc/pki/rpm-gpg/RPM-GPG-KEY-basalt
echo https://obpkg.org/basalt >/etc/dnf/vars/basalt_repo_url
restorecon /etc/pki/rpm-gpg/RPM-GPG-KEY-basalt /etc/dnf/vars/basalt_repo_url
dnf -y -q upgrade basalt-release
dnf -y -q upgrade basalt-assistant basalt-assistant-selinux
dnf -y -q --enablerepo=basalt-testing upgrade basalt-shell basalt-shell-selinux
# The lab disk's /etc was owned by the lab user; rpm warns about it.
chown root:root /etc
rpm -q basalt-release basalt-assistant basalt-shell
