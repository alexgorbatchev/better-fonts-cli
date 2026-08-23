# better-fonts

`better-fonts` is a modular, configurable macOS command-line tool for patching and unpatching Electron and Native macOS applications to use custom fonts.

## What It Does

- **Dual-Driver Architecture**: Supports both **Electron** apps (via preload script CSS injection) and **Native Cocoa / Qt / C++** apps (via `DYLD_INTERPOSE` CoreText hooking).
- **Custom Font Injection**: Intercepts font creation and renders entire user interfaces in your chosen monospace or proportional font.
- **Bi-directional Patching & Unpatching**: Safely applies, replaces, or completely removes font patches without corrupting application bundles.
- **Native ASAR Engine**: Modifies Electron archive packages in-memory, preserving `.unpacked` native binaries (`.node`, `.dylib`, `.wasm`) without relying on external extraction tools.
- **Dynamic Native Hooking**: Compiles and injects lightweight CoreText interposition dylibs with native launcher wrappers on the fly.
- **Electron Fuses & macOS Codesigning**: Automatically disables `EnableEmbeddedAsarIntegrityValidation` fuses and re-signs modified `.app` bundles with ad-hoc signatures.
- **XDG Base Directory Compliance**: Stores user preferences in `$XDG_CONFIG_HOME/better-fonts/config.toml` (`~/.config/better-fonts/config.toml`).

## Supported Applications

`better-fonts` includes built-in support for:

| Application | Driver | Default Path | Method |
| :--- | :--- | :--- | :--- |
| **Paseo** | `electron` | `/Applications/Paseo.app` | Preload CSS (`dist/preload.js`) |
| **Signal** | `electron` | `/Applications/Signal.app` | Preload CSS (`preload.bundle.js`) |
| **Slack** | `electron` | `/Applications/Slack.app` | Preload CSS (`dist/preload.bundle.js`) |
| **Rekordbox** | `native-hook` | `/Applications/rekordbox 7/rekordbox.app` | CoreText Interposition |
| **Engine DJ** | `native-hook` | `/Applications/Engine DJ.app` | CoreText Interposition |
| **Telegram** | `native-hook` | `/Applications/Telegram.app` | CoreText Interposition |

*Custom applications can also be declared in `config.toml` or patched on demand.*

## Prerequisites

- **macOS** (Apple Silicon or Intel)
- **Xcode Command Line Tools (`clang`)** (only needed when compiling native CoreText hooks)
- Standard macOS utilities: `codesign`, `osascript`, `open`, `plutil`

*Note: `better-fonts` is completely self-contained with a native ASAR parser and native Electron fuse manipulator in Go. Node.js, Bun, and npm are **not** required.*

## Installation

Go to the [Latest Release Page](https://github.com/alexgorbatchev/better-fonts-cli/releases/latest) and download the pre-compiled archive for your operating system. Extract and move the binary to your `PATH`:

```bash
tar -xzf better-fonts_*.tar.gz
sudo mv better-fonts /usr/local/bin/
```

### From Source (Optional)

Ensure Go 1.26+ and `just` are installed:

```bash
git clone https://github.com/alexgorbatchev/better-fonts-cli.git
cd better-fonts-cli
just build
cp bin/better-fonts /usr/local/bin/
```

## Quick Start

### 1. Check Application Status

Inspect all supported applications to see if they are installed, which driver is used, whether they are patched, and what font is currently applied:

```bash
better-fonts status
```

Sample Output:
```
APP         DRIVER        INSTALLED   PATCHED   CURRENT FONT           PATH
---         ------        ---------   -------   ------------           ----
Paseo       electron      Yes         Yes       Maple Mono Normal NF   /Applications/Paseo.app
Signal      electron      Yes         Yes       SF Mono                /Applications/Signal.app
Slack       electron      Yes         Yes       Maple Mono Normal NF   /Applications/Slack.app
Rekordbox   native-hook   Yes         Yes       Maple Mono Normal NF   /Applications/rekordbox 7/rekordbox.app
Engine DJ   native-hook   Yes         Yes       Maple Mono Normal NF   /Applications/Engine DJ.app
Telegram    native-hook   Yes         No        -                      /Applications/Telegram.app
```

### 2. Patch Configured Applications

Patch all applications specified in `config.toml` (default is all supported apps):

```bash
better-fonts patch
```

### 3. Patch Specific Applications

Target one or more specific applications by name, ID, or `.app` bundle path:

```bash
better-fonts patch slack
better-fonts patch rekordbox engine-dj telegram
better-fonts patch /Applications/SomeCustomApp.app
```

### 4. Override Font from CLI

Apply a specific font without editing the configuration file:

```bash
better-fonts patch --font "JetBrains Mono" slack
better-fonts patch --font "Fira Code" telegram
```

### 5. Remove Font Patch (Unpatch)

Restore original preload scripts or executables across all or targeted applications:

```bash
better-fonts unpatch
better-fonts unpatch rekordbox
better-fonts unpatch /Applications/SomeCustomApp.app
```

### 7. Self-Upgrade

Check for and install the latest binary release directly from GitHub:

```bash
better-fonts upgrade
```

### 8. List Supported Applications

```bash
better-fonts list
```

## Configuration

`better-fonts` reads its configuration from `$XDG_CONFIG_HOME/better-fonts/config.toml` (defaults to `~/.config/better-fonts/config.toml`).

If the configuration file does not exist, running `better-fonts` creates a default one automatically.

### View Configuration Path & Contents

```bash
# Print configuration file location
better-fonts config path

# Display current configuration
better-fonts config show
```

### Sample `config.toml`

```toml
# Default font applied to patched applications
font = "Maple Mono Normal NF"

# Applications to patch by default.
# Use ["*"] for all supported apps, or list specific IDs: ["slack", "rekordbox", "telegram"]
apps = ["*"]

# Automatically restart applications after patching/unpatching
restart = true

# Per-application overrides
[apps_config.slack]
font = "JetBrains Mono"
restart = true

[apps_config.rekordbox]
font = "Maple Mono Normal NF"

# User-defined custom apps
[[custom_apps]]
id = "custom-electron"
name = "Custom Electron App"
path = "/Applications/CustomElectron.app"
driver = "electron"
process_name = "CustomElectron"
asar_path = "app.asar"
preload_path = "dist/preload.js"

[[custom_apps]]
id = "custom-native"
name = "Custom Native App"
path = "/Applications/CustomNative.app"
driver = "native-hook"
process_name = "CustomNative"
```

## Options & Flags

Global flags apply to all commands:

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--config` | `-c` | `~/.config/better-fonts/config.toml` | Custom path to configuration TOML file |
| `--font` | `-f` | *(from config)* | Override font name |
| `--app` | `-a` | *(from config)* | Target specific app(s) (e.g. `-a slack,rekordbox`) |
| `--restart` | | `true` | Restart application after patching or unpatching |
| `--no-restart` | | `false` | Prevent application restart after operations |
| `--dry-run` | | `false` | Simulate actions without modifying disk files |
| `--verbose` | `-v` | `false` | Enable verbose operational logging |
| `--version` | | | Print version string |

## Development

```bash
# Build binary into bin/better-fonts
just build

# Run CLI with arguments
just run status

# Run unit tests with race detection
just test

# Check code hygiene and run vet
just lint

# Clean build artifacts and temporary files
just clean
```

## License

This project is licensed under the [MIT License](LICENSE).
