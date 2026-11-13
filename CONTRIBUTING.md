# Contributing

Thanks for your interest! Issues and pull requests are welcome.

## Setup

- Go (version in `core/go.mod`) and Node.js 22 (GUI).
- `protoc`, `protoc-gen-go` and `protoc-gen-go-grpc` only if you touch the proto.
- The repository is a Go workspace (`go.work`): `sdk/`, `core/`, plugins.

```sh
make build              # build conduitd and the plugins into ./bin
cd gui && npm ci        # GUI dependencies
```

## Tests

```sh
make test-go            # go test ./... in sdk, core and each plugin
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

## Commits and pull requests

- Use [Conventional Commits](https://www.conventionalcommits.org)
  (`feat:`, `fix:`, `docs:`, `chore:`, ...).
- Keep pull requests focused and small enough to review.
- A DCO sign-off is not required. By contributing you agree your work is
  licensed under Apache-2.0.

New networks: the `Provider` enum is closed, so a new plugin first needs an SDK
pull request adding a value. See [docs/PLUGINS.md](docs/PLUGINS.md).
