# Go binaries built with -trimpath; no separate debuginfo for the prototype.
%global debug_package %{nil}
%global selinuxtype targeted

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
BuildRequires:  selinux-policy-devel
# The agent family's base module (its interface file).
BuildRequires:  basalt-agent-selinux
BuildRequires:  bzip2

Requires:       quickshell
Requires:       sway
Requires:       xdg-desktop-portal
Requires:       xdg-desktop-portal-gtk
Requires:       polkit
Requires:       glib2
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
Recommends:     niri
Recommends:     xwayland-satellite
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
export GOFLAGS="-trimpath -mod=readonly"
make build VERSION=%{version}
make selinux

%install
make install DESTDIR=%{buildroot} PREFIX=%{_prefix} SYSCONFDIR=%{_sysconfdir} LIBEXECDIR=%{_libexecdir}
for m in basalt_shell; do
    bzip2 -9 -c build/selinux/$m.pp >$m.pp.bz2
    install -Dpm 0644 $m.pp.bz2 %{buildroot}%{_datadir}/selinux/packages/%{selinuxtype}/$m.pp.bz2
    install -Dpm 0644 selinux/$m.if %{buildroot}%{_datadir}/selinux/devel/include/distributed/$m.if
done

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
%license LICENSE LICENSE-artwork
%doc README.md docs/design.md
%{_bindir}/basalt-shell
%{_bindir}/basalt-shelld
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
%{_userunitdir}/basalt-headless.service
%{_libexecdir}/basalt-shell/
%{_datadir}/polkit-1/actions/org.openbasalt.shell.policy
%{_datadir}/polkit-1/rules.d/50-basalt-shell.rules

%files selinux
%{_datadir}/selinux/packages/%{selinuxtype}/basalt_shell.pp.bz2
%{_datadir}/selinux/devel/include/distributed/basalt_shell.if

%changelog
* Sun Oct 04 2026 Basalt OS developers - 0.2.0-1
- sway (Fedora) is the default session; SwayFX dropped; niri optional.
- Headless session for agents; screen capture, virtual input and
  control sessions for agents, behind confirmation and audit.
- SELinux: only the shell UI's domain confirms; agent clients confined.
- Command bar uses the local model when available; qt5ct; keyring PAM.

* Sat Oct 03 2026 Basalt OS developers - 0.1.0-1
- Prototype.
