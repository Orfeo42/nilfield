# nilfield

[![CI](https://github.com/Orfeo42/nilfield/actions/workflows/ci.yml/badge.svg)](https://github.com/Orfeo42/nilfield/actions/workflows/ci.yml)

`nilfield` is a `go/analysis` analyzer that reports one thing: a nil pointer or a nil interface being dereferenced. Nothing else. A nil map, slice, channel or func value is a different hazard class and is out of scope even when the value is reached through a struct field, so `o.p.m["k"] = 1` and `o.p.fn()` report only `o.p`, the pointer actually dereferenced.

It is importable as `github.com/Orfeo42/nilfield/analyzer` and distributed as a standalone binary.

## Install

```sh
go install github.com/Orfeo42/nilfield/cmd/nilfield@latest
```

Or download a prebuilt binary from a [GitHub release](https://github.com/Orfeo42/nilfield/releases).

To run `nilfield` as a golangci-lint module plugin, download the prebuilt `custom-gcl-<os>-<arch>` asset for your platform from a [GitHub release](https://github.com/Orfeo42/nilfield/releases) — `linux`, `darwin` and `windows` (`.exe`), each on `amd64` and `arm64` — which pins golangci-lint v2.13.2. `custom-gcl_checksums.txt` in the same release carries their SHA-256 sums, so `sha256sum -c` verifies a download. Building your own with `golangci-lint custom` against the `.custom-gcl.yml` in this repository stays supported. See [`GOLANGCI-PLUGIN.md`](GOLANGCI-PLUGIN.md) for the plugin contract and configuration.

## Run

```sh
nilfield ./...
nilfield -exclude-paths=internal/dao/,internal/mocks/ ./...
```

Or through `go vet`:

```sh
go vet -vettool=$(which nilfield) ./...
```

Every finding is reported and the intended gate is zero findings; there is no baseline file.

| flag | meaning |
| --- | --- |
| `-exclude-paths` | Comma-separated path fragments. A file whose slash-normalized path contains one of them is never reported; `_test.go` files are always skipped. |

There is no other flag. Nothing in the analyzer is keyed on a package path or a function name: every cross-function property it needs is derived from the callee's own body and carried as an analysis fact, so it works across packages with no annotations and no configuration beyond the exclude list.

## What is reported

Three diagnostic forms, all about a dereference: `<path> may be nil here`, `<path> is nil here`, `<path> holds a nil pointer here`.

The sites:

- selecting a field or calling a method through a nil pointer or interface
- a promoted field reached through a nil embedded pointer
- explicit `*p` and `(*pp).next`, where the dereferenced path is a struct field or an already-starred path
- a bare local whose nil origin is visible in the same function: declared with no initializer, assigned nil, copied from such a local, a nil pointer stored into an interface, an element of a map or slice of pointer type, a type assertion result, or a call result a fact marks nil-able
- a method call on a nil receiver whose body dereferences it
- `err.Error()` on an unchecked error value

Bare parameters and locals with no visible nil origin are deliberately not reported, dereferenced with `*p` as much as selected through: `*d = Date{}` in a pointer-receiver method and `newPay := *pay` are the shape of ordinary code, not of a missing guard.

## What counts as a proof

A path is proven non-nil by any of:

- `x != nil` in any boolean form, including `&&`/`||` chains and behind `!`
- an early return or panic in the opposite branch
- assignment of an address or `new(...)`
- fields set in a composite literal
- a call to an assert helper
- a call to a nil-predicate helper, on the branch where it is false: `if !g.IsNil(err) { ... }`
- a checked call to a validator method
- `switch { case x == nil: return }` and `switch x { case nil: return }`, including the code after an exiting clause and later clauses
- every construction in the package wiring the field: a pointer or interface field that only this package can write - any field of an unexported type, an unexported field of an exported one - and that no construction in the package's non-test files leaves visibly nil, is not reported at its uses. Visibly nil means the literal `nil`, a map or slice element, a type assertion result, a call a `may return nil result` fact marks nil-able, or a local carrying one of those. A dependency handed to the constructor as a parameter, copied off another struct, or returned by an interface method is not visibly nil, the same way a bare parameter with no visible nil origin is not reported at its own uses

A write to a path inside a loop body, a non-exiting `if` branch, or a switch or select clause drops the proof after that statement, since the loop or clause may not run at all. A closure started with `go` does not inherit field-path proofs, since the guard held when the goroutine was created rather than when it runs.

## Facts

Everything the analyzer knows about another function is derived from that function's own body and carried as an `analysis.Fact`, so it works across packages with no annotations and no configuration. Nothing is keyed on a package path or a function name; builtins are resolved through `types.Universe`.

| fact | meaning |
| --- | --- |
| `validates <fields>` | a method returning a single unnamed error whose body rejects a nil receiver field before any path that can return a nil error |
| `asserts argument N` | a function that panics unless its Nth boolean argument holds, recognised from its own body, including delegation to another assert helper |
| `never returns` | the function's body always ends in a panic, or in a call to another never-returning function |
| `nil-safe receiver` | a pointer-receiver method that never dereferences its own receiver unguarded |
| `may return nil result N` | used to report the caller dereferencing that result; the producing function itself is not reported |
| `reports nil argument N` | a bool-returning function that answers true whenever its Nth argument is nil, recognised from a leading `if p == nil { return true }` guard or from delegation to another such function; the caller's false branch has that argument proven |

## Scope

`nilfield` covers one hazard class: a nil pointer or a nil interface reaching a dereference. It does not report:

- a nil map, slice, channel or func value: writing to a nil map, indexing a nil slice, sending on or closing a nil channel, or calling a nil func value all panic or block, but none of them is a pointer or interface dereference
- a struct constructed with a pointer or interface field left nil: that is a finding about the constructor, not about a dereference
- a checked error that is discarded, or a declared function compared against `nil`: neither is a question about a nil value reaching a dereference

## Corpus and coverage

`analyzer/testdata/src/nilcases` is a corpus of the ways a nil value reaches a fault in Go, deliberately wider than this analyzer's scope. It holds 45 in-scope hazard cases across 47 sites, all covered; 13 cases marked out of scope; and 16 safe cases that must stay silent. `TestCorpus` turns every marker into a subtest.

The suite is green: `go test ./...` passes, and the coverage subtest logs:

```
nilfield fully covers 45 of 45 hazard cases (47 of 47 hazard sites)
out of scope: 7 not-a-dereference, 4 construction-site, 2 not-nil-analysis
```

An out-of-scope case fails its own subtest if the analyzer reports anything inside it, which makes the scope boundary a build gate rather than a promise.

## Documents

- [`REQUIREMENTS.md`](REQUIREMENTS.md), the scope and design record: what the analyzer reports, the mechanism behind each capability, and what was deliberately excluded and why.
- [`GOLANGCI-PLUGIN.md`](GOLANGCI-PLUGIN.md), an evaluation of shipping this as a golangci-lint module plugin: what the contract requires, what it costs consumers, and the recommendation.

## License

The analyzer source in this repository is MIT-licensed, see [`LICENSE`](LICENSE). The published `custom-gcl-<os>-<arch>` release assets statically link golangci-lint (GPL-3.0) with this plugin and are distributed under GPL-3.0, see [`NOTICE.md`](NOTICE.md).

## Development

```sh
go test ./...
```

Tagging `v*` builds linux, darwin and windows binaries for amd64 and arm64 via GoReleaser. CI runs gofmt, `go vet`, `go build`, `go test -race` and golangci-lint on every push and pull request; `.golangci.yml` lints this repo's own Go source.
