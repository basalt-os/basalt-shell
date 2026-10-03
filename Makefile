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

.PHONY: all build test vet install uninstall clean rpm help

all: build

help:
	@echo "make build | test | install [PREFIX=/usr/local DESTDIR=] | rpm | clean"
	@echo "scripts/install.sh --user | --system   install with dependencies (see README)"
	@echo "scripts/try-nested.sh [sway|niri]      run a session in a window of your desktop"

build:
	mkdir -p $(BUILD)
	$(GO) build $(GOFLAGS) -ldflags "-X main.version=$(VERSION)" -o $(BUILD)/basalt-shell ./cmd/basalt-shell

test:
	$(GO) vet ./...
	$(GO) test ./...

install:
	test -x $(BUILD)/basalt-shell || $(MAKE) build
	install -Dm755 $(BUILD)/basalt-shell $(DESTDIR)$(PREFIX)/bin/basalt-shell
	install -Dm755 bin/basalt-shell-ui $(DESTDIR)$(PREFIX)/bin/basalt-shell-ui
	install -Dm755 bin/basalt-session $(DESTDIR)$(PREFIX)/bin/basalt-session
	install -Dm755 bin/basalt-lock $(DESTDIR)$(PREFIX)/bin/basalt-lock
	install -d $(DESTDIR)$(PREFIX)/share/basalt-shell/qml/wallpapers $(DESTDIR)$(PREFIX)/share/basalt-shell/themes
	install -m644 shell/*.qml $(DESTDIR)$(PREFIX)/share/basalt-shell/qml/
	install -m644 shell/wallpapers/* $(DESTDIR)$(PREFIX)/share/basalt-shell/qml/wallpapers/
	install -m644 themes/*.json $(DESTDIR)$(PREFIX)/share/basalt-shell/themes/
	install -Dm644 config/sway/config $(DESTDIR)$(PREFIX)/share/basalt-shell/sway/config
	install -Dm644 config/niri/config.kdl $(DESTDIR)$(PREFIX)/share/basalt-shell/niri/config.kdl
	install -Dm644 config/niri/basalt-theme.kdl $(DESTDIR)$(PREFIX)/share/basalt-shell/niri/basalt-theme.kdl
	install -Dm644 config/portals/xdpw-config $(DESTDIR)$(PREFIX)/share/basalt-shell/portals/xdpw-config
	install -Dm644 config/sessions/basalt-sway.desktop $(DESTDIR)$(PREFIX)/share/wayland-sessions/basalt-sway.desktop
	install -Dm644 config/sessions/basalt-niri.desktop $(DESTDIR)$(PREFIX)/share/wayland-sessions/basalt-niri.desktop
	install -Dm644 config/portals/sway-portals.conf $(DESTDIR)$(SYSCONFDIR)/xdg/xdg-desktop-portal/sway-portals.conf
	install -Dm644 config/portals/niri-portals.conf $(DESTDIR)$(SYSCONFDIR)/xdg/xdg-desktop-portal/niri-portals.conf
	install -Dm644 config/systemd/basalt-session.target $(DESTDIR)$(PREFIX)/lib/systemd/user/basalt-session.target
	install -Dm755 libexec/assistant-read $(DESTDIR)$(LIBEXECDIR)/basalt-shell/assistant-read
	install -Dm644 config/polkit/org.openbasalt.shell.policy $(DESTDIR)$(POLKITDIR)/actions/org.openbasalt.shell.policy
	install -Dm644 config/polkit/50-basalt-shell.rules $(DESTDIR)$(POLKITDIR)/rules.d/50-basalt-shell.rules

uninstall:
	rm -rf $(DESTDIR)$(PREFIX)/share/basalt-shell $(DESTDIR)$(LIBEXECDIR)/basalt-shell
	rm -f $(DESTDIR)$(PREFIX)/bin/basalt-shell $(DESTDIR)$(PREFIX)/bin/basalt-shell-ui $(DESTDIR)$(PREFIX)/bin/basalt-session $(DESTDIR)$(PREFIX)/bin/basalt-lock
	rm -f $(DESTDIR)$(PREFIX)/share/wayland-sessions/basalt-sway.desktop $(DESTDIR)$(PREFIX)/share/wayland-sessions/basalt-niri.desktop
	rm -f $(DESTDIR)$(SYSCONFDIR)/xdg/xdg-desktop-portal/sway-portals.conf $(DESTDIR)$(SYSCONFDIR)/xdg/xdg-desktop-portal/niri-portals.conf
	rm -f $(DESTDIR)$(PREFIX)/lib/systemd/user/basalt-session.target
	rm -f $(DESTDIR)$(POLKITDIR)/actions/org.openbasalt.shell.policy $(DESTDIR)$(POLKITDIR)/rules.d/50-basalt-shell.rules

rpm:
	scripts/build-rpm.sh

clean:
	rm -rf $(BUILD)
