# Contributing

Thanks for your interest! Issues and pull requests are welcome.

## Setup

- Go (version in `core/go.mod`) and Node.js 22 (GUI).
- `protoc`, `protoc-gen-go` and `protoc-gen-go-grpc` only if you touch the proto.
- `sdk/` and `core/` are separate Go modules; `core` uses the local `sdk`
  through a `replace` directive. Plugins live in their own repositories
  (`cestila/conduit-plugin-<id>`); to run them against a local `conduitd`, build
  them into `bin/plugins/`.

```sh
make build              # build conduitd into ./bin
cd gui && npm ci        # GUI dependencies
```

## Tests

```sh
make test-go            # go test ./... in sdk and core
make daemon             # the GUI daemon-manager test needs bin/conduitd
cd gui && npm test      # GUI tests (plain node scripts)
```

Some `core` plugin lifecycle tests are skipped when plugin binaries are not
built. Add or update tests with your change.

## Style

- Run `gofmt` on Go code (CI fails on unformatted files; generated `*.pb.go`
  are excluded) and keep `go vet` clean.
- Documentation and repository-facing text is in English.

## Changing the proto

Edit `sdk/proto/conduit.proto` and run `make proto`; commit the regenerated
code. CI has a proto-drift check that fails if the generated files are stale.
Protocol-breaking changes must bump `pluginsdk.ProtocolVersion`.

## Pull requests

`main` is protected: nobody pushes to it directly, maintainers included.

1. Create a branch from `main` (`feat/...`, `fix/...`, `docs/...`).
2. Open a pull request; the template lists what to check.
3. CI (`go`, `proto-drift`, `gui`) must pass and the branch must be up to date
   with `main`; open review conversations must be resolved.
4. Pull requests are **squash merged**: the PR title becomes the commit
   message on `main`, so it must follow
   [Conventional Commits](https://www.conventionalcommits.org)
   (`feat:`, `fix:`, `docs:`, `chore:`, ...). The branch is deleted after merge.

Keep pull requests focused and small enough to review.

## Changelog

`CHANGELOG.md` follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Every pull request with a user-visible change adds a line under
**[Unreleased]** in the matching section (`Added`, `Changed`, `Deprecated`,
`Removed`, `Fixed`, `Security`); SDK changes go under an **SDK** heading.
Changes with no user-visible effect (CI, refactors, tests) need no entry.
Releases move the entries under the new version (see
[docs/RELEASING.md](docs/RELEASING.md)).

## New networks

The `Provider` enum is closed, so a plugin for a new network first needs an SDK
pull request adding a value. See [docs/PLUGINS.md](docs/PLUGINS.md).

## License of contributions

A DCO sign-off is not required. By contributing you agree your work is
licensed under Apache-2.0.
