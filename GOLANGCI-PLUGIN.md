# Evaluation: shipping nilfield as a golangci-lint plugin

Verified against the golangci-lint documentation and source on 2026-09-02: the module plugin docs, `github.com/golangci/plugin-module-register`, the `example-plugin-module-linter` reference plugin, and `pkg/goanalysis` in golangci-lint itself.

## The two plugin systems

golangci-lint accepts custom linters two ways.

**Go plugin system** — build the analyzer as a `.so` with `-buildmode=plugin` and point `settings.custom.<name>.path` at it. It inherits every constraint of Go's `plugin` package: the plugin and the golangci-lint binary must be built with the identical Go toolchain version, the identical versions of every shared dependency (`golang.org/x/tools` above all), and the identical build flags. It does not work on Windows. In practice it breaks on every golangci-lint upgrade. The project's own documentation steers users away from it.

**Module plugin system** — declare the analyzer as a Go module in `.custom-gcl.yml`, run `golangci-lint custom`, and get a binary with the analyzer compiled in. This is the supported path and the only one worth considering.

## What the module plugin system actually requires

The contract is one small interface, from `github.com/golangci/plugin-module-register/register`:

```go
type LinterPlugin interface {
	BuildAnalyzers() ([]*analysis.Analyzer, error)
	GetLoadMode() string
}
```

A plugin registers itself in `init()` and decodes its own settings:

```go
func init() {
	register.Plugin("nilfield", New)
}

func New(settings any) (register.LinterPlugin, error) {
	s, err := register.DecodeSettings[Settings](settings)
	if err != nil {
		return nil, err
	}

	return &plugin{settings: s}, nil
}
```

`GetLoadMode()` returns `register.LoadModeSyntax` or `register.LoadModeTypesInfo`. `nilfield` resolves field types and looks up methods, so it needs `LoadModeTypesInfo`.

`DecodeSettings` round-trips the configuration through JSON with unknown fields rejected, so the settings struct needs `json` tags and the user's YAML keys must match exactly.

The user builds the binary from `.custom-gcl.yml`:

```yaml
version: v2.13.2
name: custom-gcl
destination: .
plugins:
  - module: 'github.com/Orfeo42/nilfield'
    version: v0.1.0
```

`version` (the golangci-lint version to build against) and each plugin's `module` are required; `name` defaults to `custom-gcl`, `destination` to `.`, and a plugin entry takes `version` for a module from the proxy or `path` for a local checkout, plus `import` when the registering package is not the module root.

Then `golangci-lint custom` produces `./custom-gcl`, and the linter is enabled like any other:

```yaml
version: "2"

linters:
  enable:
    - nilfield
  settings:
    custom:
      nilfield:
        type: module
        description: reports a nil pointer or a nil interface being dereferenced
        settings:
          exclude-paths: internal/dao/
```

## What has to change in this repository

Four changes, none of them large.

1. **Done.** The analyzer moved out of `package main`. `Analyzer` and `run` now live in `analyzer/` (`github.com/Orfeo42/nilfield/analyzer`, exported `analyzer.Analyzer` and `analyzer.New(analyzer.Config{...})`), and `cmd/nilfield/main.go` holds the four-line `singlechecker.Main`. The standalone binary keeps working: `go install github.com/Orfeo42/nilfield/cmd/nilfield@latest`.

2. **Done.** Configuration now comes from a struct, not from package-level flag variables. `analyzer.Config` holds `ExcludePaths` (the only setting left, see below), `analyzer.New(cfg)` builds an analyzer whose `Flags` write into that instance for the CLI, and a plugin can construct a `Config` from `DecodeSettings` the same way. The test seam survived the change.

3. **Add the plugin package.** One file implementing the interface above, in its own package so the standalone binary does not carry the `plugin-module-register` dependency.

4. **Pin `golang.org/x/tools`.** `golangci-lint custom` compiles the plugin into the golangci-lint binary, so both link one copy of `x/tools`. A plugin pinned far from golangci-lint's own version will either fail to build or silently take golangci-lint's version through MVS. Test the plugin against the golangci-lint version named in `.custom-gcl.yml`, not just against whatever `go.mod` resolves.

## Facts across packages: verified supported

This was the real risk, because `nilfield`'s validator rule depends on it. The analyzer declares `FactTypes`, and it proves a guard by importing a fact exported from another package's `validate` method — if golangci-lint's runner dropped facts at package boundaries, the linter would lose that rule silently and start reporting correct code.

It does not. `pkg/goanalysis/runner_action_cache.go` special-cases `len(act.Analyzer.FactTypes) == 0`, persists object facts for analyzers that declare them, and rebuilds facts re-exported from dependencies in memory when loading a cached action. Fact propagation is implemented and cached deliberately.

That is source-level confirmation, not an end-to-end test. Before committing to the plugin, build a `custom-gcl` and run it over `testdata/src/validator`, which is exactly the cross-package validator case. If the analyzer under golangci-lint reports what it reports under `analysistest`, the mechanism holds.

## The cost, stated plainly

The plugin does not run on a stock golangci-lint binary. Every consumer — every developer machine, every CI job, the editor integration — has to build and use `custom-gcl` instead. Concretely:

- CI installs golangci-lint, then runs `golangci-lint custom`, then runs `./custom-gcl run`. That is an extra compile of golangci-lint itself, minutes per job, unless the custom binary is built once and published as an artifact or baked into the CI image.
- Editor integrations point at `golangci-lint` by name. Each developer reconfigures, or the custom binary is installed under that name, which then shadows the real one.
- A golangci-lint upgrade is now a two-sided upgrade: bump `version:` in `.custom-gcl.yml` and rebuild, and reconcile `x/tools` if it moved.

Against that, what the plugin buys is: one binary instead of two in CI, findings in golangci-lint's output format, and `nolint` directives working on `nilfield` findings the same way they work everywhere else.

## The alternative that costs nothing

The analyzer is a standard `go/analysis` analyzer. `singlechecker` already gives it the `-vettool` protocol:

```sh
go vet -vettool=$(which nilfield) ./...
```

It runs as its own step, needs no custom golangci-lint, breaks on no upgrade, and installs with `go install`. That is what the billing-backend does today, through a `just lint-nilfield` recipe and a dedicated CI job, and it works.

## Recommendation

**Keep shipping the standalone binary and `-vettool`; defer the plugin.**

Steps 1 and 2 are done, and they were worth doing regardless of the plugin question: splitting the analyzer out of `package main` and taking configuration from a struct is what makes the analyzer importable, which is the precondition for `-vettool`, for the plugin, and for anyone embedding it in their own multichecker.

Step 3, the plugin package, is cheap to add on top of that and does not have to be adopted to exist. Add it when there is a consumer who wants findings inside golangci-lint's output badly enough to build `custom-gcl` in CI.

The ordering argument used to be coverage, not effort: early on, packaging a linter that missed a large share of the corpus more conveniently would not have made it more useful. That argument no longer applies. The corpus is fully covered within its scope, **43 of 43 hazard cases (47 of 47 sites)**, with zero false positives, and releases now ship prebuilt binaries for every consumer to download directly. So the analyzer's problem is no longer what it catches, and the packaging question is no longer gated on effort either. What is left is purely the consumer-cost trade-off laid out above: a plugin costs every consumer a `custom-gcl` build and a two-sided upgrade path, in exchange for one binary in CI, golangci-lint's output format, and working `nolint` directives. Decide step 3 on that trade-off alone, whenever there is a consumer who wants it badly enough; until then, keep shipping the standalone binary and `-vettool`.

One thing that is no longer a caveat: `nilfield`'s scope is a nil pointer or a nil interface being dereferenced, and nothing else. It does not track a nil map, slice, channel or func value at all, so it does not overlap with what `govet`'s `nilness` and `nilfunc` passes already cover in a default golangci-lint run. Whatever the packaging, `nilfield` is a plain addition to what golangci-lint already runs, not a partial replacement for either `govet` pass.
