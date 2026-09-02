# nilfield

`nilfield` reports method calls and field accesses made through a pointer-typed struct field that no nil guard dominates — the shape that turns a missing check into a nil-pointer panic at runtime.

It is a standard `go/analysis` analyzer, distributed as a standalone binary.

## Install

```sh
go install github.com/Orfeo42/nilfield@latest
```

## Run

```sh
nilfield ./...
nilfield -assert-package=example.com/app/utility -exclude-paths=internal/dao/ ./...
```

Every finding is reported and the intended gate is zero findings; there is no baseline file.

| flag | meaning |
| --- | --- |
| `-assert-package` | Import path of a package whose `Assert`/`AssertWithCode` helpers panic on a false condition. Calls to them are read as proofs. The package must be imported by the code under analysis; a helper declared in the same package is not recognised. |
| `-exclude-paths` | Comma-separated path fragments. A file whose path contains one of them is skipped — generated code, mostly. |

## What counts as a proof

A path is proven non-nil by any of:

- an `x != nil` condition, including inside `&&`/`||` chains and behind `!`
- an early return or panic in the opposite branch
- an assignment of an address or `new(...)`
- a call to an `Assert` helper from the package named in `-assert-package`
- a checked call to a validator method: a method returning a single `error` whose body rejects a nil receiver field before any path that can return a nil error

The validator rule is derived from the callee's own guards and travels between packages as an analysis fact, so nothing has to be annotated at the call site. Writing to the field again drops the proof.

Only fields reached through a struct are reported. A bare local or parameter is out of scope, because the guard there is hard to miss.

## Scope

`nilfield` covers one hazard class deliberately. It does not look at nil maps, nil slices, nil channels, nil interface values or nil func values — `govet`'s `nilness` and `nilfunc` passes cover those, with no overlap, and both are already enabled in a default golangci-lint run.

`testdata/src/nilcases` is a corpus of every way a nil value reaches a dereference in Go, deliberately wider than this analyzer's scope: **56 hazard cases across 62 sites**, plus 17 cases that are correct and must stay silent. Every function there carries a marker naming the case, and `TestCorpus` turns each marker into a test case by counting the diagnostics that land inside it.

Current figure: **27 of 56 cases fully covered (30 of 62 sites)**, with 2 false positives.

**The test suite is red, deliberately.** Every uncovered case is a failing subtest and so is every false positive — 32 failures today. They are the work list, and each goes green on its own as the case is implemented. `go test ./...` passes only when the analyzer covers the whole corpus.

```
--- FAIL: TestCorpus/promoted-field-on-nil-embedded
    not covered: 0 of 1 hazard sites reported
--- FAIL: TestCorpus/guarded-switch-nil-case
    false positive: 1 diagnostics in a case that is correct
```

The corpus is also a yardstick for any other nil-safety tool: run it over the package and compare.

## Documents

- [`REQUIREMENTS.md`](REQUIREMENTS.md) — what each of the 29 remaining cases demands, grouped by the analysis capability it needs, with a build order.
- [`GOLANGCI-PLUGIN.md`](GOLANGCI-PLUGIN.md) — evaluation of shipping this as a golangci-lint module plugin: what the contract requires, what has to change here, what it costs consumers, and the recommendation.

## Test

```sh
go test ./...
```
