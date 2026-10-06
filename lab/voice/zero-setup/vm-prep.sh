#!/bin/bash
# Zero-setup lab, step 1 (as root in a desktop lab VM): install the
# packages under test and put the machine in the state an installed
# desktop has before anyone agreed to a download: no speech model, no
# language model, the local model service off, the translator off.
#
#   vm-prep.sh RPM_DIR
#
# RPM_DIR holds the unsigned test RPMs (basalt-shell, -selinux,
# basalt-voice, basalt-models, basalt-llm, -selinux, basalt-ledger and
# their Basalt dependencies); it becomes a local repository. Earlier lab
# models are moved aside to /root, never deleted. Lab only.
set -euo pipefail
rpms=${1:?usage: vm-prep.sh RPM_DIR}
dnf -y -q install createrepo_c glibc-langpack-pt >/dev/null
createrepo_c -q "$rpms"
repo=(--repofrompath=zs,"file://$rpms" --setopt=zs.gpgcheck=0)
pkgs=(basalt-shell basalt-shell-selinux basalt-voice basalt-models basalt-llm basalt-llm-selinux basalt-ledger)
dnf -y "${repo[@]}" install "${pkgs[@]}"
dnf -y "${repo[@]}" upgrade "${pkgs[@]}"
rpm -q "${pkgs[@]}"
systemctl disable --now basalt-llm.service || true
mkdir -p /root/zs-models-aside
mv /var/lib/basalt-llm/models/*.gguf /var/lib/basalt-voice/models/*.bin /root/zs-models-aside/ 2>/dev/null || true
sed -i '/^\[translator\]/,/^\[/ s/^enabled = yes/enabled = no/' /etc/basalt/assistant.conf
systemctl enable --now basalt-ledger.service
