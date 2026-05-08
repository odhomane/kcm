# Changelog

All notable changes to `kcm` will be documented in this file.
Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).
Versioning follows [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- Core kubeconfig parsing and multi-file discovery (`$KUBECONFIG`, `~/.kube/config`, `~/.kube/configs/`)
- SQLite metadata store for groups, labels, colors, pins, last-used timestamps (pure-Go, no CGO)
- Atomic writes via temp-file + rename; auto-backup before every destructive operation
- Up to 50 rotating backups per file in `~/.config/kcm/backups/`
- Audit log for every mutation in SQLite
- Undo stack for switch and rename operations
- CLI commands: `ls`, `use`, `ns`, `rename`, `delete`, `duplicate`, `group`, `label`, `color`, `pin`, `import`, `export`, `merge`, `split`, `validate`, `diff`, `health`, `backup`, `undo`, `serve`, `tui`, `watch`, `shell-init`, `completion`, `version`
- Fuzzy context matching for `kcm use`
- TUI with two-pane layout (bubbletea + lipgloss)
- Web dashboard on port 8585 with one-time token auth
- Server-Sent Events for live kubeconfig file watching via fsnotify
- Cloud provider auto-detection (AWS EKS, Azure AKS, GCP GKE, DigitalOcean)
- Context export with `--redact` for safe sharing
- Canonical deterministic YAML export
- Shell integration snippet (`eval "$(kcm shell-init zsh)"`)
- Shell completions for bash, zsh, fish, powershell
- Cross-platform builds: darwin/amd64, darwin/arm64, linux/amd64, linux/arm64
- GoReleaser config + Homebrew formula
- GitHub Actions CI (test on macOS + Linux) and release workflow
- Unit tests for `internal/core` and `internal/store` with table-driven fixtures
