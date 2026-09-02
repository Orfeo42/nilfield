# nilfield

`nilfield` reports method calls and field accesses made through a pointer-typed struct field that no nil guard dominates, the shape that turns a missing check into a nil-pointer panic at runtime. It also reports the same hazard on a bare local whose own nil origin is visible in the same function, a handful of return-shape mistakes, and a wiring struct constructed with a field left nil.

It is a standard `go/analysis` analyzer, importable as `github.com/Orfeo42/nilfield/analyzer` and distributed as a standalone binary.

## Install

```sh
go install github.com/Orfeo42/nilfield/cmd/nilfield@latest
```

## Run

```sh
nilfield ./...
nilfield -exclude-paths=internal/dao/,internal/mocks/ ./...
```

Every finding is reported and the intended gate is zero findings; there is no baseline file.

| flag | meaning |
| --- | --- |
| `-exclude-paths` | Comma-separated path fragments. A file whose slash-normalized path contains one of them is never reported; `_test.go` files are always skipped. |

Nothing in the analyzer is keyed on a package path or a function name. Every cross-function property it needs is derived from the callee's own body and carried as an analysis fact, so it works across packages with no annotations and no configuration beyond the exclude list.

## What counts as a proof

A path is proven non-nil by any of:

- an `x != nil` condition in any boolean form, including inside `&&`/`||` chains and behind `!`
- an early return or panic in the opposite branch
- an assignment of an address or `new(...)`, or a composite literal setting the field
- a call to a function whose body proves it panics unless a given argument holds, recognized as a fact from the callee's own body
- a checked call to a validator method, a method returning a single `error` whose body rejects a nil receiver field before any path that can return a nil error, recognized as a fact
- a tagless `switch { case x == nil: return }` or a tagged `switch x { case nil: return }` used as a guard, including later clauses and the code after an exiting clause
- an `if`/`else` pair that only leaves the non-nil branch reachable

The validator and assert-helper rules are derived from the callee's own guards and travel between packages as analysis facts, so nothing has to be annotated at the call site. A write to a path anywhere in a loop body, in a non-exiting `if` branch, or in a switch/select clause drops the proof after that statement, since the loop or clause may not run at all.

## What is reported

1. Field paths (`o.p`, `s.repo`, `o.p.next`, promoted fields reached through an embedded pointer such as `o.e` reading through a nil `o.embedded`, and explicit double indirection `(*pp).n`) with no dominating proof, on: field or method selection through a pointer or interface field, an explicit `*x`, calling a func-typed field, or writing to a map-typed field. Slice indexing and channel operations on fields are not reported, only the pointer earlier in the path is.
2. Bare locals whose nil origin is visible in the same function: declared without an initializer (`var i *inner`, `var m map[string]int`, `var ch chan int`, `var f func()`), assigned `nil`, copied from such a local (including a nil pointer stored into an interface, reported as holding a nil pointer), a map or slice element of pointer type (`m["k"].n`, `s[0].n`, or through a local holding one), a single-form or comma-ok type assertion result, or a call result carrying a nil-result fact. The uses reported are selection, `*x`, call, map write, slice index, channel send, and `close`. Messages: `x is nil here`, `x holds a nil pointer here`, `x may be nil here`.
3. Two bare cases with no visible origin at all: an explicit `*p` on an unproven bare pointer, and a method call on an unproven `error` value (`err.Error()` with no check).
4. Comparing a declared function with `nil` (`abs != nil`): `abs is never nil`.
5. Return shapes: `return nil, nil`, a nil value beside a nil error, reports `nil value returned with a nil error`; returning a nil error from inside a branch that already checked one reports `err is discarded`.
6. Construction of a wiring struct, a struct whose fields are all pointer or interface typed, that sets at least one field but leaves another unset or explicitly `nil`, whether in a composite literal or as `s := new(T)` / `s := &T{}` followed by partial field assignments: `service is constructed with dep, logger left nil`. A completely empty literal is not reported, that is the downstream `o.p may be nil` case instead.
7. Goroutines: a closure launched with `go` does not inherit the field-path proofs that held at the point it was created, since the guard held when the goroutine was launched, not when it runs; locals keep theirs.

## Facts

| fact | meaning |
| --- | --- |
| `validates <fields>` | the method returns a single unnamed `error` and its body rejects a nil receiver field before any path that can return a nil error. |
| `asserts argument N` | the function panics unless its Nth bool parameter holds, proven from its own body: an `if !p { panic(...) }` shape, an `if p { return }` followed by a panic, or delegation to another assert helper with `p` in the asserted position. |
| `never returns` | the function's body always ends in a panic, or in a call to another never-returning function, and contains no return statement. |
| `nil-safe receiver` | a pointer-receiver method whose body never dereferences its own receiver without a guard; a call through a known-nil local is not reported when the callee carries this fact. |
| `may return nil result N` | the function returns a literal `nil` in result N on some path where its error result, if it has one, is also `nil`; a caller that dereferences that result without a nil check is reported. |

## Scope

`nilfield` covers one hazard class: a nil value reaching a dereference, call, write, or blocking operation, whether it is rooted at a struct field or at a bare local whose own nil origin is visible in the same function. It does no interprocedural dataflow beyond the facts above, and it does not need SSA.

`analyzer/testdata/src/nilcases` is a corpus of every way a nil value reaches a dereference in Go: **56 hazard cases across 62 sites**, plus 17 cases that are correct and must stay silent. Every function there carries a marker naming the case, and `TestCorpus` turns each marker into a test case by counting the diagnostics that land inside it.

The corpus is fully covered. `go test ./...` is green, and `TestCorpus/coverage` logs `56 of 56 hazard cases (62 of 62 hazard sites)`, with zero false positives. `TestCorpus` is the coverage gate: if a future change drops a case below 56/56 or introduces a false positive, the corresponding subtest fails.

`govet`'s `nilness` and `nilfunc` passes, already enabled in a default golangci-lint run, independently cover 5 of the corpus sites (a declared func compared to nil, a nil map write, a nil slice index, a nil channel send, a nil dynamic func call). `nilfield` now covers those same 5 sites itself, so the two tools overlap there; they remain complements rather than one replacing the other.

The corpus is also a yardstick for any other nil-safety tool: run it over the package and compare.

## Documents

- [`REQUIREMENTS.md`](REQUIREMENTS.md), the design record: what each corpus group demanded and the mechanism built to cover it, with the order the work was done in.
- [`GOLANGCI-PLUGIN.md`](GOLANGCI-PLUGIN.md), evaluation of shipping this as a golangci-lint module plugin: what the contract requires, what changed here, what it costs consumers, and the recommendation.

## Test

```sh
go test ./...
```
