# Requirements: catching every case in the corpus

The corpus in `testdata/src/nilcases` holds **56 hazard cases across 62 hazard sites**, plus **17 safe cases** that must stay silent. `TestCorpus` measures the analyzer against it on every run and prints the figure.

Today: **27 of 56 cases fully covered (30 of 62 sites)**, with **2 false positives**. Every one of those 31 defects is a failing subtest, so the suite is red until the corpus is fully covered; the failure list is the work list.

This document says what each remaining case actually demands, grouped by the capability it needs rather than by corpus section, because the grouping is what decides the build order.

## What the analyzer does today

One intraprocedural pass over the AST. It tracks paths rooted at a **struct field** (`o.p`, `s.repo`, `o.p.next`), and reports a dereference that no guard dominates. A guard is an `x != nil` condition in any boolean form, an early return or panic in the opposite branch, an assignment of an address or `new(...)`, a call to an `Assert` helper from the package named by `-assert-package`, or a checked call to a validator method whose non-nil guarantee travels as an analysis fact.

Two properties follow from that design and explain most of the gap list:

- **Only struct fields are roots.** A bare local or parameter is deliberately out of scope, on the theory that a missing guard there is hard to miss. That single decision accounts for 11 of the 29 gaps.
- **Only pointer-typed paths are hazards.** A nil map, slice, channel, func or interface value is not tracked at all, which accounts for another 8.

## Group 1 — value-kind nils (8 cases)

`nil-map-write`, `nil-map-write-on-field` (the `.m` half), `nil-slice-index`, `nil-channel-send`, `nil-channel-close`, `call-nil-func-var`, `call-nil-func-field` (the `fn` half), `compare-declared-func-to-nil`.

The hazard is not a pointer dereference. Writing to a nil map panics while reading it does not; indexing a nil slice panics while appending to it does not; sending on a nil channel blocks forever and closing one panics; calling a nil func value panics.

**What it needs:** the same path tracking already in place, applied to map, slice, channel and func-typed paths, with a per-kind table of which operations are fatal. No new analysis machinery.

**Prior art:** `govet`'s `nilness` and `nilfunc` passes already report 5 of these 8 — nil map update, nil slice index, nil channel send, nil dynamic func call, and `f != nil` on a declared func. They are enabled by default in golangci-lint, and measured against this corpus they overlap with `nilfield` on **zero** sites. Implementing this group duplicates work that is already free. Do it only for the field-rooted halves (`o.p.m`, `o.p.fn`), which `nilness` cannot see because it starts from SSA values, not paths.

## Group 2 — locals and parameters as roots (11 cases)

`nil-slice-index`, `nil-channel-send`, `nil-channel-close`, `call-nil-func-var`, `call-on-nil-receiver`, `typed-nil-in-interface`, `nil-map-write`, `nil-error-message`, `deref-func-result`, `missing-map-pointer-value`, `nil-element-in-slice`.

Every one of these dereferences a local variable, a parameter, or the result of an expression rather than a struct field.

**What it needs:** widening the root set. Mechanically small, behaviourally the largest change in this document: a parameter is non-nil in most call sites the analyzer cannot see, so reporting every unguarded parameter dereference produces a diagnostic on a large fraction of all Go functions ever written. This is why the scope was drawn at struct fields in the first place.

**The honest options:**

1. **Report only locals whose nil origin is visible in the same function** — `var i *inner` with no assignment, a `map[k]` lookup of pointer type, a slice element, a call to a function known to return nil. That covers `call-on-nil-receiver`, `missing-map-pointer-value`, `nil-element-in-slice`, `nil-slice-index`, `nil-channel-send`, `nil-channel-close`, `call-nil-func-var`, `typed-nil-in-interface` without touching parameters.
2. **Report parameters too**, behind a flag that is off by default. That is the only way to reach `nil-error-message` and it will be noisy.

Option 1 is the one to build. It needs dataflow, not just dominance: knowing that `i` was declared and never assigned, or that `m["k"]` yields the zero value on a miss.

## Group 3 — construction sites (4 cases)

`new-service-missing-interface-field`, `new-service-missing-pointer-field`, `new-service-explicit-nil`, `new-service-by-assignment`.

The panic lands far from the bug. A composite literal omits a pointer or interface field, or `new(T)` is followed by a partial set of assignments, and the struct travels out of the constructor with a nil field inside it.

**What it needs:** a check on composite literals and on `new(T)` followed by field assignments, over struct types whose pointer and interface fields are expected to be wired. It is a different rule from everything else here — it reports the construction, not a dereference — and it needs a policy for which types it applies to, or it fires on every legitimate partially-initialised struct in the codebase.

**Prior art:** `exhaustruct` reports 2 of the 4 (the two omitted-key literals) and cannot reach the other two: an explicit `nil` value is a present key, and `new(T)` has no literal to inspect. Measured against this corpus `exhaustruct` also produced **4 false positives** on legitimate zero-value literals, which is what makes it noisy in review.

To do better than `exhaustruct` the rule has to be paired with the fact system: export a fact from the constructor saying which fields it leaves nil, and report at the dereference in the consumer rather than at the literal. That turns 4 construction cases into precise diagnostics with no false positives on intentional zero values — and it is the same machinery Group 4 needs.

## Group 4 — interprocedural nil returns (4 cases)

`deref-func-result`, `deref-not-found-result`, `find-or-nil`, `return-nil-nil`.

A function returns a nil pointer on some path; the caller dereferences the result without checking. `find-or-nil` and `return-nil-nil` are the `(nil, nil)` shape specifically: a nil error does not imply a non-nil value, so the caller's `if err != nil` guard proves nothing.

**What it needs:** an object fact per function recording which results can be nil, exported by the callee and imported by the caller — the same mechanism the validator rule already uses, pointed at return values instead of receiver fields. The analyzer already declares `FactTypes`, so the plumbing exists.

`find-or-nil` and `return-nil-nil` are worth separating out: reporting the *producer* is a design rule ("do not return `(nil, nil)`"), not a nil-dereference finding. They belong behind their own flag, or arguably in a different linter.

## Group 5 — interfaces (3 cases)

`nil-interface-call`, `typed-nil-in-interface`, `unchecked-type-assert`.

`typed-nil-in-interface` is the one that matters: an interface holding a nil pointer is itself non-nil, so `d != nil` passes and the call still panics. No syntactic guard can catch it.

**What it needs:** tracking the dynamic type and value of interface values — genuinely requires SSA. `unchecked-type-assert` is cheap by comparison: a single-result type assertion to a pointer type yields a value that can be nil, catchable with the Group 2 dataflow.

## Group 6 — embedding, indirection, concurrency (5 cases)

- `promoted-field-on-nil-embedded` — `o.e` reads through the nil embedded `*embedded`. The path is implicit; resolving it needs `types.LookupFieldOrMethod` to see that the selector traverses an embedded pointer. Small, self-contained, worth doing early.
- `double-pointer`, `double-pointer-chain` — `(*pp).n` dereferences `pp` and then `*pp`. Requires treating `*x` as a path segment, which the current path canonicalisation does not do. Also small.
- `guard-then-goroutine` — the guard holds when the goroutine is created, not when it runs. Requires knowing that a path captured by an escaping closure can be written by another goroutine, so the guard does not survive. This is a concurrency rule with real false-positive risk on single-threaded uses.
- `guard-then-loop-reassign` — a loop body assigns `o.p = v` from a slice that may hold nil, invalidating the guard that dominates the loop. The analyzer already drops proofs on assignment; it does not consider that a loop body executes zero or more times, so the write inside it never invalidates the proof outside it. This is a fix, not a feature.

## Group 7 — error shapes (1 case)

`swallow-error` — the error is checked and then discarded. Not a nil-dereference finding at all; it is here because it is one of the ways a nil ends up somewhere it should not. It belongs in a different linter.

## False positives to fix (2 cases)

- `guarded-by-assert` — `-assert-package` matches an **imported** package, so an assert helper declared in the package under analysis is invisible. The fix is to also accept a helper declared locally whose body panics when the condition is false, which the analyzer can determine by inspecting it. This is the case the billing-backend hit in practice.
- `guarded-switch-nil-case` — a tagless `switch` whose first case returns on nil is a guard, and only `if`/`else` and early returns are read as guards today. The fix is to treat `switch { case x == nil: <terminates> }` the same as `if x == nil { <terminates> }`, and to extend the same reading to `switch x := ...; x` forms.

Both are pure precision fixes with no scope change. **Do these first** — a false positive costs more review trust than a missed case.

## Build order

1. **False positives** (2 cases) — cheapest, highest trust return.
2. **Embedded pointers and double indirection** (3 cases) — small, self-contained, no new machinery.
3. **Field-rooted value kinds** (`o.p.m`, `o.p.fn`) — reuses the existing path tracking, and covers the two partial cases in the gap list.
4. **Interprocedural nil-return facts** (4 cases) — unlocks Group 3's precise form as well.
5. **Construction facts** (4 cases) — built on step 4.
6. **Local-rooted dataflow** (Group 2, option 1) — the largest single jump, 8 or more cases.
7. **Interfaces** (3 cases) — needs SSA; decide at this point whether to migrate the whole analyzer onto `buildssa` rather than bolt SSA onto an AST pass.
8. **Concurrency and error shapes** — decide whether they belong in this linter at all.

Steps 1-5 take the figure from 27/56 to roughly 38/56 without leaving the AST. Steps 6-7 are where the design question sits: an SSA-based analyzer is what NilAway is, with the runtime cost and the noise that come with it, and the reason this project exists is that NilAway was too slow and too noisy for the repository that motivated it.

## The part that is not a coverage question

Two entries in the corpus can be argued out of scope entirely rather than implemented: `swallow-error` and the `(nil, nil)` producers. If they move to another linter, the denominator drops from 56 to 53 and the target becomes honest. Decide that before chasing the number.
