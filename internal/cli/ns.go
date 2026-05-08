package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"
	"os"
	"time"

	"github.com/spf13/cobra"
)

func newNsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ns [namespace]",
		Short: "Switch or list namespaces for the current context",
		Example: `  kcm ns                   # list namespaces
  kcm ns staging           # switch to staging namespace
  kcm ns -                 # switch to previous namespace
  kcm ns --context prod    # operate on a specific context`,
		Args: cobra.MaximumNArgs(1),
		RunE: runNs,
	}
	cmd.Flags().String("context", "", "context to operate on (default: current)")
	cmd.Flags().Duration("cache-ttl", 5*time.Minute, "namespace list cache TTL")
	return cmd
}

func runNs(cmd *cobra.Command, args []string) error {
	ctxName, _ := cmd.Flags().GetString("context")
	cacheTTL, _ := cmd.Flags().GetDuration("cache-ttl")

	if ctxName == "" {
		// Find current context.
		for _, p := range mgr.Paths() {
			if c := mgr.CurrentContext(p); c != "" {
				ctxName = c
				break
			}
		}
	}
	if ctxName == "" {
		return fmt.Errorf("no current context; specify --context")
	}

	if len(args) == 0 {
		// List namespaces.
		namespaces, err := op.ListNamespaces(ctxName, cacheTTL)
		if err != nil {
			return fmt.Errorf("listing namespaces: %w", err)
		}
		favs, _ := st.FavoriteNamespaces(ctxName)
		favSet := make(map[string]bool, len(favs))
		for _, f := range favs {
			favSet[f] = true
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAMESPACE\tFAVOURITE")
		for _, ns := range namespaces {
			fav := ""
			if favSet[ns] {
				fav = "★"
			}
			fmt.Fprintf(w, "%s\t%s\n", ns, fav)
		}
		return w.Flush()
	}

	ns := args[0]
	if ns == "-" {
		// Switch to previous namespace by reading audit log.
		entries, err := st.AuditLog(10)
		if err != nil {
			return err
		}
		nc, _, err := op.Mgr.FindContext(ctxName)
		if err != nil {
			return err
		}
		nc.RLock()
		current := nc.Config.Contexts[ctxName].Namespace
		nc.RUnlock()
		for _, e := range entries {
			if e.Op == "ns-switch" && e.Context == ctxName && e.Before != current {
				ns = e.Before
				break
			}
		}
		if ns == "-" {
			return fmt.Errorf("no previous namespace found for context %q", ctxName)
		}
	}

	// Validate namespace looks reasonable (alphanumeric + dash).
	if strings.ContainsAny(ns, " /\\") {
		return fmt.Errorf("invalid namespace name %q", ns)
	}

	// Record current ns before switch.
	nc, _, _ := op.Mgr.FindContext(ctxName)
	prevNS := ""
	if nc != nil {
		nc.RLock()
		if ctx := nc.Config.Contexts[ctxName]; ctx != nil {
			prevNS = ctx.Namespace
		}
		nc.RUnlock()
	}

	if err := op.SwitchNamespace(ctxName, ns); err != nil {
		return err
	}
	_ = st.LogAudit("ns-switch", ctxName, prevNS, ns)
	fmt.Printf("Namespace switched to %q (context: %s)\n", ns, ctxName)
	return nil
}
