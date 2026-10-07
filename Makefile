# basalt-shell: daemon (Go), shell UI (Quickshell QML), themes, session files.
PREFIX ?= /usr/local
SYSCONFDIR ?= /etc
# polkit only reads /usr/share/polkit-1, and the policy names the helper's path.
POLKITDIR ?= /usr/share/polkit-1
LIBEXECDIR ?= /usr/libexec
DESTDIR ?=
VERSION := $(shell cat VERSION)
GO ?= go
GOFLAGS ?= -trimpath
BUILD := build

.PHONY: all build test test-greeter test-js vet install install-greeter uninstall clean rpm selinux install-selinux help

all: build

help:
	@echo "make build | test | install [PREFIX=/usr/local DESTDIR=] | selinux | install-selinux | rpm | clean"
	@echo "scripts/install.sh --user | --system   install with dependencies (see README)"
	@echo "scripts/try-nested.sh [sway|niri]      run a session in a window of your desktop"

build:
	mkdir -p $(BUILD)
	$(GO) build $(GOFLAGS) -ldflags "-X main.version=$(VERSION)" -o $(BUILD)/basalt-shell ./cmd/basalt-shell
	$(GO) build $(GOFLAGS) -ldflags "-X main.version=$(VERSION)" -o $(BUILD)/basalt-shelld ./cmd/basalt-shelld
	$(GO) build $(GOFLAGS) -o $(BUILD)/basalt-shell-ui-launch ./cmd/basalt-shell-ui-launch
	$(GO) build $(GOFLAGS) -o $(BUILD)/basalt-vm-cursor ./cmd/basalt-vm-cursor
	$(GO) build $(GOFLAGS) -ldflags "-X main.version=$(VERSION)" -o $(BUILD)/basalt-voiced ./cmd/basalt-voiced
	$(GO) build $(GOFLAGS) -o $(BUILD)/basalt-skill ./cmd/basalt-skill

test:
	$(GO) vet ./...
	$(GO) test ./...
	@if command -v node >/dev/null 2>&1; then $(MAKE) test-js; else echo "node not found: JavaScript tests skipped (make test-js)"; fi

# The JavaScript logic with Node's test runner: the greeter's
# (greeter/logic.js) and the lock screen's (shell/lock.js, and the rules of
# shell/Lock.qml that keep the session locked until PAM says yes).
test-js: test-greeter
	node --test shell/tests/

test-greeter:
	node --test greeter/tests/

install:
	test -x $(BUILD)/basalt-shell || $(MAKE) build
	install -Dm755 $(BUILD)/basalt-shell $(DESTDIR)$(PREFIX)/bin/basalt-shell
	install -Dm755 $(BUILD)/basalt-shelld $(DESTDIR)$(PREFIX)/bin/basalt-shelld
	install -Dm755 $(BUILD)/basalt-shell-ui-launch $(DESTDIR)$(PREFIX)/libexec/basalt-shell/basalt-shell-ui-launch
	install -Dm755 $(BUILD)/basalt-vm-cursor $(DESTDIR)$(PREFIX)/libexec/basalt-shell/basalt-vm-cursor
	install -Dm755 $(BUILD)/basalt-voiced $(DESTDIR)$(PREFIX)/bin/basalt-voiced
	# The skills worker, four times: four files, four SELinux types (read
	# content, index documents, send one confirmed e-mail, rename files
	# inside a grant); each job kind runs only in its own program.
	install -Dm755 $(BUILD)/basalt-skill $(DESTDIR)$(PREFIX)/libexec/basalt-shell/basalt-skill
	install -Dm755 $(BUILD)/basalt-skill $(DESTDIR)$(PREFIX)/libexec/basalt-shell/basalt-skill-index
	install -Dm755 $(BUILD)/basalt-skill $(DESTDIR)$(PREFIX)/libexec/basalt-shell/basalt-skill-send
	install -Dm755 $(BUILD)/basalt-skill $(DESTDIR)$(PREFIX)/libexec/basalt-shell/basalt-skill-files
	install -Dm644 config/systemd/basalt-voice.service $(DESTDIR)$(PREFIX)/lib/systemd/user/basalt-voice.service
	install -Dm644 config/systemd/80-basalt-shell.preset $(DESTDIR)$(PREFIX)/lib/systemd/user-preset/80-basalt-shell.preset
	install -Dm644 config/voice/voice.conf $(DESTDIR)$(SYSCONFDIR)/basalt/voice.conf
	install -Dm644 config/voice/desktop-models.conf $(DESTDIR)$(SYSCONFDIR)/basalt/desktop-models.conf
	install -Dm644 config/pam/basalt-lock $(DESTDIR)$(SYSCONFDIR)/pam.d/basalt-lock
	install -Dm755 bin/basalt-shell-ui $(DESTDIR)$(PREFIX)/bin/basalt-shell-ui
	install -Dm755 bin/basalt-session $(DESTDIR)$(PREFIX)/bin/basalt-session
	install -Dm755 bin/basalt-lock $(DESTDIR)$(PREFIX)/bin/basalt-lock
	install -Dm755 bin/basalt-session-init $(DESTDIR)$(PREFIX)/bin/basalt-session-init
	install -d $(DESTDIR)$(PREFIX)/share/basalt-shell/qml/wallpapers $(DESTDIR)$(PREFIX)/share/basalt-shell/themes
	install -m644 shell/*.qml shell/*.js $(DESTDIR)$(PREFIX)/share/basalt-shell/qml/
	install -m644 shell/wallpapers/* $(DESTDIR)$(PREFIX)/share/basalt-shell/qml/wallpapers/
	install -m644 themes/*.json $(DESTDIR)$(PREFIX)/share/basalt-shell/themes/
	install -d $(DESTDIR)$(PREFIX)/share/basalt-shell/locale
	install -m644 locale/*.json $(DESTDIR)$(PREFIX)/share/basalt-shell/locale/
	install -Dm644 config/sway/config $(DESTDIR)$(PREFIX)/share/basalt-shell/sway/config
	install -Dm644 config/niri/config.kdl $(DESTDIR)$(PREFIX)/share/basalt-shell/niri/config.kdl
	install -Dm644 config/niri/basalt-theme.kdl $(DESTDIR)$(PREFIX)/share/basalt-shell/niri/basalt-theme.kdl
	install -Dm644 config/portals/xdpw-config $(DESTDIR)$(PREFIX)/share/basalt-shell/portals/xdpw-config
	install -Dm644 config/sessions/basalt-sway.desktop $(DESTDIR)$(PREFIX)/share/wayland-sessions/basalt-sway.desktop
	install -Dm644 config/sessions/basalt-niri.desktop $(DESTDIR)$(PREFIX)/share/wayland-sessions/basalt-niri.desktop
	install -Dm644 config/portals/sway-portals.conf $(DESTDIR)$(SYSCONFDIR)/xdg/xdg-desktop-portal/sway-portals.conf
	install -Dm644 config/portals/niri-portals.conf $(DESTDIR)$(SYSCONFDIR)/xdg/xdg-desktop-portal/niri-portals.conf
	install -Dm644 config/systemd/basalt-session.target $(DESTDIR)$(PREFIX)/lib/systemd/user/basalt-session.target
	install -Dm644 config/systemd/basalt-headless.service $(DESTDIR)$(PREFIX)/lib/systemd/user/basalt-headless.service
	install -Dm755 libexec/assistant-read $(DESTDIR)$(LIBEXECDIR)/basalt-shell/assistant-read
	install -Dm644 config/polkit/org.openbasalt.shell.policy $(DESTDIR)$(POLKITDIR)/actions/org.openbasalt.shell.policy
	install -Dm644 config/polkit/50-basalt-shell.rules $(DESTDIR)$(POLKITDIR)/rules.d/50-basalt-shell.rules
	$(MAKE) install-greeter

# The login screen (basalt-greeter): QML, catalogs, its sway configuration,
# the launchers greetd runs, its configuration and runtime directories.
install-greeter:
	install -d $(DESTDIR)$(PREFIX)/share/basalt-greeter/qml $(DESTDIR)$(PREFIX)/share/basalt-greeter/locale $(DESTDIR)$(LIBEXECDIR)/basalt-greeter
	install -m644 greeter/*.qml greeter/logic.js $(DESTDIR)$(PREFIX)/share/basalt-greeter/qml/
	install -m644 greeter/locale/*.json $(DESTDIR)$(PREFIX)/share/basalt-greeter/locale/
	install -m644 greeter/sway.conf $(DESTDIR)$(PREFIX)/share/basalt-greeter/sway.conf
	install -m755 greeter/bin/greeter-session greeter/bin/basalt-greeter greeter/bin/greeter-ui $(DESTDIR)$(LIBEXECDIR)/basalt-greeter/
	install -Dm644 config/greeter/greeter.conf $(DESTDIR)$(SYSCONFDIR)/basalt/greeter.conf
	install -Dm644 config/greeter/basalt-greeter.tmpfiles $(DESTDIR)$(PREFIX)/lib/tmpfiles.d/basalt-greeter.conf

uninstall:
	rm -rf $(DESTDIR)$(PREFIX)/share/basalt-shell $(DESTDIR)$(LIBEXECDIR)/basalt-shell
	rm -f $(DESTDIR)$(PREFIX)/bin/basalt-shell $(DESTDIR)$(PREFIX)/bin/basalt-shelld $(DESTDIR)$(PREFIX)/libexec/basalt-shell/basalt-shell-ui-launch $(DESTDIR)$(PREFIX)/libexec/basalt-shell/basalt-vm-cursor $(DESTDIR)$(PREFIX)/bin/basalt-shell-ui $(DESTDIR)$(PREFIX)/bin/basalt-session $(DESTDIR)$(PREFIX)/bin/basalt-lock $(DESTDIR)$(PREFIX)/bin/basalt-session-init
	rm -f $(DESTDIR)$(PREFIX)/share/wayland-sessions/basalt-sway.desktop $(DESTDIR)$(PREFIX)/share/wayland-sessions/basalt-niri.desktop
	rm -f $(DESTDIR)$(SYSCONFDIR)/xdg/xdg-desktop-portal/sway-portals.conf $(DESTDIR)$(SYSCONFDIR)/xdg/xdg-desktop-portal/niri-portals.conf
	rm -f $(DESTDIR)$(PREFIX)/lib/systemd/user/basalt-session.target $(DESTDIR)$(PREFIX)/lib/systemd/user/basalt-headless.service
	rm -f $(DESTDIR)$(PREFIX)/lib/systemd/user/basalt-voice.service $(DESTDIR)$(PREFIX)/lib/systemd/user-preset/80-basalt-shell.preset
	rm -f $(DESTDIR)$(POLKITDIR)/actions/org.openbasalt.shell.policy $(DESTDIR)$(POLKITDIR)/rules.d/50-basalt-shell.rules
	rm -f $(DESTDIR)$(SYSCONFDIR)/pam.d/basalt-lock
	rm -rf $(DESTDIR)$(PREFIX)/share/basalt-greeter $(DESTDIR)$(LIBEXECDIR)/basalt-greeter
	rm -f $(DESTDIR)$(PREFIX)/lib/tmpfiles.d/basalt-greeter.conf

# SELinux modules basalt_shell and basalt_greeter: need selinux-policy-devel
# and the agent family's base module interface (basalt-agent-selinux installs
# basalt_agent_base.if), or scripts/build-selinux.sh in a Fedora container.
selinux:
	mkdir -p $(BUILD)/selinux
	cp selinux/*.te selinux/*.if selinux/*.fc $(BUILD)/selinux/
	$(MAKE) -C $(BUILD)/selinux -f /usr/share/selinux/devel/Makefile basalt_shell.pp basalt_greeter.pp

install-selinux:
	semodule -i $(BUILD)/selinux/basalt_shell.pp $(BUILD)/selinux/basalt_greeter.pp
	restorecon -F $(PREFIX)/bin/basalt-shell $(PREFIX)/bin/basalt-shelld $(PREFIX)/libexec/basalt-shell/basalt-shell-ui-launch \
		$(PREFIX)/bin/basalt-voiced $(PREFIX)/libexec/basalt-shell/basalt-skill $(PREFIX)/libexec/basalt-shell/basalt-skill-index

rpm:
	scripts/build-rpm.sh

clean:
	rm -rf $(BUILD)
