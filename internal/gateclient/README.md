# gateclient

A copy of `pkg/gate` from the basalt-os repository
(`packages/basalt-gate/pkg/gate`): the reference client of the Basalt
approval gate protocol (gate/1, `packages/basalt-gate/PROTOCOL.md` there),
standard library only. Dual-licensed MIT OR Apache-2.0 like the protocol
(`LICENSE-MIT` here, the repository's `LICENSE` for Apache-2.0).

Only the package name differs from the original. `testdata/digest.json`
holds the protocol's digest vectors; `go test` checks this copy against
them. To update: copy the four files again, replacing `package gate` with
`package gateclient`, and the vectors.
