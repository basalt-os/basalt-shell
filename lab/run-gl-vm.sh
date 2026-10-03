#!/bin/bash
# Run the desktop lab VM with 3D acceleration (virtio-gpu with virgl,
# rendered by the host GPU through egl-headless), outside libvirt.
#
# Why: libvirt runs QEMU in a private /dev and a device cgroup that only
# allow the render node; with NVIDIA's driver EGL also needs /dev/nvidia*,
# so egl-headless fails there. A QEMU started by the lab user sees the
# host's devices as that user does. niri refuses software (llvmpipe)
# rendering, so it needs this to run in a VM at all.
#
#   lab/run-gl-vm.sh import    copy the libvirt VM's disk and firmware variables (VM shut off)
#   lab/run-gl-vm.sh start     start (VNC and SPICE on $DESK_DISPLAY_LISTEN, SSH on 127.0.0.1:$DESK_SSH_PORT)
#   lab/run-gl-vm.sh stop      ACPI shutdown
#
# Uses QEMU user networking (its own NAT, isolated from other labs), no TPM.
set -euo pipefail
top=${LAB_TOP:-$(pwd)}
set -a; . "$top/.env"; set +a
vm=$top/vm
name=${VM_NAME}-gl
listen=${DESK_DISPLAY_LISTEN:-127.0.0.1}
vnc_display=${DESK_GL_VNC_DISPLAY:-33}     # port 5900 + 33
spice_port=${DESK_GL_SPICE_PORT:-5934}
ssh_port=${DESK_SSH_PORT:-2231}

import() {
  local state
  state=$(virsh -c qemu:///system domstate "$VM_NAME" 2>/dev/null || echo undefined)
  [ "$state" = "shut off" ] || { echo "$VM_NAME is $state; shut it down first" >&2; exit 1; }
  mkdir -p "$vm"
  sudo cp --sparse=always "$VM_DIR/$VM_NAME.qcow2" "$vm/disk.qcow2"
  sudo cp "/var/lib/libvirt/qemu/nvram/${VM_NAME}_VARS.qcow2" "$vm/vars.qcow2"
  sudo chown "$(id -u):$(id -g)" "$vm"/*
  chmod 0600 "$vm"/*
  echo "imported into $vm"
}

start() {
  local code=/usr/share/edk2/ovmf/OVMF_CODE_4M.secboot.qcow2
  [ -s "$LAB_DIR/desk-display.pass" ] || { echo "missing $LAB_DIR/desk-display.pass" >&2; exit 1; }
  systemd-run --user --unit="$name" --collect \
    qemu-system-x86_64 -name "$name" \
      -machine q35,accel=kvm,smm=on -cpu host -smp "${VM_VCPUS:-4}" -m "${VM_MEMORY_MB:-8192}" \
      -global driver=cfi.pflash01,property=secure,value=on \
      -drive if=pflash,format=qcow2,readonly=on,file="$code" \
      -drive if=pflash,format=qcow2,file="$vm/vars.qcow2" \
      -drive file="$vm/disk.qcow2",if=virtio,format=qcow2,discard=unmap \
      -device virtio-vga-gl,xres=1920,yres=1080 \
      -display egl-headless,rendernode="${DESK_RENDERNODE:-/dev/dri/renderD128}" \
      -vnc "$listen:$vnc_display,password=on" \
      -object secret,id=spicepw,file="$LAB_DIR/desk-display.pass" \
      -spice "port=$spice_port,addr=$listen,password-secret=spicepw" \
      -device virtio-tablet-pci -device virtio-keyboard-pci \
      -netdev "user,id=n0,hostfwd=tcp:127.0.0.1:$ssh_port-:22" \
      -device virtio-net-pci,netdev=n0,mac="${VM_MAC_PREFIX}:0a" \
      -audiodev spice,id=snd0 -device ich9-intel-hda -device hda-duplex,audiodev=snd0 \
      -monitor unix:"$vm/monitor.sock",server,nowait \
      -serial file:"$vm/serial.log"
  for _ in $(seq 20); do [ -S "$vm/monitor.sock" ] && break; sleep 0.5; done
  # VNC passwords are at most 8 characters.
  printf 'set_password vnc %s\n' "$(head -c 8 "$LAB_DIR/desk-display.pass")" | nc -U -q1 "$vm/monitor.sock" >/dev/null 2>&1 ||
    printf 'set_password vnc %s\n' "$(head -c 8 "$LAB_DIR/desk-display.pass")" | socat - UNIX-CONNECT:"$vm/monitor.sock" >/dev/null
  echo "started $name: VNC $listen:$((5900 + vnc_display)), SPICE $listen:$spice_port, SSH 127.0.0.1:$ssh_port"
}

stop() {
  printf 'system_powerdown\n' | nc -U -q1 "$vm/monitor.sock" >/dev/null 2>&1 || true
}

case "${1:-}" in
  import) import ;;
  start) start ;;
  stop) stop ;;
  *) sed -n '2,15p' "$0"; exit 2 ;;
esac
