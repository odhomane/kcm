package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/odhomane/kcm/internal/core"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newLsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list", "contexts"},
		Short:   "List all contexts across all kubeconfig files",
		Example: `  kcm ls
  kcm ls --output wide
  kcm ls --output json
  kcm ls --group prod`,
		RunE: runLs,
	}
	cmd.Flags().String("group", "", "filter by group")
	cmd.Flags().String("label", "", "filter by label (key=value)")
	cmd.Flags().Bool("pinned", false, "show only pinned contexts")
	return cmd
}

func runLs(cmd *cobra.Command, _ []string) error {
	output := viper.GetString("output")
	groupFilter, _ := cmd.Flags().GetString("group")
	labelFilter, _ := cmd.Flags().GetString("label")
	pinnedOnly, _ := cmd.Flags().GetBool("pinned")

	contexts := mgr.AllContexts()
	metaMap, _ := st.AllMeta()

	// Merge metadata into context infos.
	type row struct {
		core.ContextInfo
		Group     string
		Color     string
		Labels    map[string]string
		Pinned    bool
		LastUsed  *time.Time
	}

	rows := make([]row, 0, len(contexts))
	for _, ci := range contexts {
		m := metaMap[ci.Name]
		r := row{
			ContextInfo: ci,
			Group:       m.Group,
			Color:       m.Color,
			Labels:      m.Labels,
			Pinned:      m.Pinned,
			LastUsed:    m.LastUsedAt,
		}

		// Apply filters.
		if groupFilter != "" && r.Group != groupFilter {
			continue
		}
		if labelFilter != "" {
			kv := strings.SplitN(labelFilter, "=", 2)
			if len(kv) == 2 {
				if r.Labels[kv[0]] != kv[1] {
					continue
				}
			}
		}
		if pinnedOnly && !r.Pinned {
			continue
		}
		rows = append(rows, r)
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Name < rows[j].Name
	})

	switch output {
	case "json":
		return json.NewEncoder(os.Stdout).Encode(rows)
	case "yaml":
		// Simple YAML-ish output for each row.
		for _, r := range rows {
			fmt.Printf("- name: %s\n  cluster: %s\n  user: %s\n  namespace: %s\n  source: %s\n",
				r.Name, r.ClusterName, r.UserName, r.Namespace, r.SourceFile)
		}
	default: // table or wide
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		if output == "wide" {
			fmt.Fprintln(w, "CURRENT\tNAME\tCLUSTER\tUSER\tNAMESPACE\tGROUP\tSOURCE\tLAST USED")
		} else {
			fmt.Fprintln(w, "CURRENT\tNAME\tCLUSTER\tNAMESPACE\tGROUP")
		}
		for _, r := range rows {
			current := " "
			if r.IsCurrent {
				current = "✔"
				if noColor() {
					current = "*"
				}
			}
			pinMark := ""
			if r.Pinned {
				pinMark = " ★"
			}
			lastUsed := ""
			if r.LastUsed != nil {
				lastUsed = r.LastUsed.Format("2006-01-02 15:04")
			}
			if output == "wide" {
				fmt.Fprintf(w, "%s\t%s%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					current, r.Name, pinMark, r.ClusterName, r.UserName, r.Namespace, r.Group, r.SourceFile, lastUsed)
			} else {
				fmt.Fprintf(w, "%s\t%s%s\t%s\t%s\t%s\n",
					current, r.Name, pinMark, r.ClusterName, r.Namespace, r.Group)
			}
		}
		w.Flush()
	}
	return nil
}
