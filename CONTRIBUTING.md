# Contributing to the Basalt shell

Thank you for helping. This repository is the desktop shell of
[Basalt OS](https://basalt-os.org); the distribution itself (packages,
installer, system assistant, agent confinement) lives in
[basalt-os/basalt-os](https://github.com/basalt-os/basalt-os).

Security problems: do not open an issue, follow [SECURITY.md](SECURITY.md).

## Before you start

- Bugs and small fixes: open a pull request directly, or an issue first if
  you are not sure it is a bug.
- New typed actions, new MCP tools, new dependencies, or changes to what
  needs confirmation, what is audited or what the SELinux policy allows:
  open an issue first to agree on the design. The shell's rules come
  first: agents ask through typed actions, the person confirms every
  change, every change is written to the activity log, and only the shell
  UI's SELinux domain can confirm ([docs/design.md](docs/design.md),
  [docs/selinux.md](docs/selinux.md)). Contributions keep these properties.
- Looks: every color, size, radius and duration comes from the design
  tokens (`internal/theme/tokens.go`); QML does not hard-code them.

## Development setup

You need Go (the version in `go.mod` or newer), make, and for the UI
Quickshell with Qt 6. The quickest way to see a change without installing
anything is a nested session from a Fedora container:

```sh
make build test
scripts/try-podman.sh sway     # or niri
```

`make test` runs `go vet` and the unit tests (a fake compositor adapter
stands in for sway and niri where a test needs one). For the SELinux module,
`scripts/build-selinux.sh` builds it in a Fedora container; it needs the
agent family's base module `basalt_agent_base` from
[basalt-os/basalt-os](https://github.com/basalt-os/basalt-os)
(`packages/basalt-agent/selinux`), checked out next to this repository or
named with `BASALT_AGENT_SELINUX=DIR`. `make rpm` builds the packages the
same way.

The `lab/` directory holds the scripts used to build a Basalt OS desktop VM
and to capture the screenshots in `media/`. They run on a lab host you
configure in a `.env` file (`lab/env.example`); nothing in them points at a
particular machine.

## Pull requests

- Keep them focused; one topic per pull request.
- `make test` passes and `gofmt -l .` prints nothing. Add or update tests
  with the change.
- A change that a person sees comes with a screenshot (sway or niri) in the
  pull request.
- Code comments, documentation and commit messages in English.
- Do not commit secrets, real host names or addresses, not even in tests:
  use documentation addresses (192.0.2.0/24, `example.com`).
- Commit messages: a short summary line, then what changed and why.

By submitting a contribution you agree that it is licensed under the
Apache License, Version 2.0, this repository's license (see [LICENSE](LICENSE)
and [NOTICE](NOTICE)), as section 5 of that license provides. Artwork
contributions to `shell/wallpapers` are CC-BY-SA-4.0
([LICENSE-artwork](LICENSE-artwork)).

## Code of conduct

Be respectful and constructive. The OpenBasalt code of conduct
([CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)) applies to this repository.
