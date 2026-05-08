package cli

import (
	"fmt"

	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/spf13/cobra"
)

func newUseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "use [context]",
		Short: "Switch to a context (fuzzy match supported)",
		Example: `  kcm use prod-cluster
  kcm use prod        # fuzzy — matches "prod-cluster" if unambiguous
  kcm use -           # switch to previous context`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if name == "-" {
				return op.UsePrev(primaryPath())
			}
			resolved, err := resolveContext(name)
			if err != nil {
				return err
			}
			if err := op.Use(resolved); err != nil {
				return err
			}
			fmt.Printf("Switched to context %q\n", resolved)
			return nil
		},
	}
	return cmd
}

// resolveContext returns the exact context name from a potentially fuzzy input.
func resolveContext(input string) (string, error) {
	all := mgr.AllContexts()
	// Exact match first.
	for _, ci := range all {
		if ci.Name == input {
			return ci.Name, nil
		}
	}
	// Fuzzy match.
	names := make([]string, len(all))
	for i, ci := range all {
		names[i] = ci.Name
	}
	matches := fuzzy.RankFindFold(input, names)
	if len(matches) == 0 {
		return "", fmt.Errorf("no context matching %q", input)
	}
	if len(matches) > 1 {
		fmt.Printf("Ambiguous match for %q, candidates:\n", input)
		for _, m := range matches {
			fmt.Printf("  %s\n", m.Target)
		}
		return "", fmt.Errorf("specify a more precise name")
	}
	return matches[0].Target, nil
}
