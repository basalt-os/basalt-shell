#!/bin/bash
# Desktop lab: build a Basalt OS installer ISO with the desktop profile and
# install it into a VM with a virtual GPU, using the basalt-os lab scripts.
#
# Run on the lab host, in a directory that holds:
#   os-src/     a checkout or `git archive` of the basalt-os repository
#   shell-src/  this repository
#   .env        lab settings (see lab/env.example); copied to os-src/.env
#
#   lab/build-lab.sh iso       keys, packages (basalt-os + basalt-shell), signed repo, desktop ISO
#   lab/build-lab.sh install   install the ISO into $VM_NAME (unattended), then add the GPU and display
#   lab/build-lab.sh display   (re)configure the VM's display: virtio-gpu with virgl if the host can, SPICE
#
# The VM uses an emulated TPM (swtpm) only, never the host's TPM.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
top=${LAB_TOP:-$(pwd)}
os=$top/os-src
shell=$top/shell-src
[ -f "$top/.env" ] || { echo "missing $top/.env (see lab/env.example)" >&2; exit 1; }
cp "$top/.env" "$os/.env"
set -a; . "$top/.env"; set +a

iso() {
  # The desktop profile is added to a copy of the kickstart (the server
  # default stays as it is).
  python3 "$here/make-desktop-ks.py" <"$os/kickstart/basalt-server.ks" >"$os/kickstart/basalt-server.ks.new"
  mv "$os/kickstart/basalt-server.ks.new" "$os/kickstart/basalt-server.ks"
  make -C "$os" lab-keys rpms
  (cd "$shell" && PODMAN="${PODMAN:-sudo podman}" scripts/build-rpm.sh "$FEDORA_RELEASE")
  "$os/scripts/repo.sh" publish
  "$os/scripts/repo.sh" publish "$shell"/build/rpm/basalt-shell-*.rpm
  if [ ! -s "$ISO_CACHE/latest" ]; then make -C "$os" iso-fetch; fi
  # Site variant: no disk encryption (lab VM), desktop profile, a desktop
  # user that logs in automatically, its password kept in LAB_DIR.
  local site="$LAB_DIR/site-desktop"
  install -d -m 0755 "$site"
  cp "$SITE_DIR/site.ks" "$site/site.ks"
  { cat "$SITE_DIR/site.conf"; echo "# desktop variant"
    echo BASALT_ENCRYPT=0; echo BASALT_PROFILE=desktop; echo "BASALT_DESKTOP_AUTOLOGIN=$DESK_USER"; } >"$site/site.conf"
  umask 077
  [ -s "$LAB_DIR/desk-user.pass" ] || openssl rand -base64 18 | tr -d '/+=' | head -c 20 >"$LAB_DIR/desk-user.pass"
  umask 022
  hash=$(openssl passwd -6 -stdin <"$LAB_DIR/desk-user.pass")
  {
    echo "user --name=$DESK_USER --gecos=\"Basalt desktop\" --groups=wheel --iscrypted --password=$hash"
    echo "%post --log=/root/basalt-install-desk.log"
    echo "install -d -m 0700 -o $DESK_USER -g $DESK_USER /home/$DESK_USER/.ssh"
    echo "cat >/home/$DESK_USER/.ssh/authorized_keys <<'EOF'"
    cat "$LAB_DIR/keys/authorized_keys"
    echo "EOF"
    echo "chown $DESK_USER:$DESK_USER /home/$DESK_USER/.ssh/authorized_keys; chmod 0600 /home/$DESK_USER/.ssh/authorized_keys"
    echo "restorecon -R /home/$DESK_USER"
    echo "%end"
  } >>"$site/site.ks"
  SITE_DIR="$site" SITE_NAME=desktop make -C "$os" iso
}

display() {
  # virtio-gpu; 3D through virgl rendered by the host GPU (egl-headless)
  # when DESK_GL=1, else a 2D framebuffer and llvmpipe in the guest.
  # SPICE (and VNC) on the address in DESK_DISPLAY_LISTEN with a password.
  local v="virsh -c qemu:///system" xml
  xml=$(mktemp)
  $v dumpxml --inactive "$VM_NAME" >"$xml"
  python3 - "$xml" <<'PY'
import os, sys, xml.etree.ElementTree as ET
p = sys.argv[1]
t = ET.parse(p); d = t.getroot().find("devices")
for tag in ("graphics", "video"):
    for e in d.findall(tag):
        d.remove(e)
gl = os.environ.get("DESK_GL", "1") == "1"
listen = os.environ.get("DESK_DISPLAY_LISTEN", "127.0.0.1")
pw = open(os.environ["LAB_DIR"] + "/desk-display.pass").read().strip()
if gl:
    g = ET.SubElement(d, "graphics", type="egl-headless")
    ET.SubElement(g, "gl", rendernode=os.environ.get("DESK_RENDERNODE", "/dev/dri/renderD128"))
s = ET.SubElement(d, "graphics", type="spice", autoport="no", port=os.environ.get("DESK_SPICE_PORT", "5931"), passwd=pw)
ET.SubElement(s, "listen", type="address", address=listen)
ET.SubElement(s, "image", compression="off")
vn = ET.SubElement(d, "graphics", type="vnc", autoport="no", port=os.environ.get("DESK_VNC_PORT", "5932"), passwd=pw[:8])
ET.SubElement(vn, "listen", type="address", address=listen)
v = ET.SubElement(d, "video")
m = ET.SubElement(v, "model", type="virtio", heads="1", primary="yes")
if gl:
    ET.SubElement(m, "acceleration", accel3d="yes")
# Input devices for the display.
for kind in ("tablet", "keyboard"):
    if not any(i.get("type") == kind for i in d.findall("input")):
        ET.SubElement(d, "input", type=kind, bus="virtio")
t.write(p)
PY
  $v define "$xml" >/dev/null
  rm -f "$xml"
  echo "display: gl=${DESK_GL:-1} spice=${DESK_DISPLAY_LISTEN:-127.0.0.1}:${DESK_SPICE_PORT:-5931} vnc=:${DESK_VNC_PORT:-5932}"
}

install_vm() {
  umask 077
  [ -s "$LAB_DIR/desk-display.pass" ] || openssl rand -base64 12 | tr -d '/+=' | head -c 12 >"$LAB_DIR/desk-display.pass"
  umask 022
  "$os/scripts/lab/install.sh" "$BUILD_DIR/iso/basalt-os-$(cat "$os/VERSION")-x86_64-desktop.iso"
  virsh -c qemu:///system shutdown "$VM_NAME" >/dev/null || true
  for _ in $(seq 60); do [ "$(virsh -c qemu:///system domstate "$VM_NAME")" = "shut off" ] && break; sleep 2; done
  display
  virsh -c qemu:///system start "$VM_NAME"
}

case "${1:-}" in
  iso) iso ;;
  install) install_vm ;;
  display) display ;;
  *) sed -n '2,16p' "$0"; exit 2 ;;
esac
