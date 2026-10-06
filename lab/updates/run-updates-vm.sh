#!/bin/bash
# Lab VM for Settings, Updates and channels: a qcow2 overlay on an
# installed Basalt OS desktop disk (the backing file is opened read only and
# never changed), no TPM device (never the host's), Secure Boot firmware
# with its own copy of the variables.
#
#   lab/updates/run-updates-vm.sh create BACKING.qcow2 VARS.qcow2
#   lab/updates/run-updates-vm.sh start [gl|nogpu]
#   lab/updates/run-updates-vm.sh stop          ACPI shutdown
#   lab/updates/run-updates-vm.sh monitor CMD   one QEMU monitor command
#   lab/updates/run-updates-vm.sh shot FILE     screen dump (nogpu mode; gl mode: use VNC)
#
# Settings (environment): UPD_TOP (default: current directory),
# UPD_LISTEN (VNC and SPICE address, default 127.0.0.1), UPD_VNC (VNC
# display number, default 91), UPD_SPICE (default 5992), UPD_SSH (SSH
# port forwarded on 127.0.0.1, default 2311), UPD_PASS (file with the
# display password), UPD_RENDERNODE (default /dev/dri/renderD128).
set -euo pipefail
top=${UPD_TOP:-$(pwd)}
vm=$top/vm
name=basalt-upd-vm
listen=${UPD_LISTEN:-127.0.0.1}
vnc=${UPD_VNC:-91}
spice=${UPD_SPICE:-5992}
ssh_port=${UPD_SSH:-2311}
pass=${UPD_PASS:-$top/display.pass}

create() {
  local backing=$1 vars=$2
  mkdir -p "$vm"
  [ -e "$vm/disk.qcow2" ] && { echo "$vm/disk.qcow2 exists" >&2; exit 1; }
  qemu-img create -q -f qcow2 -F qcow2 -b "$(realpath "$backing")" "$vm/disk.qcow2"
  cp "$vars" "$vm/vars.qcow2"
  chmod 0600 "$vm"/*
  [ -s "$pass" ] || { head -c 12 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 12 >"$pass"; chmod 0600 "$pass"; }
  echo "created $vm (overlay on $backing)"
}

# QEMU options are comma separated by design.
# shellcheck disable=SC2054
start() {
  local code=/usr/share/edk2/ovmf/OVMF_CODE_4M.secboot.qcow2
  local gpu=(-device virtio-vga-gl,xres=1920,yres=1080 -display egl-headless,rendernode="${UPD_RENDERNODE:-/dev/dri/renderD128}")
  # QEMU options are comma separated by design.
  # shellcheck disable=SC2054
  case "${1:-gl}" in
    gl) ;;
    nogpu) gpu=(-device virtio-vga,xres=1920,yres=1080 -display none) ;;
    *) echo "start [gl|nogpu]" >&2; exit 2 ;;
  esac
  systemd-run --user --unit="$name" --collect \
    qemu-system-x86_64 -name "$name" \
      -machine q35,accel=kvm,smm=on -cpu host -smp 4 -m 6144 \
      -global driver=cfi.pflash01,property=secure,value=on \
      -drive if=pflash,format=qcow2,readonly=on,file="$code" \
      -drive if=pflash,format=qcow2,file="$vm/vars.qcow2" \
      -drive file="$vm/disk.qcow2",if=virtio,format=qcow2,discard=unmap \
      "${gpu[@]}" \
      -vnc "$listen:$vnc,password=on" \
      -object secret,id=spicepw,file="$pass" \
      -spice "port=$spice,addr=$listen,password-secret=spicepw" \
      -device virtio-tablet-pci -device virtio-keyboard-pci \
      -netdev "user,id=n0,hostfwd=tcp:127.0.0.1:$ssh_port-:22" \
      -device virtio-net-pci,netdev=n0 \
      -monitor unix:"$vm/monitor.sock",server,nowait \
      -serial file:"$vm/serial.log"
  for _ in $(seq 20); do [ -S "$vm/monitor.sock" ] && break; sleep 0.5; done
  # VNC passwords are at most 8 characters.
  monitor "set_password vnc $(head -c 8 "$pass")"
  echo "started $name: VNC $listen:$((5900 + vnc)), SPICE $listen:$spice, SSH 127.0.0.1:$ssh_port"
}

# monitor CMD: one command to the QEMU monitor.
monitor() {
  python3 - "$vm/monitor.sock" "$1" <<'PY'
import socket, sys, time
s = socket.socket(socket.AF_UNIX); s.connect(sys.argv[1]); time.sleep(0.3); s.recv(4096)
s.sendall((sys.argv[2] + "\n").encode()); time.sleep(1); print(s.recv(65536).decode(errors="replace")); s.close()
PY
}

case "${1:-}" in
  create) create "$2" "$3" ;;
  start) start "${2:-gl}" ;;
  stop) monitor system_powerdown || true ;;
  monitor) monitor "$2" ;;
  shot) monitor "screendump $2" ;;
  *) sed -n '2,19p' "$0"; exit 2 ;;
esac
