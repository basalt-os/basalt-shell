#!/bin/bash
# Install the Basalt shell on Fedora (44 or newer) with its dependencies.
#
#   scripts/install.sh            dependencies (dnf; SwayFX from its COPR), then
#                                 `make install` to /usr/local; adds the sessions
#                                 "Basalt (SwayFX)" and "Basalt (niri)" to the login screen
#   scripts/install.sh --no-swayfx   plain sway from Fedora instead of SwayFX (no COPR)
#   scripts/install.sh --uninstall
#
# Afterwards: log out and pick a Basalt session, or try it in a window of
# your current desktop with scripts/try-nested.sh.
set -euo pipefail
cd "$(dirname "$0")/.."
swayfx=1
case "${1:-}" in
  --no-swayfx) swayfx=0 ;;
  --uninstall) sudo make uninstall PREFIX=/usr/local; exit 0 ;;
  "") ;;
  *) sed -n '2,12p' "$0"; exit 2 ;;
esac
. /etc/os-release
[ "${ID:-}" = fedora ] || [[ " ${ID_LIKE:-} " == *" fedora "* ]] || echo "warning: not Fedora; install the packages listed below yourself" >&2

pkgs=(niri quickshell xwayland-satellite xorg-x11-server-Xwayland
  xdg-desktop-portal xdg-desktop-portal-gtk xdg-desktop-portal-wlr xdg-desktop-portal-gnome
  pipewire wireplumber upower polkit gnome-keyring gcr
  foot adw-gtk3-theme adwaita-icon-theme adwaita-cursor-theme qt6ct qt6-qtwayland
  rsms-inter-fonts jetbrains-mono-fonts google-noto-sans-fonts
  swayidle swaylock wl-clipboard cliphist grim slurp wtype brightnessctl xdg-utils dbus-daemon golang make)
if [ "$swayfx" = 1 ]; then
  sudo dnf -y install dnf-plugins-core
  sudo dnf -y copr enable swayfx/swayfx
  if rpm -q sway >/dev/null 2>&1; then
    sudo dnf -y swap sway swayfx
  else
    pkgs+=(swayfx)
  fi
else
  pkgs+=(sway)
fi
sudo dnf -y install "${pkgs[@]}"
make build
sudo make install PREFIX=/usr/local
echo
echo "Installed. Log out and choose \"Basalt (SwayFX)\" or \"Basalt (niri)\", or run scripts/try-nested.sh sway|niri."
