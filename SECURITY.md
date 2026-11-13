# Security Policy

## Reporting a vulnerability

Please report vulnerabilities **privately** using GitHub's **"Report a
vulnerability"** button in the Security tab of the affected repository
(GitHub Security Advisories). Do not open a public issue for a security
problem. We will acknowledge the report and work with you on a fix and
disclosure.

## Scope

In scope: `conduitd`, the desktop app, the SDK and the official plugins,
including:

- the **plugin installer** (download, checksum verification, archive
  extraction, file placement);
- **credential storage** and handling;
- the local gRPC API.

Out of scope: vulnerabilities in the third-party networks themselves, and
account restrictions caused by using unofficial clients (see the README
disclaimer).

## Credential storage: please read

Account credentials (tokens, `api_id`/`api_hash`, ...) are stored **in
plaintext** (protobuf JSON) in the `accounts.credentials` column of the local
SQLite database, `~/.local/share/conduit/conduit.db`. They are protected only by
your operating-system file permissions. A keyring helper exists in the code
but is not used for account credentials today. Protect the data directory, use
full-disk encryption and do not share the database file.

The daemon listens on `127.0.0.1:50051` without authentication, so any local
process of your machine can use the API. That includes the `InstallPlugin`
call, which downloads and starts the latest release of an **official** plugin
(only the ids in the built-in catalog are accepted; arbitrary `owner/repo`
installs are CLI-only).

`conduitd` runs every `conduit-plugin-*` executable found in its plugin
directories, including `<current directory>/bin/plugins` (a development
convenience). Do not start `conduitd` from a directory you do not trust, and
keep `CONDUIT_PLUGIN_DIR` pointed at directories only you can write to.

Plugin installs verify a SHA-256 checksum published in the same GitHub Release
as the archive; this detects corruption but does not replace trusting the
repository you install from.
