#!/bin/bash
# Build and load the basalt_shell SELinux module from a source tree in a
# lab VM (needs selinux-policy-devel and basalt-agent-selinux's interface
# file; run as root). Development only.
set -euo pipefail
src=${1:?source tree}
work=$(mktemp -d)
cp "$src"/selinux/basalt_shell.{te,if,fc} "$work/"
make -s -C "$work" -f /usr/share/selinux/devel/Makefile basalt_shell.pp
semodule -i "$work/basalt_shell.pp"
rm -rf "$work"
echo "basalt_shell loaded"
