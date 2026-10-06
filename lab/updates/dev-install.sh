#!/bin/bash
# Install a development build of the shell (and, when given, of the system
# assistant's command line) into a lab VM without an RPM (development
# only; run as root in the VM): the same paths as the packages. The
# original files are kept once in /root/upd-orig.
#   dev-install.sh SRC_DIR BIN_DIR
# BIN_DIR holds basalt-shelld and basalt-shell (go build of their cmd/ packages), and
# optionally basalt (packages/basalt-assistant cmd/basalt of basalt-os).
set -euo pipefail
src=${1:?shell source tree}
bin=${2:?directory with the built binaries}
orig=/root/upd-orig
if [ ! -d "$orig" ]; then
  mkdir -p "$orig/qml" "$orig/locale"
  cp -a /usr/bin/basalt-shelld /usr/bin/basalt-shell /usr/libexec/basalt-shell/assistant-read "$orig/"
  cp -a /usr/share/basalt-shell/qml/*.qml "$orig/qml/"
  cp -a /usr/share/basalt-shell/locale/*.json "$orig/locale/"
  [ -e /usr/bin/basalt ] && cp -a /usr/bin/basalt "$orig/"
fi
install -m755 "$bin/basalt-shelld" "$bin/basalt-shell" /usr/bin/
[ -e "$bin/basalt" ] && install -m755 "$bin/basalt" /usr/bin/basalt
install -m755 "$src/libexec/assistant-read" /usr/libexec/basalt-shell/assistant-read
install -m644 "$src"/shell/*.qml /usr/share/basalt-shell/qml/
install -m644 "$src"/locale/*.json /usr/share/basalt-shell/locale/
restorecon -R /usr/bin/basalt-shelld /usr/bin/basalt-shell /usr/bin/basalt /usr/libexec/basalt-shell /usr/share/basalt-shell
echo "installed the development build from $src and $bin (originals in $orig)"
