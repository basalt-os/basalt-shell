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
# The login screen (tuigreet comes with it, as the text fallback).
Requires:       basalt-greeter = %{version}-%{release}
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
# Voice works out of the box: the speech-to-text program and the consented
# model downloads (the desktop offers the speech model for the person's
# language, and the assistant's local model when a skill needs it).
Requires:       basalt-voice
Requires:       basalt-models
Recommends:     basalt-llm
Recommends:     basalt-llm-selinux
Requires(post): systemd

%description -n basalt-desktop
The Basalt OS desktop edition on top of the server system: the Basalt
shell on SwayFX, the greetd login screen (basalt-greeter) starting it, portals,
PipeWire, fonts and themes so GTK and Qt apps follow the shell, and the
default apps (foot, Files, Text Editor, Firefox). The graphical target is
set by the installer.

%package -n basalt-greeter
Summary:        Basalt OS login screen: a graphical greetd greeter
BuildArch:      noarch
# Themes and wallpapers come from the shell.
Requires:       %{name} = %{version}-%{release}
Requires:       (basalt-greeter-selinux = %{version}-%{release} if selinux-policy-%{selinuxtype})
Requires:       greetd
Requires:       quickshell
Requires:       sway
# The text login, when the graphical one cannot run.
Requires:       tuigreet
Requires:       util-linux
Requires:       rsms-inter-fonts
Requires:       adwaita-cursor-theme
Recommends:     NetworkManager
Recommends:     upower
Recommends:     accountsservice
Requires(post): systemd

%description -n basalt-greeter
The login screen of the Basalt OS desktop edition: a greetd greeter drawn
with Quickshell in a locked-down sway, in the Basalt theme. The people of
the computer with their pictures, the password with a Caps Lock warning
and clear messages, the session (Basalt on Sway or on niri), keyboard
layout, network and battery, large text and high contrast, language
(English, Brazilian Portuguese), suspend, restart and power off, and a
fade into the session. It runs confined in its own SELinux domain; when
it cannot start or fails, the text login (tuigreet) takes its place.

%package -n basalt-greeter-selinux
Summary:        SELinux policy for the Basalt OS login screen
BuildArch:      noarch
Requires:       selinux-policy-%{selinuxtype}
Requires(post): selinux-policy-%{selinuxtype}
%{?selinux_requires}

%description -n basalt-greeter-selinux
SELinux module basalt_greeter: the graphical greeter runs in
basalt_greeter_t, entered from greetd's greeter, with access to the
screen, greetd's socket and what it shows, never to password databases or
people's home directories.

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
for m in basalt_shell basalt_greeter; do
    bzip2 -9 -c build/selinux/$m.pp >$m.pp.bz2
    install -Dpm 0644 $m.pp.bz2 %{buildroot}%{_datadir}/selinux/packages/%{selinuxtype}/$m.pp.bz2
    install -Dpm 0644 selinux/$m.if %{buildroot}%{_datadir}/selinux/devel/include/distributed/$m.if
done

%post
# The voice service starts with every person's session (user preset).
%systemd_user_post basalt-voice.service

%preun
%systemd_user_preun basalt-voice.service

# Upgrades from versions without the user preset: enable the voice
# service for every person once.
%triggerun -- basalt-shell < 0.6.0
systemctl --no-reload --global preset basalt-voice.service >/dev/null 2>&1 || :

%post -n basalt-greeter
systemd-tmpfiles --create %{_tmpfilesdir}/basalt-greeter.conf >/dev/null 2>&1 || :

%pre -n basalt-greeter-selinux
%selinux_relabel_pre -s %{selinuxtype}

%post -n basalt-greeter-selinux
%selinux_modules_install -s %{selinuxtype} %{_datadir}/selinux/packages/%{selinuxtype}/basalt_greeter.pp.bz2

%postun -n basalt-greeter-selinux
if [ $1 -eq 0 ]; then
    %selinux_modules_uninstall -s %{selinuxtype} basalt_greeter
fi

%posttrans -n basalt-greeter-selinux
%selinux_relabel_post -s %{selinuxtype}
if [ -x %{_sbindir}/selinuxenabled ] && %{_sbindir}/selinuxenabled; then
    for p in %{_libexecdir}/basalt-greeter /run/basalt-greeter %{_localstatedir}/cache/basalt-greeter; do
        [ -e "$p" ] && %{_sbindir}/restorecon -R "$p" >/dev/null 2>&1 || :
    done
fi

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
# The programs the voice and skill domains may run get their types from
# this module. The relabel above follows the /usr/lib paths of the file
# contexts and misses the same files under /usr/lib64 (whisper-cli of
# basalt-voice, Chromium), and a package installed in the same transaction
# before the module was loaded keeps the old label: push to talk then
# fails with "permission denied". Label them here.
if [ -x %{_sbindir}/selinuxenabled ] && %{_sbindir}/selinuxenabled; then
    for p in %{_libdir}/basalt-voice %{_libexecdir}/basalt-voice %{_bindir}/pw-cat %{_bindir}/pdftotext %{_bindir}/pdfinfo %{_libdir}/chromium-browser; do
        [ -e "$p" ] && %{_sbindir}/restorecon -R "$p" >/dev/null 2>&1 || :
    done
fi

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
%{_userpresetdir}/80-basalt-shell.preset
%dir %{_sysconfdir}/basalt
%config(noreplace) %{_sysconfdir}/basalt/voice.conf
%config(noreplace) %{_sysconfdir}/basalt/desktop-models.conf
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

%files -n basalt-greeter
%license LICENSE
%doc docs/greeter.md
%{_datadir}/basalt-greeter/
%dir %{_libexecdir}/basalt-greeter
%{_libexecdir}/basalt-greeter/greeter-session
%{_libexecdir}/basalt-greeter/basalt-greeter
%{_libexecdir}/basalt-greeter/greeter-ui
%dir %{_sysconfdir}/basalt
%config(noreplace) %{_sysconfdir}/basalt/greeter.conf
%{_tmpfilesdir}/basalt-greeter.conf

%files -n basalt-greeter-selinux
%{_datadir}/selinux/packages/%{selinuxtype}/basalt_greeter.pp.bz2
%{_datadir}/selinux/devel/include/distributed/basalt_greeter.if

%files selinux
%{_datadir}/selinux/packages/%{selinuxtype}/basalt_shell.pp.bz2
%{_datadir}/selinux/devel/include/distributed/basalt_shell.if

%changelog
* Tue Oct 06 2026 Basalt OS developers - 0.8.0-1
- The approval gate (Basalt OS basalt-gate, ADR 0020 phase 2): where the
  gate decides the shell's proposals, the daemon asks it for the person
  or the agent, the sheet, the command bar and the voice card send the
  person's decision from the shell UI straight to the gate (the desktop
  decider), and the daemon claims and runs only what the gate allowed;
  "Approve and remember" where the gate offers it; the activity log names
  the rule or the person that decided. Elsewhere the shell decides as
  before and tells the gate (shadow mode). Assistant proposals can be
  queued in the gate and approved there (assistant-read submit). Without
  a gate nothing changes. Vendored gate client (internal/gateclient,
  MIT OR Apache-2.0); the SELinux module connects the daemon and the UI
  to the gate when its policy is loaded.
- Skill grants, model downloads and consent requests go through the gate
  where it decides them: a grant's approval becomes a rule that ends with
  the grant (revoking removes it), Download is the person's approval of a
  model.download request with the card's consent as its preview, and the
  typed actions knowledge.fetch and remote.consent carry the consent text
  for the assistant loop.

* Tue Oct 06 2026 Basalt OS developers - 0.7.0-1
- basalt-greeter: the graphical login screen of the desktop edition
  (greetd greeter in Quickshell, run by a locked-down sway as the greeter
  user): blurred Basalt wallpaper, clock and date, the people of the
  computer with their pictures or initials and "Other user", the password
  with show and hide, a Caps Lock warning (keyboard LEDs, else the typed
  letters), plain messages for a wrong password, a locked account and
  PAM's own notices, the session (Basalt on Sway, Basalt on niri and the
  other installed sessions), keyboard layout, network and battery, large
  text and high contrast, English and Brazilian Portuguese, suspend,
  restart and power off, and a fade into the session. It remembers the
  last person and each person's session, never a password.
- basalt-greeter-selinux: the greeter runs confined in basalt_greeter_t
  (entered from greetd's xdm_t); no password database, no home
  directories, no session bus.
- The text login (tuigreet) takes over when there is no display device,
  when the graphical greeter fails or never draws its screen (watchdog),
  after two failures in one boot, or with GREETER=text in
  /etc/basalt/greeter.conf.
- basalt-desktop: greetd starts basalt-greeter instead of tuigreet.

* Tue Oct 06 2026 Basalt OS developers - 0.6.3-1
- Every window can be closed with the mouse. foot draws its own title
  bar with buttons ([csd] preferred=client in Basalt's foot settings,
  in the theme's colors, the title in GTK's header size and weight)
  instead of sway's bar without buttons; GTK 3 windows without a header
  bar do the same (the session sets GTK_CSD=1). Apps show the buttons
  the compositor honors: close on sway, maximize and close on niri.
- sway title bars (X11 apps, tiled windows): a middle click closes the
  window, a right click opens the window menu as before; clicks inside
  apps are untouched and a left click still drags.
- Panel window list: the entry under the pointer shows a close button;
  entries stop before the clock instead of running under it.
- Window menu: in English and Brazilian Portuguese; it opens under the
  window's title bar where the window is now (sway sends no event when a
  floating window is dragged, so it used to open where the window had
  been).
- docs/design.md: who draws title bars, and why the shell does not draw
  buttons over sway's own.

* Tue Oct 06 2026 Basalt OS developers - 0.6.2-1
- Push to talk: each person chooses Hold to talk (the default, as
  before) or Press to start and stop in Settings, Voice and assistant
  (push_to_talk in voice-and-assistant.conf), applied at the next press.
  In press to start and stop, Super+V or the panel button starts
  listening, the next press sends, Escape or the card's Cancel drops the
  words, and it also ends at the hold limit (30 s) and after a silence
  once the person spoke (auto_stop_silence, 2 s by default, 0 never).
  The voice card says "Listening, press Super+V again to stop" and shows
  the mode (English and Brazilian Portuguese). niri's Super+V goes
  through the same code path as a press with no release to follow. sway
  gets the binding mode basalt-voice (Escape) while it listens.
  basalt-voiced reports whether speech was heard and the silence since.
- Voice card: holding Super+V right after an utterance that heard
  nothing no longer shows "Voice error" over the listening hints (a new
  press clears the last error or note); nothing understood is now the
  note "I did not catch that. Try again." in the person's language, not
  an error; red is only for errors. Routing is a pure rule with tests:
  nothing focused, or a field that lost focus, goes to the assistant.
- Power menu: a power button at the right end of the panel and in
  quick settings, and Super+Shift+E, open Lock screen, Log out, Suspend,
  Restart and Power off (English and Brazilian Portuguese, keyboard
  navigable). Log out, Restart and Power off count down 60 s and then go
  ahead, with Cancel, and list the open apps. Through logind
  (systemctl, loginctl) with the system's polkit rules. Typed or spoken
  requests ("restart the computer", "desligar", "sair da sessão",
  "bloquear a tela") become the action session.power as a proposal the
  person confirms; agents can neither propose nor call it (person-only
  actions are no longer listed as MCP tools). swayidle also locks on
  logind's lock request.
- Voice: the answer to the last utterance no longer closes the card of
  a new one started meanwhile (and is not spoken over it), and the
  spoken answer uses its own connection to the voice service, so a press
  made while an answer is being synthesized opens the microphone at
  once. Both found in the 0.6.2 lab run.

* Tue Oct 06 2026 Basalt OS developers - 0.6.1-1
- Settings, Additional drivers: with a system assistant older than
  basalt drivers (0.9.0 and before), or while the basalt-nonfree
  repository is not published, the page says that driver installation
  is coming soon (English and Brazilian Portuguese) instead of the error
  "basalt: unknown command drivers", and never offers Install. Install
  shows only when the assistant reports that the repository's
  definition can be installed (nonfree_available).

* Tue Oct 06 2026 Basalt OS developers - 0.6.0-1
- basalt-shell-selinux labels the voice and skill programs under
  /usr/lib64 (whisper-cli, Chromium) after its module is loaded: when
  basalt-voice was installed first, whisper-cli kept lib_t and push to
  talk failed with "permission denied" (found in the zero-setup lab).
- Zero setup for voice and the local model (Basalt OS rule: no feature
  asks the person to run a command):
- The voice service is enabled for every person by a user preset and
  starts with the session; push to talk starts it when its socket is
  missing, and the card never shows a socket error.
- Push to talk without a speech model for the person's language offers
  the download on the voice card (Download, Not now), with its size and
  where it comes from: ggml-base.en for English, ggml-base-q5_1 with
  Silero VAD for any other language. The download (basalt-models:
  polkit, a confined system service, pinned URLs and SHA-256) shows its
  progress on the card, waits for the network and starts again by
  itself, says when a file was damaged, and a notification says when
  voice is ready; push to talk works at once, without logging out.
- The speech language follows the person's session language when
  neither they nor the administrator set one (voice.conf ships
  BASALT_VOICE_LANGUAGE empty).
- A skill that needs the assistant's local model when none is
  downloaded offers it on a card (the model basalt-llm picks for this
  computer, its size); after the download the model service runs it and
  the command bar and skills use it at once.
- Settings, Voice and assistant: download and remove speech models and
  the local model with one click, downloads in progress at the top.
- Consents are written to the activity log and to basalt-ledger.
- basalt-desktop requires basalt-voice and basalt-models and recommends
  basalt-llm.

* Tue Oct 06 2026 Basalt OS developers - 0.5.1-1
- basalt-session: in a virtual machine (virtio-gpu, QXL, bochs, Cirrus,
  VMware SVGA, VirtualBox, Hyper-V, or the firmware framebuffer under a
  hypervisor) sway draws the pointer itself (WLR_NO_HARDWARE_CURSORS=1,
  unless already set): on virtio-gpu the cursor plane's image reached the
  viewer 41 rows below its hotspot, so the pointer seen in virt-manager
  was not where clicks went and the top bar could not be reached. The
  decision is logged to the journal (basalt-session).
- basalt-vm-cursor (libexec): before such a session starts, gives the
  hypervisor an empty, hidden cursor, so the viewer does not draw its own
  pointer or one left by an earlier session next to the session's.

* Mon Oct 05 2026 Basalt OS developers - 0.5.0-1
- Settings, Additional drivers: the graphics hardware, the NVIDIA driver
  of Basalt OS's basalt-nonfree repository for Turing and newer GPUs (what
  changes, the license to accept, PRIME offload on laptops with two GPUs,
  the datacenter notice for GeForce), installed through the system
  assistant's driver.install proposal and its confirmation; after a failed
  first start, why and the rollback to the snapshot from before. GPUs of
  the 580 legacy branch get an explanation (no package). English and
  Brazilian Portuguese.
- basalt-session starts sway and SwayFX with --unsupported-gpu while the
  NVIDIA kernel module is loaded.
- The read helper accepts the Additional drivers requests (report,
  license, storing the install or rollback proposal).

* Mon Oct 05 2026 Basalt OS developers - 0.4.1-1
- basalt-session allows wlroots' software renderer
  (WLR_RENDERER_ALLOW_SOFTWARE=1): SwayFX needs GLES, so on machines and
  VMs without a GPU the session exited at once and the login screen came
  back; a hardware GPU is still used when present.

* Sun Oct 04 2026 Basalt OS developers - 0.4.0-1
- Voice: push to talk (basalt-voiced: whisper.cpp with Silero VAD and
  Piper, no network, nothing kept); off while the screen is locked.
- Read-only skills: find files, read and summarize e-mail and web pages,
  in confined workers with per-job network sessions and consent grants
  that expire; content is data (guard against prompt injection).
- Acting skills, always previewed and confirmed in the shell: dictation
  into the focused text field (input method, no synthetic keys), reply
  to an e-mail (draft edited, sent only after Send), move and rename
  files inside a granted folder (no delete, no replace, undo).
- Records of acting steps go to basalt-ledger with their exact preview.
- SELinux 0.4.0: the voice and skill domains run only their own tools
  (no shell, no setuid helper); new domains for sending and moving.
- Speech recognition is told the names of contacts and mail senders.

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
