#!/bin/bash
# Install the greeter from a source tree into a lab VM without an RPM
# (development only; run as root in the VM): the same paths as the
# basalt-greeter package. Usage: dev-install.sh SRC_DIR
set -euo pipefail
src=${1:?source tree}
install -d /usr/share/basalt-greeter/qml /usr/share/basalt-greeter/locale /usr/libexec/basalt-greeter
install -m644 "$src"/greeter/*.qml "$src"/greeter/logic.js /usr/share/basalt-greeter/qml/
install -m644 "$src"/greeter/locale/*.json /usr/share/basalt-greeter/locale/
install -m644 "$src"/greeter/sway.conf /usr/share/basalt-greeter/sway.conf
install -m755 "$src"/greeter/bin/greeter-session "$src"/greeter/bin/basalt-greeter "$src"/greeter/bin/greeter-ui /usr/libexec/basalt-greeter/
[ -e /etc/basalt/greeter.conf ] || install -Dm644 "$src"/config/greeter/greeter.conf /etc/basalt/greeter.conf
install -Dm644 "$src"/config/greeter/basalt-greeter.tmpfiles /usr/lib/tmpfiles.d/basalt-greeter.conf
systemd-tmpfiles --create /usr/lib/tmpfiles.d/basalt-greeter.conf
restorecon -R /usr/share/basalt-greeter /usr/libexec/basalt-greeter /run/basalt-greeter /var/cache/basalt-greeter 2>/dev/null || :
echo "installed from $src"
