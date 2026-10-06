#!/bin/bash
# Build and load the basalt_greeter SELinux module from a source tree in a
# lab VM (needs selinux-policy-devel; run as root). PERMISSIVE=1 marks the
# domain permissive (development only: collect denials without breaking).
set -euo pipefail
src=${1:?source tree}
work=$(mktemp -d)
cp "$src"/selinux/basalt_greeter.{te,if,fc} "$work/"
[ "${PERMISSIVE:-0}" = 1 ] && printf '\npermissive basalt_greeter_t;\n' >>"$work/basalt_greeter.te"
make -s -C "$work" -f /usr/share/selinux/devel/Makefile basalt_greeter.pp
semodule -i "$work/basalt_greeter.pp"
rm -rf "$work"
restorecon -RF /usr/libexec/basalt-greeter /run/basalt-greeter /var/cache/basalt-greeter
echo "basalt_greeter loaded (permissive=${PERMISSIVE:-0})"
