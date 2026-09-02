# Requirements: catching every case in the corpus

The corpus in `analyzer/testdata/src/nilcases` holds **56 hazard cases across 62 hazard sites**, plus **17 safe cases** that must stay silent. `TestCorpus` measures the analyzer against it on every run and prints the figure.

As of 2026-09-02: **56 of 56 cases fully covered (62 of 62 sites)**, with **0 false positives**. `go test ./...` is green.

This document is the design record. It says what each corpus case demanded, grouped by the capability it needed rather than by corpus section because the grouping is what decided the build order, and it records the mechanism actually built for each group.

## What the analyzer does today

One intraprocedural pass over the AST. It tracked paths rooted at a **struct field** (`o.p`, `s.repo`, `o.p.next`), and reported a dereference that no guard dominates. A guard was an `x != nil` condition in any boolean form, an early return or panic in the opposite branch, an assignment of an address or `new(...)`, a call to an `Assert` helper from the package named by the `-assert-package` flag (removed once assert helpers were recognized as a fact, see the false positives section below), or a checked call to a validator method whose non-nil guarantee travels as an analysis fact.

Two properties follow from that design and explain most of the gap list:

- **Only struct fields are roots.** A bare local or parameter is deliberately out of scope, on the theory that a missing guard there is hard to miss. That single decision accounts for 11 of the 29 gaps.
- **Only pointer-typed paths are hazards.** A nil map, slice, channel, func or interface value is not tracked at all, which accounts for another 8.

## Group 1 — value-kind nils (8 cases)

`nil-map-write`, `nil-map-write-on-field` (the `.m` half), `nil-slice-index`, `nil-channel-send`, `nil-channel-close`, `call-nil-func-var`, `call-nil-func-field` (the `fn` half), `compare-declared-func-to-nil`.

The hazard is not a pointer dereference. Writing to a nil map panics while reading it does not; indexing a nil slice panics while appending to it does not; sending on a nil channel blocks forever and closing one panics; calling a nil func value panics.

**Built:** the same path tracking already in place, applied to map-, slice-, channel- and func-typed paths, with a per-kind use table (`useMapWrite`, `useSliceIndex`, `useChanSend`, `useChanClose`, `useCall` in `analyzer/expr.go`) naming which operation is fatal for that kind. No new analysis machinery, and once Group 2 gave bare locals their own nil-state, the same per-kind table reports the local-rooted forms of these cases too, so this group and the bare-local half of Group 2 share one mechanism.

**Prior art:** `govet`'s `nilness` and `nilfunc` passes already report 5 of these 8 — nil map update, nil slice index, nil channel send, nil dynamic func call, and `f != nil` on a declared func. They are enabled by default in golangci-lint, and measured against this corpus they overlap with `nilfield` on **zero** sites. Implementing this group duplicates work that is already free. Do it only for the field-rooted halves (`o.p.m`, `o.p.fn`), which `nilness` cannot see because it starts from SSA values, not paths.

## Group 2 — locals and parameters as roots (11 cases)

`nil-slice-index`, `nil-channel-send`, `nil-channel-close`, `call-nil-func-var`, `call-on-nil-receiver`, `typed-nil-in-interface`, `nil-map-write`, `nil-error-message`, `deref-func-result`, `missing-map-pointer-value`, `nil-element-in-slice`.

Every one of these dereferences a local variable, a parameter, or the result of an expression rather than a struct field.

Widening the root set is mechanically small and behaviourally the largest change in this document: a parameter is non-nil in most call sites the analyzer cannot see, so reporting every unguarded parameter dereference produces a diagnostic on a large fraction of all Go functions ever written. This is why the scope was drawn at struct fields in the first place.

**The honest options:**

1. **Report only locals whose nil origin is visible in the same function** — `var i *inner` with no assignment, a `map[k]` lookup of pointer type, a slice element, a call to a function known to return nil. That covers `call-on-nil-receiver`, `missing-map-pointer-value`, `nil-element-in-slice`, `nil-slice-index`, `nil-channel-send`, `nil-channel-close`, `call-nil-func-var`, `typed-nil-in-interface` without touching parameters.
2. **Report parameters too**, behind a flag that is off by default. That is the only way to reach `nil-error-message` and it will be noisy.

Option 1 is the one built. It needed dataflow, not just dominance: knowing that `i` was declared and never assigned, or that `m["k"]` yields the zero value on a miss.

**Built:** a per-function scope carries `nilable`, a map from a bare local's name to what is known about its own value (`analyzer/scope.go`). A local is marked nil, or maybe-nil, when it is declared without an initializer, assigned `nil`, copied from another local already marked, a map or slice element of pointer type, a single-form or comma-ok type assertion result, or the result of a call whose callee carries the `may return nil result N` fact. Any later use (selection, `*x`, call, map write, slice index, channel send, `close`) is reported against that state, which is what makes `typed-nil-in-interface` fall out of the same mechanism: assigning a nil pointer to an interface-typed local marks it, and the message says it holds a nil pointer rather than is nil. Parameters were deliberately left out of `nilable`, exactly as option 1 specified. `nil-error-message` is still covered, by a second, narrower rule bundled into the same change: an explicit `*p` on an unproven bare path, and a method call on an unproven `error` value, are reported regardless of whether the path is a local or a parameter, because both are self-evidently wrong at the definition site with no call-site context needed. That is two universal cases, not general parameter dataflow, so option 2's noise does not apply.

## Group 3 — construction sites (4 cases)

`new-service-missing-interface-field`, `new-service-missing-pointer-field`, `new-service-explicit-nil`, `new-service-by-assignment`.

The panic lands far from the bug. A composite literal omits a pointer or interface field, or `new(T)` is followed by a partial set of assignments, and the struct travels out of the constructor with a nil field inside it.

**Built:** a construction-site check (`analyzer/construction.go`), over a narrow policy for which types it applies to: a struct is a wiring struct only when every one of its fields is pointer or interface typed. That policy is what keeps it off legitimate partially-initialised structs that mix value and pointer fields. A composite literal, or `new(T)`/`&T{}` followed by field assignments, that sets at least one field but leaves another unset or explicitly `nil` is reported at the construction site, not at a later dereference: `service is constructed with dep, logger left nil`. A completely empty literal is not reported by this rule, that stays the ordinary `o.p may be nil` case downstream.

**Prior art:** `exhaustruct` reports 2 of the 4 (the two omitted-key literals) and cannot reach the other two: an explicit `nil` value is a present key, and `new(T)` has no literal to inspect. Measured against this corpus `exhaustruct` also produced **4 false positives** on legitimate zero-value literals, which is what makes it noisy in review.

The plan was to do better than `exhaustruct` by pairing the rule with the fact system: export a fact from the constructor saying which fields it leaves nil, and report at the dereference in the consumer rather than at the literal. That turned out not to be necessary. The all-pointer-or-interface-fields policy is narrow enough on its own to report at the construction site directly with no false positives on intentional zero values, so Group 3 shipped without depending on Group 4's fact plumbing.

## Group 4 — interprocedural nil returns (4 cases)

`deref-func-result`, `deref-not-found-result`, `find-or-nil`, `return-nil-nil`.

A function returns a nil pointer on some path; the caller dereferences the result without checking. `find-or-nil` and `return-nil-nil` are the `(nil, nil)` shape specifically: a nil error does not imply a non-nil value, so the caller's `if err != nil` guard proves nothing.

**Built:** the `may return nil result N` object fact (`analyzer/results.go`), exported by the callee and imported by the caller, the same mechanism the validator rule already uses, pointed at return values instead of receiver fields. It is exported for a function that returns a literal `nil` in a pointer, interface, map, chan or signature-typed result on some path where its error result, if it has one, is also `nil` (slices are excluded, a nil slice is an ordinary empty value, not a missing one). A caller that dereferences that result through `checkKnownNil`, having assigned it to a local, is reported.

`find-or-nil` and `return-nil-nil` are also the *producer* side: a return statement whose error result is nil while another result is a literal nil of one of those kinds is reported directly, `nil value returned with a nil error` (`analyzer/returns.go`). This was kept in scope and always on rather than split behind a flag or into another linter; see the closing note below.

## Group 5 — interfaces (3 cases)

`nil-interface-call`, `typed-nil-in-interface`, `unchecked-type-assert`.

`typed-nil-in-interface` is the one that matters: an interface holding a nil pointer is itself non-nil, so `d != nil` passes and the call still panics. No syntactic guard can catch it.

**Built:** none of the three needed SSA in the end. `nil-interface-call` (`o.iface.Do()`) is an ordinary field-path selector on an interface-typed field, already covered by Group 1's field-kind table. `unchecked-type-assert` is a single-form type assertion result, marked in `scope.nilable` by the same Group 2 dataflow that handles a declared-but-unassigned pointer. `typed-nil-in-interface` looked like it needed the dynamic type and value of an interface value, which is genuinely an SSA question in general, but the corpus case is narrower: assigning a nil pointer identifier to an interface-typed local is visible syntactically at the assignment, so `scope.nilable` marks the local `typedNil` there and reports it as holding a nil pointer rather than being nil. That covers the shape this analyzer cares about without a wholesale migration to `buildssa`.

## Group 6 — embedding, indirection, concurrency (5 cases)

- `promoted-field-on-nil-embedded` — `o.e` reads through the nil embedded `*embedded`. The path is implicit; resolving it needs `types.LookupFieldOrMethod` to see that the selector traverses an embedded pointer. Small, self-contained, worth doing early.
- `double-pointer`, `double-pointer-chain` — `(*pp).n` dereferences `pp` and then `*pp`. Requires treating `*x` as a path segment, which the current path canonicalisation does not do. Also small.
- `guard-then-goroutine` — the guard holds when the goroutine is created, not when it runs. Requires knowing that a path captured by an escaping closure can be written by another goroutine, so the guard does not survive. This is a concurrency rule with real false-positive risk on single-threaded uses.
- `guard-then-loop-reassign` — a loop body assigns `o.p = v` from a slice that may hold nil, invalidating the guard that dominates the loop. The analyzer already drops proofs on assignment; it does not consider that a loop body executes zero or more times, so the write inside it never invalidates the proof outside it. This is a fix, not a feature.

**Built:** all four. `promoted-field-on-nil-embedded` is `checkPromotedField` in `analyzer/expr.go`, which walks `types.Selection.Index()` to check every intermediate embedded pointer a promoted selector traverses, not just the field it finally reaches. `double-pointer`/`double-pointer-chain` treat `*x` as its own path segment (the `useStar` use kind plus canonical-path handling), so `pp` and `(*pp)` are checked as two distinct paths. `guard-then-goroutine` is goroutine scope: a closure literal launched with `go` walks with a scope that carries the enclosing field-path proofs stripped out, since a guard that held when the goroutine was created says nothing about when it runs; a bare local's `nilable` state is unaffected, since a local cannot be reassigned out from under the closure the way a shared field can. `guard-then-loop-reassign` is the general invalidation fix: a write to a path anywhere inside a loop body, a non-exiting `if` branch, or a switch/select clause drops that path's proof after the statement, because none of those bodies are guaranteed to run, or to run only once.

## Group 7 — error shapes (1 case)

`swallow-error` — the error is checked and then discarded. Not a nil-dereference finding at all; it is here because it is one of the ways a nil ends up somewhere it should not. It belongs in a different linter.

**Built:** kept in this linter and always on rather than split out. `checkSwallowedError` (`analyzer/returns.go`) reports a `return` reached only through a branch that already checked an error, naming the error, when that return would let it vanish instead of propagating it. See the closing note below for the case against splitting it out.

## False positives fixed (2 cases)

- `guarded-by-assert` — `-assert-package` matched an **imported** package, so an assert helper declared in the package under analysis was invisible. **Built:** superseded rather than patched. `-assert-package` is gone entirely; an assert helper is now recognised from its own body as the `asserts argument N` fact (`analyzer/assert.go`), regardless of which package declares it or whether that package is imported by the code under analysis. This is the case the billing-backend hit in practice, and it is now structurally impossible: there is no package name to match.
- `guarded-switch-nil-case` — a tagless `switch` whose first case returns on nil is a guard, and only `if`/`else` and early returns were read as guards. **Built:** a tagless `switch { case x == nil: <terminates> }` and a tagged `switch x { case nil: <terminates> }` are read as guards the same way `if x == nil { <terminates> }` is, including later clauses and the code after an exiting clause.

Both were pure precision fixes with no scope change, and were done first, a false positive costs more review trust than a missed case.

## Build order

This is the order the work was actually done in, close to but not identical to the plan above.

1. **Precision fixes**, including recognising an assert helper as a fact from its own body instead of matching an imported package name, which removed `-assert-package` entirely.
2. **Switch guards** (tagless and tagged, as dominance-equivalent to `if`/`else`) **plus branch invalidation**, so a write inside a loop body, a non-exiting `if`, or a switch/select clause drops the proof after that statement.
3. **Field-path kinds beyond plain selection**: explicit star paths (`double-pointer`), promoted fields through an embedded pointer, and the map/slice/chan/func use-kind table applied to field paths.
4. **Local nil-state** (`scope.nilable`) for a bare local with a visible nil origin, plus the `nil-safe receiver` fact so a call through a known-nil local is silent when the callee tolerates it.
5. **Nil-result facts** (`may return nil result N`) and the two return-time rules, `nil value returned with a nil error` and `err is discarded`.
6. **Construction checks**: composite literals and `new(T)` plus partial field assignment, on wiring structs (structs whose fields are all pointer or interface typed).
7. **Goroutine scope**: a closure launched with `go` does not inherit the field-path proofs that held at the point it was created.

Step 4 turned out to cover Group 5's `typed-nil-in-interface` and `unchecked-type-assert` as well, so the SSA migration considered in the original plan never happened: nothing here needed `buildssa`, and the reason this project exists is that NilAway's SSA-based approach was too slow and too noisy for the repository that motivated it.

## The part that was not a coverage question

`swallow-error` and the `(nil, nil)` producers (`find-or-nil`, `return-nil-nil`) could have been argued out of scope entirely, as return-time or error-handling rules rather than nil-dereference findings in the strict sense. They were kept in scope and are always on, the same as every other rule here, no flag turns them off. If either proves noisy in real code, most plausibly `err is discarded` firing on a return that deliberately maps one error to another, they can be revisited and split out to their own check or a different linter.
