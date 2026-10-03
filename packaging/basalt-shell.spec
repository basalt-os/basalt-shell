# Go binary built with -trimpath; no separate debuginfo for the prototype.
%global debug_package %{nil}

Name:           basalt-shell
Version:        %{basalt_version}
Release:        1%{?dist}
Summary:        Basalt OS desktop shell: AI-coordinated, themeable, compositor-agnostic
License:        Apache-2.0 AND CC-BY-SA-4.0
URL:            https://github.com/openbasalt/basalt-shell
Source0:        basalt-shell-%{version}.tar.gz

BuildRequires:  golang >= 1.24
BuildRequires:  make
BuildRequires:  systemd-rpm-macros

Requires:       quickshell
Requires:       (swayfx or sway or niri)
Requires:       xdg-desktop-portal
Requires:       xdg-desktop-portal-gtk
Requires:       polkit
Requires:       glib2
Recommends:     swayfx
Recommends:     niri
Recommends:     xwayland-satellite
Recommends:     xdg-desktop-portal-wlr
Recommends:     xdg-desktop-portal-gnome
Recommends:     adw-gtk3-theme
Recommends:     qt6ct
Recommends:     rsms-inter-fonts
Recommends:     jetbrains-mono-fonts
Recommends:     swayidle
Recommends:     swaylock
Recommends:     wl-clipboard
Recommends:     cliphist
Recommends:     grim
Recommends:     slurp
Recommends:     foot
Recommends:     gnome-keyring

%description
The Basalt OS desktop shell: panel, launcher, notifications, quick
settings, an "Ask the system" command bar and an activity feed, drawn with
Quickshell from one set of design tokens that end users can change live.
A daemon exposes the desktop as typed actions over a local socket and an
MCP server; every change an agent asks for waits for the person's
confirmation and is audited. Works on sway / SwayFX and niri.

%prep
%autosetup

%build
export GOFLAGS="-trimpath -mod=readonly"
make build VERSION=%{version}

%install
make install DESTDIR=%{buildroot} PREFIX=%{_prefix} SYSCONFDIR=%{_sysconfdir} LIBEXECDIR=%{_libexecdir}

%files
%license LICENSE LICENSE-artwork
%doc README.md docs/design.md
%{_bindir}/basalt-shell
%{_bindir}/basalt-shell-ui
%{_bindir}/basalt-session
%{_bindir}/basalt-lock
%{_bindir}/basalt-session-init
%{_datadir}/basalt-shell/
%{_datadir}/wayland-sessions/basalt-sway.desktop
%{_datadir}/wayland-sessions/basalt-niri.desktop
%config(noreplace) %{_sysconfdir}/xdg/xdg-desktop-portal/sway-portals.conf
%config(noreplace) %{_sysconfdir}/xdg/xdg-desktop-portal/niri-portals.conf
%{_userunitdir}/basalt-session.target
%{_libexecdir}/basalt-shell/
%{_datadir}/polkit-1/actions/org.openbasalt.shell.policy
%{_datadir}/polkit-1/rules.d/50-basalt-shell.rules

%changelog
* Sat Oct 03 2026 Basalt OS developers - 0.1.0-1
- Prototype.
