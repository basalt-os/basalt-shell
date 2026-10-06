# Lab: Settings, Updates and channels

A VM to try the Updates and channels page end to end: enable the testing
channel with its consent, check for updates, install them with a
snapshot, undo the last update, turn the testing channel off, add and
remove a software source.

- `run-updates-vm.sh`: the VM, a qcow2 overlay on an installed Basalt OS
  desktop disk (the backing file is opened read only), no TPM device,
  Secure Boot firmware with its own copy of the variables. `create
  BACKING VARS`, `start [gl|nogpu]`, `stop`, `monitor CMD`.
- `dev-install.sh SRC BIN`: run as root in the VM, installs a development
  build of the shell (daemon, client, QML, catalogs, the read helper) and,
  when BIN has it, of the assistant's `basalt` command, over the
  installed packages (originals kept in /root/upd-orig).
- `vm-type.py`: types into the VM through the QEMU monitor (the password
  for polkit, Super+Comma for Settings).

The VM needs the published Basalt OS repositories: basalt-release 44-8 or
newer (basalt, basalt-tools and basalt-testing defined, signed with the
OpenBasalt release key) and the assistant and shell packages they carry,
so that the development build sits on the same SELinux policy and helpers.

Run on the VM, as the person, with the page:

1. Settings (Super+Comma), Updates and channels.
2. Basalt testing: the toggle, the consent sheet, Details, "Turn on
   preview builds", the administrator's password; the card shows it on.
3. Check for updates; Install updates; the sheet follows the steps (the
   snapshot, the download, the install, the checks).
4. Restart now when asked; after the restart, Undo the last update, the
   password, Restart now: the system is back on the snapshot.
5. Basalt testing off.
6. Add a source (for example Visual Studio Code): the sheet shows the
   key's fingerprint and owner; Trust and add; then Remove.

Check afterwards, as root: `basalt channels`, `basalt updates`, `snapper
list`, `basalt audit 30`, and `ausearch -m AVC,USER_AVC -ts recent` (no
denials).
