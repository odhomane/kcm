package cli

// commands.go contains all remaining subcommands:
//   rename, delete, duplicate, group, label, color, pin,
//   import, export, merge, split, validate, diff, health,
//   backup, undo, serve, tui, watch, shell-init, completion, version.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/odhomane/kcm/internal/core"
	"github.com/odhomane/kcm/internal/ops"
	"github.com/spf13/cobra"
)

// ─── rename ───────────────────────────────────────────────────────────────────

func newRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "rename <old> <new>",
		Short:   "Rename a context",
		Example: "  kcm rename old-name new-name",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := op.Rename(args[0], args[1]); err != nil {
				return err
			}
			fmt.Printf("Renamed %q → %q\n", args[0], args[1])
			return nil
		},
	}
}

// ─── delete ───────────────────────────────────────────────────────────────────

func newDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete <context>",
		Aliases: []string{"rm"},
		Short:   "Delete a context",
		Example: "  kcm delete old-cluster\n  kcm delete old-cluster --cascade",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cascade, _ := cmd.Flags().GetBool("cascade")
			yes, _ := cmd.Flags().GetBool("yes")
			if !yes {
				fmt.Printf("Delete context %q? [y/N] ", args[0])
				var resp string
				fmt.Scanln(&resp)
				if strings.ToLower(resp) != "y" {
					fmt.Println("Aborted.")
					return nil
				}
			}
			if err := op.Delete(args[0], cascade); err != nil {
				return err
			}
			fmt.Printf("Deleted context %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().Bool("cascade", false, "also delete orphaned cluster and user entries")
	cmd.Flags().BoolP("yes", "y", false, "skip confirmation prompt")
	return cmd
}

// ─── duplicate ────────────────────────────────────────────────────────────────

func newDuplicateCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "duplicate <src> <dst>",
		Aliases: []string{"dup", "copy"},
		Short:   "Duplicate a context under a new name",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := op.Duplicate(args[0], args[1]); err != nil {
				return err
			}
			fmt.Printf("Duplicated %q → %q\n", args[0], args[1])
			return nil
		},
	}
}

// ─── group ────────────────────────────────────────────────────────────────────

func newGroupCmd() *cobra.Command {
	grp := &cobra.Command{
		Use:   "group",
		Short: "Manage context groups",
	}
	grp.AddCommand(
		&cobra.Command{
			Use:   "set <context> <group>",
			Short: "Assign a context to a group",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				if err := op.SetGroup(args[0], args[1]); err != nil {
					return err
				}
				fmt.Printf("Context %q assigned to group %q\n", args[0], args[1])
				return nil
			},
		},
		&cobra.Command{
			Use:   "ls",
			Short: "List all groups and their contexts",
			RunE: func(cmd *cobra.Command, _ []string) error {
				metaMap, err := st.AllMeta()
				if err != nil {
					return err
				}
				groups := make(map[string][]string)
				for name, m := range metaMap {
					if m.Group != "" {
						groups[m.Group] = append(groups[m.Group], name)
					}
				}
				w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
				fmt.Fprintln(w, "GROUP\tCONTEXTS")
				for g, names := range groups {
					fmt.Fprintf(w, "%s\t%s\n", g, strings.Join(names, ", "))
				}
				return w.Flush()
			},
		},
	)
	return grp
}

// ─── label ────────────────────────────────────────────────────────────────────

func newLabelCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "label <context> key=val [key=val...]",
		Short:   "Set labels on a context",
		Example: "  kcm label prod env=production tier=backend",
		Args:    cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			for _, kv := range args[1:] {
				parts := strings.SplitN(kv, "=", 2)
				if len(parts) != 2 {
					return fmt.Errorf("label %q is not in key=value format", kv)
				}
				if err := op.SetLabel(name, parts[0], parts[1]); err != nil {
					return err
				}
			}
			fmt.Printf("Labels set on %q\n", name)
			return nil
		},
	}
}

// ─── color ────────────────────────────────────────────────────────────────────

func newColorCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "color <context> <color>",
		Short:   "Set a display color for a context",
		Example: "  kcm color prod red\n  kcm color staging yellow",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := op.SetColor(args[0], args[1]); err != nil {
				return err
			}
			fmt.Printf("Color %q set on %q\n", args[1], args[0])
			return nil
		},
	}
}

// ─── pin ──────────────────────────────────────────────────────────────────────

func newPinCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "pin <context>",
		Short:   "Pin (or unpin) a context as a favourite",
		Example: "  kcm pin prod-cluster\n  kcm pin prod-cluster --unpin",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			unpin, _ := cmd.Flags().GetBool("unpin")
			if err := op.SetPinned(args[0], !unpin); err != nil {
				return err
			}
			if unpin {
				fmt.Printf("Unpinned %q\n", args[0])
			} else {
				fmt.Printf("Pinned %q\n", args[0])
			}
			return nil
		},
	}
	cmd.Flags().Bool("unpin", false, "remove the pin")
	return cmd
}

// ─── import ───────────────────────────────────────────────────────────────────

func newImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import <path|url|->",
		Short: "Import a kubeconfig from file, URL, or stdin",
		Example: `  kcm import ~/Downloads/new.yaml
  kcm import https://example.com/kube.yaml
  cat kube.yaml | kcm import -`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dest, _ := cmd.Flags().GetString("dest")
			if dest == "" {
				dest = primaryPath()
			}
			resolution, _ := cmd.Flags().GetString("on-conflict")
			res := ops.ConflictSkip
			switch resolution {
			case "overwrite":
				res = ops.ConflictOverwrite
			case "rename":
				res = ops.ConflictAutoRename
			}

			src := ops.ImportSource{}
			switch args[0] {
			case "-":
				src.Stdin = true
			default:
				if strings.HasPrefix(args[0], "http://") || strings.HasPrefix(args[0], "https://") {
					src.URL = args[0]
				} else {
					src.Path = args[0]
				}
			}

			warnings, err := op.Import(src, dest, res)
			for _, w := range warnings {
				fmt.Fprintln(os.Stderr, "warn:", w)
			}
			if err != nil {
				return err
			}
			fmt.Printf("Imported into %s\n", dest)
			return nil
		},
	}
	cmd.Flags().String("dest", "", "destination kubeconfig file (default: primary)")
	cmd.Flags().String("on-conflict", "skip", "conflict resolution: skip|overwrite|rename")
	return cmd
}

// ─── export ───────────────────────────────────────────────────────────────────

func newExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export <context>",
		Short: "Export a context as a standalone kubeconfig",
		Example: `  kcm export prod -o /tmp/prod.yaml
  kcm export prod --redact | pbcopy
  kcm export prod --canonical`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			outFile, _ := cmd.Flags().GetString("output")
			redact, _ := cmd.Flags().GetBool("redact")
			canonical, _ := cmd.Flags().GetBool("canonical")

			if canonical {
				b, err := op.ExportCanonical(args[0])
				if err != nil {
					return err
				}
				fmt.Print(string(b))
				return nil
			}

			if outFile == "" || outFile == "-" {
				// Write to stdout.
				tmp, _ := os.CreateTemp("", "kcm-export-*.yaml")
				name := tmp.Name()
				tmp.Close()
				defer os.Remove(name)
				if err := op.Export(args[0], name, redact); err != nil {
					return err
				}
				data, _ := os.ReadFile(name)
				fmt.Print(string(data))
				return nil
			}
			if err := op.Export(args[0], outFile, redact); err != nil {
				return err
			}
			fmt.Printf("Exported %q to %s\n", args[0], outFile)
			return nil
		},
	}
	cmd.Flags().StringP("output", "o", "-", "output file (- for stdout)")
	cmd.Flags().Bool("redact", false, "replace sensitive fields with <redacted>")
	cmd.Flags().Bool("canonical", false, "deterministically ordered YAML")
	return cmd
}

// ─── merge ────────────────────────────────────────────────────────────────────

func newMergeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "merge <src> [src...] -o <dest>",
		Short: "Merge kubeconfig files into one",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dest, _ := cmd.Flags().GetString("output")
			if dest == "" {
				return fmt.Errorf("--output / -o is required")
			}
			resolution, _ := cmd.Flags().GetString("on-conflict")
			res := ops.ConflictSkip
			switch resolution {
			case "overwrite":
				res = ops.ConflictOverwrite
			case "rename":
				res = ops.ConflictAutoRename
			}
			warnings, err := op.MergeFiles(args, dest, res)
			for _, w := range warnings {
				fmt.Fprintln(os.Stderr, "warn:", w)
			}
			return err
		},
	}
	cmd.Flags().StringP("output", "o", "", "destination file (required)")
	cmd.Flags().String("on-conflict", "skip", "conflict resolution: skip|overwrite|rename")
	return cmd
}

// ─── split ────────────────────────────────────────────────────────────────────

func newSplitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "split [path]",
		Short: "Split a kubeconfig into one file per context",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			outDir, _ := cmd.Flags().GetString("output")
			if outDir == "" {
				outDir = "."
			}
			path := primaryPath()
			if len(args) > 0 {
				path = args[0]
			}
			if err := mgr.Load(path); err != nil {
				return err
			}
			return op.Split(path, outDir)
		},
	}
	cmd.Flags().StringP("output", "o", "", "output directory (default: current dir)")
	return cmd
}

// ─── validate ─────────────────────────────────────────────────────────────────

func newValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate [path]",
		Short: "Validate kubeconfig files for syntax and reference errors",
		RunE: func(cmd *cobra.Command, args []string) error {
			var issues []interface{ GetMessage() string }
			if len(args) > 0 {
				raw, err := op.ValidatePath(args[0])
				if err != nil {
					return err
				}
				for _, i := range raw {
					fmt.Printf("[%s] %s: %s — %s\n", i.Severity, i.File, i.Context, i.Message)
				}
				if len(raw) == 0 {
					fmt.Println("OK")
				}
				_ = issues
				return nil
			}
			all := op.ValidateAll()
			if len(all) == 0 {
				fmt.Println("All kubeconfigs OK")
				return nil
			}
			for _, i := range all {
				fmt.Printf("[%s] %s: %s — %s\n", i.Severity, i.File, i.Context, i.Message)
			}
			return nil
		},
	}
}

// ─── diff ─────────────────────────────────────────────────────────────────────

func newDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff <ctx1|file1> <ctx2|file2>",
		Short: "Show differences between two contexts or kubeconfig files",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			files, _ := cmd.Flags().GetBool("files")
			if files {
				diff, err := op.DiffFiles(args[0], args[1])
				if err != nil {
					return err
				}
				fmt.Print(diff)
				return nil
			}
			diffs, err := mgr.DiffContexts(args[0], args[1])
			if err != nil {
				return err
			}
			if len(diffs) == 0 {
				fmt.Println("No differences")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "FIELD\tOLD\tNEW")
			for _, d := range diffs {
				fmt.Fprintf(w, "%s\t%s\t%s\n", d.Field, d.Old, d.New)
			}
			return w.Flush()
		},
	}
	cmd.Flags().Bool("files", false, "treat arguments as file paths instead of context names")
	return cmd
}

// ─── health ───────────────────────────────────────────────────────────────────

func newHealthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "health [context]",
		Short: "Check cluster reachability",
		Example: `  kcm health             # check all clusters
  kcm health prod        # check one cluster`,
		RunE: func(cmd *cobra.Command, args []string) error {
			timeout, _ := cmd.Flags().GetDuration("timeout")
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "CONTEXT\tSERVER\tSTATUS\tLATENCY")

			if len(args) == 1 {
				hs := op.CheckHealth(args[0], timeout)
				printHealth(w, hs)
			} else {
				results := op.BulkHealth(timeout)
				for _, hs := range results {
					printHealth(w, hs)
				}
			}
			return w.Flush()
		},
	}
	cmd.Flags().Duration("timeout", 3*time.Second, "per-cluster connection timeout")
	return cmd
}

func printHealth(w *tabwriter.Writer, hs ops.HealthStatus) {
	status := "✔ OK"
	if !hs.OK {
		status = "✘ FAIL: " + hs.Err
	}
	fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", hs.ContextName, hs.Server, status, hs.Latency.Round(time.Millisecond))
}

// ─── backup ───────────────────────────────────────────────────────────────────

func newBackupCmd() *cobra.Command {
	backup := &cobra.Command{
		Use:   "backup",
		Short: "Manage kubeconfig backups",
	}
	backup.AddCommand(
		&cobra.Command{
			Use:   "ls",
			Short: "List available backups",
			RunE: func(cmd *cobra.Command, _ []string) error {
				entries, err := core.ListBackups()
				if err != nil {
					return err
				}
				w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
				fmt.Fprintln(w, "ID\tORIGINAL\tTIME")
				for _, e := range entries {
					fmt.Fprintf(w, "%s\t%s\t%s\n", e.ID, e.Original, e.Time.Format("2006-01-02 15:04:05"))
				}
				return w.Flush()
			},
		},
		&cobra.Command{
			Use:   "restore <id>",
			Short: "Restore a backup",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				entries, err := core.ListBackups()
				if err != nil {
					return err
				}
				for _, e := range entries {
					if e.ID == args[0] {
						dest := primaryPath()
						if err := core.RestoreBackup(e.Path, dest); err != nil {
							return err
						}
						fmt.Printf("Restored %s to %s\n", e.ID, dest)
						return mgr.Load(dest)
					}
				}
				return fmt.Errorf("backup %q not found", args[0])
			},
		},
	)
	return backup
}

// ─── undo ─────────────────────────────────────────────────────────────────────

func newUndoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "undo",
		Short: "Undo the last reversible operation",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := op.Undo(); err != nil {
				return err
			}
			fmt.Println("Undone.")
			return nil
		},
	}
}

// ─── serve ────────────────────────────────────────────────────────────────────

func newServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Launch the web dashboard",
		Example: `  kcm serve
  kcm serve --port 9090 --no-browser`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			port, _ := cmd.Flags().GetInt("port")
			host, _ := cmd.Flags().GetString("host")
			noBrowser, _ := cmd.Flags().GetBool("no-browser")
			return startServer(mgr, st, op, host, port, noBrowser)
		},
	}
	cmd.Flags().Int("port", 8585, "port to listen on")
	cmd.Flags().String("host", "127.0.0.1", "host to bind (use 0.0.0.0 with caution)")
	cmd.Flags().Bool("no-browser", false, "do not auto-open browser")
	return cmd
}

// ─── tui ──────────────────────────────────────────────────────────────────────

func newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Launch the interactive TUI",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTUI()
		},
	}
}

// ─── watch ────────────────────────────────────────────────────────────────────

func newWatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Live status of all clusters, refreshing every N seconds",
		RunE: func(cmd *cobra.Command, _ []string) error {
			interval, _ := cmd.Flags().GetDuration("interval")
			timeout, _ := cmd.Flags().GetDuration("timeout")
			return runWatch(op, interval, timeout)
		},
	}
	cmd.Flags().Duration("interval", 5*time.Second, "refresh interval")
	cmd.Flags().Duration("timeout", 3*time.Second, "per-cluster timeout")
	return cmd
}

// ─── shell-init ───────────────────────────────────────────────────────────────

func newShellInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "shell-init <bash|zsh|fish>",
		Short: "Print shell integration snippet",
		Long: `Emit shell init code for per-shell context switching.

Add to your shell rc:
  eval "$(kcm shell-init zsh)"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			shell := args[0]
			switch shell {
			case "bash", "zsh":
				fmt.Print(shellInitBashZsh)
			case "fish":
				fmt.Fprint(os.Stdout, shellInitFish)
			default:
				return fmt.Errorf("unsupported shell %q; choose bash, zsh, or fish", shell)
			}
			return nil
		},
	}
}

const shellInitBashZsh = `
# kcm shell integration — per-shell KUBECONFIG isolation
_kcm_tmp="${TMPDIR:-/tmp}/kcm-$$"
export KUBECONFIG="${_kcm_tmp}.yaml:${KUBECONFIG:-$HOME/.kube/config}"
# Copy current context into the tmp file on first run.
kcm _shell-sync "${_kcm_tmp}.yaml" 2>/dev/null
# Clean up on exit.
trap 'rm -f "${_kcm_tmp}.yaml"' EXIT
`

// shellInitFish uses $fish_pid instead of %self so Go vet doesn't flag %s.
const shellInitFish = `
# kcm shell integration (fish)
set -gx _kcm_tmp (mktemp /tmp/kcm-$fish_pid.yaml)
set -gx KUBECONFIG "$_kcm_tmp:$HOME/.kube/config"
kcm _shell-sync $_kcm_tmp 2>/dev/null
function _kcm_cleanup --on-event fish_exit
    rm -f $_kcm_tmp
end
`

// ─── completion ───────────────────────────────────────────────────────────────

func newCompletionCmd(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "completion <bash|zsh|fish|powershell>",
		Short: "Generate shell completion scripts",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletion(os.Stdout)
			case "zsh":
				return root.GenZshCompletion(os.Stdout)
			case "fish":
				return root.GenFishCompletion(os.Stdout, true)
			case "powershell":
				return root.GenPowerShellCompletion(os.Stdout)
			default:
				return fmt.Errorf("unknown shell %q", args[0])
			}
		},
	}
}

// ─── version ──────────────────────────────────────────────────────────────────

func newVersionCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Println("kcm", version)
		},
	}
}

// ─── helpers (forward declarations wired by wiring.go) ────────────────────────

// These vars are set by wiring.go init() to avoid import cycles between
// cli ↔ tui/gui packages.
var (
	startServer func(m interface{}, s interface{}, o interface{}, host string, port int, noBrowser bool) error
	runTUI      func() error
	runWatch    func(o interface{}, interval, timeout time.Duration) error
)

func init() {
	// Fallbacks — overridden by wiring.go before main() runs.
	startServer = func(m, s, o interface{}, host string, port int, noBrowser bool) error {
		return fmt.Errorf("GUI not compiled in")
	}
	runTUI = func() error { return fmt.Errorf("TUI not compiled in") }
	runWatch = func(o interface{}, interval, timeout time.Duration) error {
		fmt.Println("watch: running in degraded mode (TUI not linked)")
		return nil
	}
}

// keep unused imports satisfied.
var _ = bufio.NewScanner
var _ = filepath.Join
var _ = strings.TrimSpace
