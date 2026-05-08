// Package cli wires cobra commands to the ops layer.
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/odhomane/kcm/internal/core"
	"github.com/odhomane/kcm/internal/ops"
	"github.com/odhomane/kcm/internal/store"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	// Global state initialised in PersistentPreRunE.
	mgr *core.Manager
	st  *store.Store
	op  *ops.Ops
)

// NewRootCmd builds and returns the root cobra command.
func NewRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:   "kcm",
		Short: "Kubernetes kubeconfig manager",
		Long: `kcm — a lightweight manager for Kubernetes kubeconfig files.

Run without arguments to launch the interactive TUI picker.

Examples:
  kcm                      # interactive TUI
  kcm ls                   # list all contexts
  kcm use my-context       # switch to a context
  kcm ns staging           # switch namespace
  kcm serve                # launch web dashboard`,
		PersistentPreRunE: initGlobals,
		RunE: func(cmd *cobra.Command, args []string) error {
			// No args → launch TUI.
			return runTUI()
		},
		SilenceUsage: true,
	}

	// Global flags.
	root.PersistentFlags().StringSlice("kubeconfig", nil, "additional kubeconfig file(s)")
	root.PersistentFlags().String("output", "table", "output format: table|wide|json|yaml")
	root.PersistentFlags().Bool("no-color", false, "disable colour output")
	_ = viper.BindPFlag("kubeconfig", root.PersistentFlags().Lookup("kubeconfig"))
	_ = viper.BindPFlag("output", root.PersistentFlags().Lookup("output"))
	_ = viper.BindPFlag("no_color", root.PersistentFlags().Lookup("no-color"))
	_ = viper.BindEnv("no_color", "NO_COLOR")

	// Sub-commands.
	root.AddCommand(
		newLsCmd(),
		newUseCmd(),
		newNsCmd(),
		newRenameCmd(),
		newDeleteCmd(),
		newDuplicateCmd(),
		newGroupCmd(),
		newLabelCmd(),
		newColorCmd(),
		newPinCmd(),
		newImportCmd(),
		newExportCmd(),
		newMergeCmd(),
		newSplitCmd(),
		newValidateCmd(),
		newDiffCmd(),
		newHealthCmd(),
		newBackupCmd(),
		newUndoCmd(),
		newServeCmd(),
		newTUICmd(),
		newWatchCmd(),
		newShellInitCmd(),
		newCompletionCmd(root),
		newVersionCmd(version),
	)

	return root
}

// initGlobals is the PersistentPreRunE that sets up Manager, Store and Ops
// before any command runs.
func initGlobals(cmd *cobra.Command, _ []string) error {
	// Skip init for completion/help — they don't need kubeconfig access.
	if cmd.Name() == "completion" || cmd.Name() == "__complete" || cmd.Name() == "help" {
		return nil
	}

	// Load kcm config via viper.
	cfgDir, err := core.KCMConfigDir()
	if err != nil {
		return err
	}
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(cfgDir)
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	_ = viper.ReadInConfig() // OK if no config file exists.

	// Merge extra kubeconfig paths from flag + viper.
	var extras []string
	if v := viper.GetStringSlice("kubeconfig"); len(v) > 0 {
		extras = append(extras, v...)
	}
	if v := viper.GetStringSlice("extra_kubeconfigs"); len(v) > 0 {
		extras = append(extras, v...)
	}

	paths := core.DiscoverPaths(extras)

	mgr = core.NewManager()
	var loadErrs []string
	for _, p := range paths {
		if err := mgr.Load(p); err != nil {
			loadErrs = append(loadErrs, fmt.Sprintf("  %s: %v", p, err))
		}
	}
	if len(loadErrs) > 0 {
		fmt.Fprintf(os.Stderr, "kcm: warning — could not load some kubeconfigs:\n%s\n",
			strings.Join(loadErrs, "\n"))
	}

	dbPath, err := core.KCMDBPath()
	if err != nil {
		return fmt.Errorf("getting db path: %w", err)
	}
	st, err = store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("opening store: %w", err)
	}

	op = ops.New(mgr, st)
	return nil
}

// primaryPath returns the primary kubeconfig path (first discovered, or
// $KUBECONFIG first entry, or ~/.kube/config).
func primaryPath() string {
	if ps := mgr.Paths(); len(ps) > 0 {
		return ps[0]
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".kube", "config")
}

// noColor returns true if colour should be suppressed.
func noColor() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return true
	}
	return viper.GetBool("no_color")
}
