package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/odhomane/kcm/internal/core"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"
)

// ─── Styles ───────────────────────────────────────────────────────────────────

var (
	lsColorEnabled = true

	colHeader   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("240"))
	colCurrent  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("82"))   // green
	colName     = lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	colMono     = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	colDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	colGroup    = lipgloss.NewStyle().Foreground(lipgloss.Color("75"))              // blue
	colPinned   = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))             // yellow
	colCloud    = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))             // orange
	colActive   = lipgloss.NewStyle().Bold(true).Background(lipgloss.Color("236"))
)

// ─── Command ──────────────────────────────────────────────────────────────────

func newLsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list", "contexts"},
		Short:   "List all contexts across all kubeconfig files",
		Example: `  kcm ls
  kcm ls --output wide
  kcm ls --output json
  kcm ls --group prod
  kcm ls --label env=production`,
		RunE: runLs,
	}
	cmd.Flags().String("group", "", "filter by group")
	cmd.Flags().String("label", "", "filter by label key=value")
	cmd.Flags().Bool("pinned", false, "show only pinned contexts")
	return cmd
}

// ─── Row type ─────────────────────────────────────────────────────────────────

type lsRow struct {
	core.ContextInfo
	Group    string
	Color    string
	Labels   map[string]string
	Pinned   bool
	LastUsed *time.Time
}

// ─── Runner ───────────────────────────────────────────────────────────────────

func runLs(cmd *cobra.Command, _ []string) error {
	output := viper.GetString("output")
	groupFilter, _ := cmd.Flags().GetString("group")
	labelFilter, _ := cmd.Flags().GetString("label")
	pinnedOnly, _ := cmd.Flags().GetBool("pinned")

	lsColorEnabled = !noColor()

	contexts := mgr.AllContexts()
	metaMap, _ := st.AllMeta()

	rows := make([]lsRow, 0, len(contexts))
	for _, ci := range contexts {
		m := metaMap[ci.Name]
		r := lsRow{
			ContextInfo: ci,
			Group:       m.Group,
			Color:       m.Color,
			Labels:      m.Labels,
			Pinned:      m.Pinned,
			LastUsed:    m.LastUsedAt,
		}
		if groupFilter != "" && r.Group != groupFilter {
			continue
		}
		if labelFilter != "" {
			kv := strings.SplitN(labelFilter, "=", 2)
			if len(kv) == 2 && r.Labels[kv[0]] != kv[1] {
				continue
			}
		}
		if pinnedOnly && !r.Pinned {
			continue
		}
		rows = append(rows, r)
	}

	sort.Slice(rows, func(i, j int) bool {
		// current context always first
		if rows[i].IsCurrent != rows[j].IsCurrent {
			return rows[i].IsCurrent
		}
		// pinned second
		if rows[i].Pinned != rows[j].Pinned {
			return rows[i].Pinned
		}
		return rows[i].Name < rows[j].Name
	})

	switch output {
	case "json":
		return json.NewEncoder(os.Stdout).Encode(rows)

	case "yaml":
		for _, r := range rows {
			fmt.Printf("- name: %s\n  cluster: %s\n  user: %s\n  namespace: %s\n  group: %s\n  source: %s\n",
				r.Name, r.ClusterName, r.UserName, r.Namespace, r.Group, r.SourceFile)
		}

	default: // "table" or "wide"
		printTable(rows, output == "wide")
	}
	return nil
}

// ─── Table renderer ───────────────────────────────────────────────────────────

func printTable(rows []lsRow, wide bool) {
	termW, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || termW <= 0 {
		termW = 120
	}

	// Column definitions: header, extractor func, min width, max width
	type colDef struct {
		header string
		get    func(lsRow) string
		min    int
		max    int
		wide   bool // only show in wide mode
	}

	cols := []colDef{
		{"", func(r lsRow) string { return statusGlyph(r) }, 2, 2, false},
		{"NAME", func(r lsRow) string { return r.Name }, 20, 60, false},
		{"CLUSTER", func(r lsRow) string { return r.ClusterName }, 14, 40, false},
		{"NAMESPACE", func(r lsRow) string {
			if r.Namespace == "" { return "—" }
			return r.Namespace
		}, 10, 24, false},
		{"GROUP", func(r lsRow) string {
			if r.Group == "" { return "—" }
			return r.Group
		}, 8, 20, false},
		{"CLOUD", func(r lsRow) string {
			if r.CloudProvider == "" { return "—" }
			if r.CloudRegion != "" { return r.CloudProvider + "/" + r.CloudRegion }
			return r.CloudProvider
		}, 8, 22, false},
		{"USER", func(r lsRow) string { return r.UserName }, 10, 30, true},
		{"SOURCE", func(r lsRow) string { return shortSource(r.SourceFile) }, 12, 36, true},
		{"LAST USED", func(r lsRow) string { return fmtLastUsed(r.LastUsed) }, 10, 18, false},
	}

	// Filter wide-only cols.
	activeCols := make([]colDef, 0, len(cols))
	for _, c := range cols {
		if !c.wide || wide {
			activeCols = append(activeCols, c)
		}
	}

	// Compute column widths: start at min, expand to fit content.
	widths := make([]int, len(activeCols))
	for i, c := range activeCols {
		widths[i] = len(c.header)
		if widths[i] < c.min {
			widths[i] = c.min
		}
	}
	for _, r := range rows {
		for i, c := range activeCols {
			l := len(c.get(r))
			if l > widths[i] {
				widths[i] = l
			}
			if widths[i] > c.max {
				widths[i] = c.max
			}
		}
	}

	// ─── Header ──────────────────────────────────────────────────────────
	var hdr strings.Builder
	for i, c := range activeCols {
		cell := padRight(c.header, widths[i])
		if lsColorEnabled {
			cell = colHeader.Render(cell)
		}
		if i > 0 {
			hdr.WriteString("  ")
		}
		hdr.WriteString(cell)
	}
	fmt.Println(hdr.String())

	// Separator line.
	if lsColorEnabled {
		sep := strings.Repeat("─", min(termW, 120))
		fmt.Println(colDim.Render(sep))
	}

	// ─── Rows ────────────────────────────────────────────────────────────
	for _, r := range rows {
		var line strings.Builder
		for i, c := range activeCols {
			raw := c.get(r)
			// Truncate if over max.
			if len(raw) > c.max {
				raw = raw[:c.max-1] + "…"
			}
			cell := padRight(raw, widths[i])

			if lsColorEnabled {
				cell = applyColStyle(i, c.header, cell, r)
			}
			if i > 0 {
				line.WriteString("  ")
			}
			line.WriteString(cell)
		}

		rendered := line.String()
		if lsColorEnabled && r.IsCurrent {
			// Subtle highlight for current row.
			rendered = colActive.Render(rendered)
		}
		fmt.Println(rendered)
	}

	// Footer count.
	if lsColorEnabled {
		fmt.Println(colDim.Render(fmt.Sprintf("\n  %d context(s)", len(rows))))
	}
}

// applyColStyle applies lipgloss color based on column type and row state.
func applyColStyle(colIdx int, header, cell string, r lsRow) string {
	switch header {
	case "": // status glyph column
		if r.IsCurrent {
			return colCurrent.Render(cell)
		}
		if r.Pinned {
			return colPinned.Render(cell)
		}
		return colDim.Render(cell)
	case "NAME":
		s := colName
		if r.IsCurrent {
			s = colCurrent
		} else if r.Color != "" {
			if c, ok := namedColor(r.Color); ok {
				s = lipgloss.NewStyle().Foreground(c)
			}
		}
		return s.Render(cell)
	case "CLUSTER", "USER", "SOURCE":
		return colMono.Render(cell)
	case "GROUP":
		if strings.TrimSpace(cell) == "—" {
			return colDim.Render(cell)
		}
		return colGroup.Render(cell)
	case "CLOUD":
		if strings.TrimSpace(cell) == "—" {
			return colDim.Render(cell)
		}
		return colCloud.Render(cell)
	case "NAMESPACE", "LAST USED":
		if strings.TrimSpace(cell) == "—" {
			return colDim.Render(cell)
		}
		return colMono.Render(cell)
	}
	return cell
}

// statusGlyph returns the leading indicator character.
func statusGlyph(r lsRow) string {
	if r.IsCurrent {
		return "✔"
	}
	if r.Pinned {
		return "★"
	}
	return " "
}

func fmtLastUsed(t *time.Time) string {
	if t == nil {
		return "—"
	}
	diff := time.Since(*t)
	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		return fmt.Sprintf("%dm ago", int(diff.Minutes()))
	case diff < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(diff.Hours()))
	default:
		return t.Format("2006-01-02")
	}
}

func shortSource(path string) string {
	path = strings.ReplaceAll(path, os.Getenv("HOME"), "~")
	parts := strings.Split(path, "/")
	if len(parts) > 3 {
		return "…/" + strings.Join(parts[len(parts)-2:], "/")
	}
	return path
}

func padRight(s string, n int) string {
	// Use visual length (runes) to avoid multi-byte issues with padding.
	runes := []rune(s)
	if len(runes) >= n {
		return string(runes[:n])
	}
	return s + strings.Repeat(" ", n-len(runes))
}

func namedColor(name string) (lipgloss.Color, bool) {
	m := map[string]lipgloss.Color{
		"red":     "196",
		"green":   "82",
		"blue":    "75",
		"yellow":  "220",
		"orange":  "208",
		"purple":  "135",
		"cyan":    "51",
		"magenta": "201",
		"pink":    "213",
		"white":   "255",
		"gray":    "245",
	}
	c, ok := m[strings.ToLower(name)]
	return c, ok
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
