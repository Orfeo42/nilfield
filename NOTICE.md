# Notice

The `custom-gcl-<os>-<arch>` release assets are golangci-lint v2.13.2 with the `nilfield` analyzer from this repository compiled in as a module plugin, via `golangci-lint custom` and the `.custom-gcl.yml` in this repository. They are published for `linux`, `darwin` and `windows` on `amd64` and `arm64`.

The `nilfield` analyzer source in this repository is MIT-licensed, see `LICENSE`.

golangci-lint is licensed under GPL-3.0. Because each `custom-gcl-<os>-<arch>` asset statically links golangci-lint with this plugin, the combined binary is a derivative work and is distributed under GPL-3.0.

Corresponding source:

- this repository, `github.com/Orfeo42/nilfield`, for the plugin
- `github.com/golangci/golangci-lint`, for golangci-lint itself
