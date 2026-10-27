# Linux CORE Acceptance

This checklist separates native implementation and automated checks from human
acceptance and release publication. The current runtime remains **0.0.2**. The
private signed 0.0.3 candidate was used only for isolated upgrade testing.

## Safe Human Session

Build from the reviewed checkout, without installing over an existing ATM:

```bash
qa=$(mktemp -d /tmp/atm-core-human.XXXXXX)
go build -o "$qa/atm" ./cmd/atm
bash scripts/test_linux_core.sh --binary "$qa/atm" --home "$PWD"
```

The harness requires Linux and a real terminal. It inspects the selected runtime
read-only, creates private temporary HOME/XDG/ATM directories, clears inherited
credentials/settings, removes shell/Python commands from ATM's PATH and supplies
`--dry-run` to every ATM command. Bash is only the QA launcher, not a CORE service.
Use only an independently trusted executable and runtime, not arbitrary downloads.

For CLI checks without opening the interface:

```bash
bash scripts/test_linux_core.sh --binary "$qa/atm" --home "$PWD" --no-tui
```

There is no network discovery, installation, signing, publication or profile edit.
The temporary QA directory is printed and retained; the harness fails if ATM
writes inside its isolated user directories. A PASS confirms those automated
checks, not visual approval. CLI output is retained in `cli.log` beside those
directories. Do not `source` the script.

## Human Checklist

Record PASS/FAIL with Linux distribution, architecture, terminal, source commit
and any error text. Unperformed checks remain **Pending**.

- Resize the terminal; verify readable, non-overlapping content and navigation.
- Use arrow keys and mouse selection; verify they select the displayed item.
- Open CORE and inspect update, changes, backup/recovery, setup and PATH actions.
- Check CORE/plugin updates in dry-run; expect offline plans, not live badges.
- Inspect single/all plugin actions and Full Setup; no application should start.
- Verify confirmations default to Cancel where offered; cancel, return and quit.
- Exercise the available dry-run action path; expect a plan, not a completed install.
- Quit normally and with Ctrl+C in separate sessions; verify terminal echo/cursor
  and mouse behavior return to normal.
- Confirm the harness reports no user-state writes. Do not treat a dry-run as
  evidence that a real package update or application installation completed.

Signed enrollment/verification, explicit plugin baseline verification and
independent CORE recovery are intentionally CLI-only. The TUI does not provide a
CORE history picker. Those are documented interface boundaries, not hidden Bash
fallbacks. Application-specific native backends are a separate migration.

## Automated Evidence

The Go tests cover native CLI/TUI routing, consent, cancellation, no-write/no-network
dry-run, setup/PATH isolation, plugin transactions and independent CORE recovery.
The Linux PTY suite tests compiled binaries in a pseudo-terminal; it does not
replace human visual acceptance. See the route evidence in
[Native CORE Administration](NATIVE-CORE-ADMINISTRATION.md).

The officially signed full-package cycle and six process-interruption cases are
documented in [Signed Linux Release Test](LINUX-SIGNED-RELEASE-TEST.md#two-version-acceptance).
Those tests never update the real installed copy. Missing private QA artifacts
cause opt-in tests to skip; a skip must not be counted as a passing acceptance.

Windows, macOS and Linux ARM64 runtime acceptance remain **Pending**. Five-target
cross-builds and package checks do not change that status. Windows automatic
external-updater handoff also remains Pending implementation.

## Trust Boundaries

The implementation checks the embedded approved Ed25519 identity, signed payload
hashes, installed baseline, strictly newer versions and local-file conflicts.
It does not establish independent freshness, automate signing-key revocation or
rotation, guarantee power-loss durability or defend against a hostile process
running as the same user. Stop other ATM sessions before a real update.

These limits remain explicit; this checklist does not introduce a new trust policy
or authorize key replacement, automatic deployment or release publication. If the
key is suspected compromised, do not use self-update to obtain a replacement trust
root; maintainer review and an independently authenticated distribution are needed.
