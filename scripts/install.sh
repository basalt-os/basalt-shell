#!/bin/bash
# Install the Basalt shell on Fedora (44 or newer) with its dependencies,
# all from Fedora's own repositories.
#
#   scripts/install.sh              dependencies (dnf), then `make install` to
#                                   /usr/local; adds the sessions "Basalt" (sway)
#                                   and "Basalt (niri)" to the login screen
#   scripts/install.sh --uninstall
#
# The SELinux policy (the confirmation boundary, docs/selinux.md) comes with
# the RPMs (basalt-shell-selinux, basalt-agent-selinux); without it the
# shell checks the UI by program name only, and says so in Settings.
# Afterwards: log out and pick a Basalt session, or try it in a window of
# your current desktop with scripts/try-nested.sh.
set -euo pipefail
cd "$(dirname "$0")/.."
case "${1:-}" in
  --uninstall) sudo make uninstall PREFIX=/usr/local; exit 0 ;;
  "") ;;
  *) sed -n '2,14p' "$0"; exit 2 ;;
esac
. /etc/os-release
[ "${ID:-}" = fedora ] || [[ " ${ID_LIKE:-} " == *" fedora "* ]] || echo "warning: not Fedora; install the packages listed below yourself" >&2

pkgs=(sway niri quickshell xwayland-satellite xorg-x11-server-Xwayland
  xdg-desktop-portal xdg-desktop-portal-gtk xdg-desktop-portal-wlr xdg-desktop-portal-gnome
  pipewire wireplumber upower polkit gnome-keyring gnome-keyring-pam gcr
  foot adw-gtk3-theme adwaita-icon-theme adwaita-cursor-theme qt6ct qt5ct qt6-qtwayland
  rsms-inter-fonts jetbrains-mono-fonts google-noto-sans-fonts
  swayidle swaylock wl-clipboard cliphist grim slurp wtype wayvnc brightnessctl xdg-utils dbus-daemon golang make)
sudo dnf -y install "${pkgs[@]}"
make build
sudo make install PREFIX=/usr/local
echo
echo "Installed. Log out and choose \"Basalt\" or \"Basalt (niri)\", or run scripts/try-nested.sh sway|niri."
