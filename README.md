# kcm — Kubernetes Kubeconfig Manager

A lightweight, cross-platform utility for managing Kubernetes kubeconfig files, contexts, clusters, users, and namespaces — with three interchangeable interfaces sharing a single core library.

```
Single static binary  ·  < 20 MB  ·  < 30 MB idle RAM  ·  < 100 ms startup
macOS (Intel + Apple Silicon)  ·  Linux (amd64 + arm64)
```

## Interfaces

| Interface | Launch |
|-----------|--------|
| **CLI** | `kcm ls`, `kcm use prod`, … |
| **TUI** | `kcm` or `kcm tui` |
| **Web dashboard** | `kcm serve` → http://127.0.0.1:8585 |

## Install

### Homebrew (macOS / Linux)
```bash
brew install odhomane/tap/kcm
```

### One-line script
```bash
curl -sSL https://raw.githubusercontent.com/odhomane/kcm/main/install.sh | sh
```

### Manual
Download the binary for your platform from [Releases](https://github.com/odhomane/kcm/releases), then:
```bash
chmod +x kcm && sudo mv kcm /usr/local/bin/
```

### Build from source
```bash
git clone https://github.com/odhomane/kcm
cd kcm
make install
```

## Shell integration (per-shell context switching)

Add to `~/.zshrc` or `~/.bashrc`:
```bash
eval "$(kcm shell-init zsh)"   # or bash / fish
```

This writes to a per-shell temp file so context switches don't affect other terminals.

## Command reference

```
kcm                          # interactive TUI picker
kcm ls                       # list all contexts (table)
kcm ls --output wide         # more columns
kcm ls --output json         # machine-readable
kcm ls --group prod          # filter by group

kcm use <context>            # switch context (fuzzy match OK)
kcm use -                    # switch to previous context

kcm ns                       # list namespaces for current context
kcm ns staging               # switch namespace
kcm ns -                     # previous namespace

kcm rename <old> <new>       # rename (reference-safe)
kcm delete <ctx> [--cascade] # delete context (cascade removes orphaned cluster/user)
kcm duplicate <src> <dst>    # duplicate context

kcm group set <ctx> <group>  # assign to group
kcm group ls                 # list all groups

kcm label <ctx> key=val ...  # set labels
kcm color <ctx> <color>      # set display color
kcm pin <ctx>                # pin as favourite
kcm pin <ctx> --unpin        # remove pin

kcm import <path|url|->      # import from file, URL, or stdin
kcm export <ctx> [-o file]   # export standalone kubeconfig
kcm export <ctx> --redact    # strip sensitive fields
kcm export <ctx> --canonical # deterministic YAML for git diff

kcm merge <src...> -o <dst>  # merge kubeconfigs
kcm split [-o dir]           # split into one file per context

kcm validate [path]          # lint kubeconfig(s)
kcm diff <ctx1> <ctx2>       # diff two contexts
kcm diff --files <f1> <f2>   # diff two files

kcm health                   # bulk cluster ping (parallel, 3s timeout)
kcm health <ctx>             # single cluster health check

kcm backup ls                # list backups
kcm backup restore <id>      # restore a backup
kcm undo                     # undo last reversible operation

kcm serve                    # web dashboard on :8585
kcm serve --port 9090 --no-browser

kcm watch                    # live status, refreshing every 5s

kcm completion zsh           # shell completion script
kcm shell-init zsh           # per-shell context switching

kcm version
```

## Architecture

```
kcm/
├── cmd/kcm/               # entry point
├── internal/
│   ├── core/              # kubeconfig parsing, merging, validation, backup
│   │                      # (uses k8s.io/client-go/tools/clientcmd)
│   ├── store/             # SQLite metadata: groups, labels, colors,
│   │                      # last-used, audit log, undo stack, profiles
│   │                      # (modernc.org/sqlite — pure Go, no CGO)
│   ├── ops/               # all mutations: switch, rename, delete, merge,
│   │                      # split, import, export, health, namespace ops
│   ├── cli/               # cobra command implementations
│   ├── tui/               # bubbletea + lipgloss TUI
│   └── gui/               # net/http + embed.FS dashboard
│       └── static/        # Alpine.js + hand-rolled CSS (no CDN)
└── go.mod
```

All three interfaces call into `internal/ops` — logic is never duplicated.

## Key design decisions

- **Atomic writes**: every kubeconfig write goes to `<file>.kcm.tmp` then `rename()` — no partial writes.
- **Non-destructive by default**: original files are unchanged unless you explicitly request a mutation. Backup is always taken first.
- **Pure Go SQLite** (`modernc.org/sqlite`): no CGO, no system libraries needed → single static binary everywhere.
- **No CDN**: Alpine.js (~44 KB) is embedded in the binary. The dashboard works fully offline.
- **Auth**: the web dashboard generates a one-time random token printed to stdout. Required as cookie or `Authorization: Bearer <token>` header.

## Configuration

`~/.config/kcm/config.yaml`:
```yaml
# Additional kubeconfig files to always load.
extra_kubeconfigs:
  - ~/.kube/work.yaml
  - ~/.kube/eks-prod.yaml

# Default output format.
output: table   # table | wide | json | yaml
```

## Development

```bash
make build        # build for host
make test         # run tests with -race
make test-cover   # coverage HTML report
make lint         # golangci-lint
make cross        # all four platforms
make release-snapshot  # local goreleaser snapshot
```

## License

MIT
