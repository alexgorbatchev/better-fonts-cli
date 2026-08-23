---
created_on: 2026-08-23 07:30
last_modified: 2026-08-23 07:45
status: current
---

# better-fonts-cli

Go CLI tool for modular patching and unpatching of macOS Electron and Native applications with custom fonts.

## Commands
- **Build Local Binary:** `just build` (compiles strictly to `bin/better-fonts`)
- **Install Binary:** `just install` (`go install ./cmd/better-fonts`)
- **Run CLI with Arguments:** `just run <args>` (`go run ./cmd/better-fonts <args>`)
- **Upgrade CLI Binary:** `better-fonts upgrade`
- **Run Tests:** `just test` (`go test -race ./...`)
- **Run Linter / Static Analysis:** `just lint` (`go mod tidy -diff` && `go vet ./...`)
- **Run Vet:** `just vet` (`go vet ./...`)
- **Format Code:** `just fmt` (`go fmt ./...`)
- **Clean Artifacts:** `just clean` (`rm -rf bin/ .tmp/ coverage.out`)

## Setup & Environment
- **Prerequisites:** Go 1.26+, macOS, `clang` (for native CoreText dylib compilation), and `just`.
- **Zero Node/NPM Dependency:** Both ASAR archive processing and Electron fuse flipping (`EnableEmbeddedAsarIntegrityValidation=off`) are implemented in pure native Go.
- **Configuration Directory:** Follows XDG Base Directory specification (`$XDG_CONFIG_HOME/better-fonts/config.toml`, fallback `~/.config/better-fonts/config.toml`).
- **Temporary Files:** Temporary file operations use `.tmp/` within the project root or user cache directory.

## Conventions
- **Dual-Driver Architecture:**
  - **`electron`**: In-memory ASAR modification (`internal/asar`), preload script injection, Electron integrity fuse disabling (`EnableEmbeddedAsarIntegrityValidation=off`), and ad-hoc codesigning.
  - **`native-hook`**: Native Objective-C/C `DYLD_INTERPOSE` dynamic library injection (`internal/driver/native`), wrapper launcher compilation via `clang`, executable substitution (`<binary>.orig`), and ad-hoc codesigning.
- **Unified Patch & Unpatch Commands:** `better-fonts patch [apps...]` and `better-fonts unpatch [apps...]` handle all configured apps, built-in apps, and arbitrary `.app` file paths seamlessly.
- **XDG Base Directory Compliance:** User settings strictly resolve via `$XDG_CONFIG_HOME` with standard fallbacks.
- **Version Flag Contract:** `--version` MUST return ONLY the raw version string (e.g. `1.0.0\n` or `dev\n`) without application name or labels. GoReleaser injects this via `-X main.version={{.Version}}`.
- **Release Automation:** Releases are driven via `.goreleaser.yml` and triggered on version tags (`v*`) in `.github/workflows/release.yml`.
- **Hermetic & Offline Unit Tests:** Unit tests (`just test`) MUST remain hermetic and deterministic using synthetic test fixtures in `t.TempDir()`.
- **Atomic Operations & Backups:** Never modify production `.app` bundles destructively without preserving `.orig` or staging copies.

## Boundaries
- **Always:** Run `just test` and `just lint` before finishing any task or committing changes.
- **Always:** Place compiled binaries strictly into `bin/better-fonts` and keep `bin/` git-ignored.
- **Always:** Use Cobra (`github.com/spf13/cobra`) for CLI commands and flag parsing.
- **Never:** Commit compiled Go binaries, temporary directories (`.tmp/`), or `.DS_Store` files to git.
- **Never:** Use heredocs in bash commands or scripts.

## References
- `README.md` — Project overview, installation, CLI usage, and configuration schema.
