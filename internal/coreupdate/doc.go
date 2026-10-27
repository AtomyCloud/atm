// Package coreupdate provides opt-in, offline compiled CORE verification,
// staging and recoverable single-executable replacement, plus offline signed
// full-package verification. The maintainer-approved public key is embedded;
// production replacement integration is not enabled. Full packages use their
// own signature contract and are never fed to the single-executable service.
//
// The caller must approve a key, baseline receipt, binary-only payload boundary,
// version and user consent, and must quiesce other ATM sessions before replacing.
// Only trusted code may construct Policy; no CLI public-key bypass is intended.
// No downloaded code is executed. The caller owns reader deadlines/cancellation.
// Plugins, user data, legacy assets and launchers are never replaced.
//
// StartupGuard is a conservative read-only guard requiring no key. With an
// approved policy, CheckStartup verifies completed/rolled-back state. Callers
// must run guards before normal services and exit the old process after Replace.
// A guard in a new binary cannot repair an executable that cannot itself start;
// an independent trusted recovery entrypoint remains an integration requirement.
//
// Windows supports staging only; Replace and Recover refuse all Windows renames,
// including in-use executables. Its staging inherits the root's ACLs: the caller
// must supply a private per-user root, not a shared directory. POSIX modes do not
// establish Windows ACL privacy. Same-host Linux/macOS replacement retains an
// immutable journal, candidate and backup. Recovery only restores the pinned
// baseline and refuses local edits. Finalize can move a completed transaction
// into retained history before another independently authorized binary-only
// transaction. This is not a full-package replacement engine. Key rotation,
// downgrade/prerelease policy, automatic restart and archive extraction remain
// unsupported here.
// Incomplete pre-journal stages or corrupt state require manual inspection.
// Guarantees cover ordinary failure/process interruption on local filesystems,
// not power-loss durability, external writers or hostile same-user processes.
package coreupdate
