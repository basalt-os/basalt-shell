#!/usr/bin/env python3
"""Add the desktop profile (basalt.profile=desktop) to the Basalt OS server
kickstart. Reads the kickstart on stdin, writes the patched one on stdout.

The server default is unchanged: every addition is guarded by
BASALT_PROFILE=desktop. Used by lab/build-iso.sh; a proposal for the
basalt-os repository, not applied there.
"""
import sys

s = sys.stdin.read()


def rep(old, new):
    global s
    if old not in s:
        sys.exit("make-desktop-ks: anchor not found: " + old[:70])
    s = s.replace(old, new, 1)


rep('''#       basalt.profile=minimal          no hardware firmware, CPU microcode or
#                                       fwupd, for virtual machines (default: standard)''',
    '''#       basalt.profile=minimal          no hardware firmware, CPU microcode or
#                                       fwupd, for virtual machines (default: standard)
#       basalt.profile=desktop          standard plus the desktop edition: the
#                                       Basalt shell on sway (and niri),
#                                       portals, PipeWire, greetd (traditional
#                                       Fedora; the server default is unchanged)''')
rep('BASALT_RECOVERY_KEY_PAUSE, BASALT_REPO_URL) and /basalt/site.ks',
    'BASALT_RECOVERY_KEY_PAUSE, BASALT_REPO_URL, BASALT_DESKTOP_AUTOLOGIN) and /basalt/site.ks')
rep('''BASALT_LOCKDOWN=1
if [ -r''', '''BASALT_LOCKDOWN=1
BASALT_DESKTOP_AUTOLOGIN=
if [ -r''')
rep('case "$BASALT_PROFILE" in standard|minimal) ;;',
    'case "$BASALT_PROFILE" in standard|minimal|desktop) ;;')
rep('''  echo "Basalt OS packages not found on the media and BASALT_REPO_URL is not set" >&2
  exit 1
fi
''', '''  echo "Basalt OS packages not found on the media and BASALT_REPO_URL is not set" >&2
  exit 1
fi
''')

DESKTOP_PKGS = """basalt-shell
basalt-shell-selinux
basalt-agent-selinux
basalt-assistant
basalt-assistant-selinux
sway
niri
quickshell
xwayland-satellite
xorg-x11-server-Xwayland
xdg-desktop-portal
xdg-desktop-portal-gtk
xdg-desktop-portal-wlr
xdg-desktop-portal-gnome
pipewire
pipewire-pulseaudio
wireplumber
upower
polkit
gnome-keyring
gnome-keyring-pam
gcr
greetd
tuigreet
NetworkManager
network-manager-applet
mesa-dri-drivers
mesa-vulkan-drivers
foot
nautilus
gnome-text-editor
adw-gtk3-theme
adwaita-icon-theme
adwaita-cursor-theme
qt6ct
qt5ct
qt6-qtwayland
rsms-inter-fonts
jetbrains-mono-fonts
google-noto-sans-fonts
google-noto-emoji-fonts
swayidle
swaylock
wl-clipboard
cliphist
grim
slurp
wf-recorder
wtype
wayvnc
brightnessctl
xdg-utils
xdg-user-dirs
flatpak
"""

rep('''  if [ "$BASALT_PROFILE" = minimal ]; then
    printf '%s\\n' -linux-firmware -linux-firmware-whence '-*-firmware' -microcode_ctl '-fwupd*' -flashrom
  fi
  echo "%end"''', '''  if [ "$BASALT_PROFILE" = minimal ]; then
    printf '%s\\n' -linux-firmware -linux-firmware-whence '-*-firmware' -microcode_ctl '-fwupd*' -flashrom
  fi
  if [ "$BASALT_PROFILE" = desktop ]; then
    cat <<'PKG'
''' + DESKTOP_PKGS + '''PKG
  fi
  echo "%end"''')
rep('''BASALT_TANG_THP=$BASALT_TANG_THP
EOF''', '''BASALT_TANG_THP=$BASALT_TANG_THP
BASALT_PROFILE=$BASALT_PROFILE
BASALT_DESKTOP_AUTOLOGIN=$BASALT_DESKTOP_AUTOLOGIN
EOF''')
rep('# --- boot splash: Basalt theme; text prompt on the serial console --------------',
    '''# --- desktop edition: graphical login through greetd ------------------------
# tuigreet on the first virtual terminal; with BASALT_DESKTOP_AUTOLOGIN=USER
# that user's first login starts the Basalt session directly (labs, kiosks).
if [ "${BASALT_PROFILE:-}" = desktop ]; then
  mkdir -p /etc/greetd
  cat >/etc/greetd/config.toml <<'EOF'
[terminal]
vt = 1

[default_session]
command = "tuigreet --time --remember --remember-session --sessions /usr/share/wayland-sessions --cmd 'basalt-session sway'"
user = "greetd"
EOF
  if [ -n "${BASALT_DESKTOP_AUTOLOGIN:-}" ]; then
    printf '\\n[initial_session]\\ncommand = "basalt-session sway"\\nuser = "%s"\\n' "$BASALT_DESKTOP_AUTOLOGIN" >>/etc/greetd/config.toml
  fi
  systemctl enable greetd.service
  systemctl set-default graphical.target
  log "desktop edition: greetd enabled, graphical target"
fi

# --- boot splash: Basalt theme; text prompt on the serial console --------------''')
sys.stdout.write(s)
