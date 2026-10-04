# Go binaries built with -trimpath; no separate debuginfo for the prototype.
%global debug_package %{nil}
%global selinuxtype targeted

Name:           basalt-shell
Version:        %{basalt_version}
Release:        1%{?dist}
Summary:        Basalt OS desktop shell: AI-coordinated, themeable, compositor-agnostic
License:        Apache-2.0 AND CC-BY-SA-4.0
URL:            https://github.com/basalt-os/basalt-shell
Source0:        basalt-shell-%{version}.tar.gz

BuildRequires:  golang >= 1.24
BuildRequires:  make
BuildRequires:  systemd-rpm-macros
BuildRequires:  selinux-policy-devel
# The agent family's base module (its interface file).
BuildRequires:  basalt-agent-selinux
BuildRequires:  bzip2

Requires:       quickshell
Requires:       sway
Requires:       xdg-desktop-portal
Requires:       xdg-desktop-portal-gtk
Requires:       polkit
# gsettings (application appearance).
Requires:       /usr/bin/gsettings
Requires:       (%{name}-selinux = %{version}-%{release} if selinux-policy-%{selinuxtype})
# Agents' last resort (screen capture, virtual keyboard) and the headless
# session's VNC view.
Requires:       grim
Requires:       wtype
Recommends:     wayvnc
# Qt 5 and Qt 6 apps follow the theme; the keyring unlocks at login
# (greetd's PAM stack already lists pam_gnome_keyring as optional).
Requires:       qt6ct
Requires:       qt5ct
Requires:       gnome-keyring
Requires:       gnome-keyring-pam
# The read-only skills: PDF text (poppler), the browse skill (headless
# Chromium). Speech: basalt-voice (whisper.cpp) and the PipeWire tools.
Requires:       poppler-utils
Requires:       pipewire-utils
Recommends:     chromium
Recommends:     basalt-voice
Recommends:     xdg-desktop-portal-wlr
Recommends:     xdg-desktop-portal-gnome
Recommends:     adw-gtk3-theme
# Qt title bars matching GTK 4 and libadwaita (client-side decorations).
Recommends:     qt6-qtwayland-adwaita-decoration
Recommends:     qadwaitadecorations-qt5
Recommends:     rsms-inter-fonts
Recommends:     jetbrains-mono-fonts
Recommends:     swayidle
Recommends:     swaylock
Recommends:     wl-clipboard
Recommends:     cliphist
Recommends:     slurp
Recommends:     foot
Recommends:     dbus-daemon

%description
The Basalt OS desktop shell: panel, launcher, notifications, quick
settings, an "Ask the system" command bar and an activity feed, drawn with
Quickshell from one set of design tokens that end users can change live.
A daemon exposes the desktop as typed actions over a local socket and an
MCP server; every change an agent asks for waits for the person's
confirmation and is audited. Runs on sway (the default, also headless
for agents) and niri.

%package niri
Summary:        Basalt shell session on niri (optional)
Requires:       %{name} = %{version}-%{release}
BuildArch:      noarch
Requires:       niri
Requires:       xwayland-satellite

%description niri
The Basalt shell on niri, a scrollable-tiling Wayland compositor: the
"Basalt (niri)" login session, its compositor configuration and portal
settings. Optional: the default session is Basalt on sway (SwayFX).

%package -n basalt-desktop
Summary:        Basalt OS desktop edition: the Basalt session, login screen and default apps
BuildArch:      noarch
Requires:       %{name} = %{version}-%{release}
Requires:       (%{name}-selinux = %{version}-%{release} if selinux-policy-%{selinuxtype})
# The session's compositor: SwayFX (Provides sway) from the Basalt repository.
Requires:       swayfx
Requires:       greetd
Requires:       tuigreet
Requires:       xorg-x11-server-Xwayland
Requires:       xdg-desktop-portal-wlr
Requires:       xdg-desktop-portal-gtk
Requires:       pipewire
Requires:       pipewire-pulseaudio
Requires:       wireplumber
Requires:       mesa-dri-drivers
Requires:       mesa-vulkan-drivers
Requires:       upower
Requires:       NetworkManager
Requires:       xdg-user-dirs
Requires:       xdg-utils
Requires:       adwaita-icon-theme
Requires:       adwaita-cursor-theme
Requires:       adw-gtk3-theme
Requires:       qt6-qtwayland
Requires:       qt6-qtwayland-adwaita-decoration
Requires:       qadwaitadecorations-qt5
Requires:       rsms-inter-fonts
Requires:       jetbrains-mono-fonts
Requires:       google-noto-sans-fonts
Requires:       google-noto-emoji-fonts
Requires:       swayidle
Requires:       swaylock
Requires:       wl-clipboard
# Default apps: terminal, file manager, text editor, browser.
Requires:       foot
Requires:       nautilus
Requires:       gnome-text-editor
Requires:       firefox
Requires(post): systemd

%description -n basalt-desktop
The Basalt OS desktop edition on top of the server system: the Basalt
shell on SwayFX, the greetd login screen (tuigreet) starting it, portals,
PipeWire, fonts and themes so GTK and Qt apps follow the shell, and the
default apps (foot, Files, Text Editor, Firefox). The graphical target is
set by the installer.

%package selinux
Summary:        SELinux policy for the Basalt desktop shell and its agent clients
BuildArch:      noarch
Requires:       selinux-policy-%{selinuxtype}
Requires(post): selinux-policy-%{selinuxtype}
# basalt_agent_base: the agent family (attribute, baseline, neverallow).
Requires:       basalt-agent-selinux
Requires(post): basalt-agent-selinux
%{?selinux_requires}

%description selinux
SELinux module basalt_shell: the daemon (basalt_shell_t), the shell UI
(basalt_shell_ui_t, the only domain that may confirm) and the shell's MCP
server and command line, confined in basalt_agent_mcp_t, a member of the
Basalt agent family (basalt_agent_base, from basalt-agent-selinux).

%prep
%autosetup

%build
# PIE, like the other Basalt OS Go packages (rpmlint: no static binaries).
export GOFLAGS="-trimpath -mod=readonly -buildmode=pie"
make build VERSION=%{version}
make selinux

%install
make install DESTDIR=%{buildroot} PREFIX=%{_prefix} SYSCONFDIR=%{_sysconfdir} LIBEXECDIR=%{_libexecdir}
install -Dpm 0644 config/desktop/greetd.toml %{buildroot}%{_sysconfdir}/basalt/greetd.toml
install -Dpm 0644 config/desktop/greetd-basalt.conf %{buildroot}%{_unitdir}/greetd.service.d/50-basalt.conf
install -Dpm 0644 config/desktop/80-basalt-desktop.preset %{buildroot}%{_presetdir}/80-basalt-desktop.preset
for m in basalt_shell; do
    bzip2 -9 -c build/selinux/$m.pp >$m.pp.bz2
    install -Dpm 0644 $m.pp.bz2 %{buildroot}%{_datadir}/selinux/packages/%{selinuxtype}/$m.pp.bz2
    install -Dpm 0644 selinux/$m.if %{buildroot}%{_datadir}/selinux/devel/include/distributed/$m.if
done

%post -n basalt-desktop
# greetd may be installed in the same transaction, before this preset.
if [ $1 -eq 1 ]; then
    systemctl --no-reload preset greetd.service >/dev/null 2>&1 || :
fi

%pre selinux
%selinux_relabel_pre -s %{selinuxtype}

%post selinux
%selinux_modules_install -s %{selinuxtype} %{_datadir}/selinux/packages/%{selinuxtype}/basalt_shell.pp.bz2

%postun selinux
if [ $1 -eq 0 ]; then
    %selinux_modules_uninstall -s %{selinuxtype} basalt_shell
fi

%posttrans selinux
%selinux_relabel_post -s %{selinuxtype}

%files
%license LICENSE LICENSE-artwork NOTICE
%doc README.md docs/design.md docs/voice.md
%{_bindir}/basalt-shell
%{_bindir}/basalt-shelld
%{_bindir}/basalt-voiced
%{_bindir}/basalt-shell-ui
%{_bindir}/basalt-session
%{_bindir}/basalt-lock
%{_bindir}/basalt-session-init
%{_datadir}/basalt-shell/
%exclude %{_datadir}/basalt-shell/niri/
%{_datadir}/wayland-sessions/basalt-sway.desktop
%config(noreplace) %{_sysconfdir}/xdg/xdg-desktop-portal/sway-portals.conf
%{_userunitdir}/basalt-session.target
%{_userunitdir}/basalt-headless.service
%{_userunitdir}/basalt-voice.service
%dir %{_sysconfdir}/basalt
%config(noreplace) %{_sysconfdir}/basalt/voice.conf
%{_libexecdir}/basalt-shell/
%{_datadir}/polkit-1/actions/org.openbasalt.shell.policy
%{_datadir}/polkit-1/rules.d/50-basalt-shell.rules

%files niri
%{_datadir}/basalt-shell/niri/
%{_datadir}/wayland-sessions/basalt-niri.desktop
%config(noreplace) %{_sysconfdir}/xdg/xdg-desktop-portal/niri-portals.conf

%files -n basalt-desktop
%dir %{_sysconfdir}/basalt
%config(noreplace) %{_sysconfdir}/basalt/greetd.toml
%dir %{_unitdir}/greetd.service.d
%{_unitdir}/greetd.service.d/50-basalt.conf
%{_presetdir}/80-basalt-desktop.preset

%files selinux
%{_datadir}/selinux/packages/%{selinuxtype}/basalt_shell.pp.bz2
%{_datadir}/selinux/devel/include/distributed/basalt_shell.if

%changelog
* Sun Oct 04 2026 Basalt OS developers - 0.3.1-1
- basalt-desktop: the desktop edition's session, login screen (greetd)
  and default apps.
- niri moves to the optional basalt-shell-niri subpackage; the default
  session is Basalt on sway (SwayFX when installed).
- SwayFX: shadows on client-decorated windows, dimming reaches open
  windows; effects (shadows, blur, dimming) off on weak hardware
  (software rendering, few CPUs, little memory, headless).
- Window states stay in sync with the panel after an action.
- Apps started from the shell run as systemd user services in the
  person's SELinux domain, no longer in the daemon's basalt_shell_t.
- FeatherPad's text area follows the light or dark mode.

* Sun Oct 04 2026 Basalt OS developers - 0.3.0-1
- Window decorations that follow the theme on sway and niri; Qt windows
  use client-side decorations when the Adwaita plugin is installed.
- Window states (normal, minimized, maximized, left, right) as a typed
  action and MCP tool; panel window list and window menu.

* Sun Oct 04 2026 Basalt OS developers - 0.2.0-1
- sway (Fedora) is the default session; SwayFX dropped; niri optional.
- Headless session for agents; screen capture, virtual input and
  control sessions for agents, behind confirmation and audit.
- SELinux: only the shell UI's domain confirms; agent clients confined.
- Command bar uses the local model when available; qt5ct; keyring PAM.

* Sat Oct 03 2026 Basalt OS developers - 0.1.0-1
- Prototype.
