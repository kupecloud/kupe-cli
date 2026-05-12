# Security policy

We take the security of `kupe-cli` and the wider Kupe Cloud platform seriously.
The CLI handles authentication tokens that grant tenant-level access; please
help us keep them safe by reporting vulnerabilities responsibly.

## Reporting a vulnerability

**Please do not file public GitHub issues for security reports.**

Use one of:

1. **GitHub private vulnerability reporting** (preferred) — open a report at
   https://github.com/kupecloud/kupe-cli/security/advisories/new. Only the
   maintainer team sees the report; we coordinate the fix and disclosure with
   you privately.
2. **Email** — `security@kupe.cloud`. Please include enough detail to
   reproduce: kupe-cli version (`kupe version -o json`), OS, the command(s)
   that triggered the issue, and any logs (with secrets redacted).

We aim to:

- Acknowledge the report within **2 business days**.
- Provide an initial assessment within **5 business days**.
- Issue a fix within **30 days** for high-severity issues; longer for
  lower-severity issues, and we'll keep you posted.

## Supported versions

We patch the **latest minor release**. Security fixes are shipped as a new
patch release (`x.y.z+1`) rather than backported to older minor lines.
We'll publish an explicit support window here if the policy changes.

## Scope

In scope:
- The `kupe` binary, its installation methods (Homebrew tap, Scoop bucket,
  install script), and its release artifacts (archives, checksums, SBOMs).
- The CLI's local credential storage (OS keyring + plaintext fallback).
- The OAuth 2.0 device-code flow against Authentik (`kupe-cli` public client).

Out of scope (please report to the relevant project's security channel):
- Vulnerabilities in `kupe-api`, `console`, the Kupe Cloud platform itself,
  or the upstream Authentik / kubectl / cobra projects.
- Issues only reproducible against an end-of-life kupe-cli release.

## Hall of fame

We're happy to credit responsible reporters in release notes — just let us
know your preferred name (or pseudonym) when you file the report.
