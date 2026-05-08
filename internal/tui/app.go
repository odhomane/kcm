// Package tui provides the bubbletea interactive interface.
package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/odhomane/kcm/internal/core"
	"github.com/odhomane/kcm/internal/ops"
	"github.com/odhomane/kcm/internal/store"
)

// ─── Styles ───────────────────────────────────────────────────────────────────

var (
	styleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ECEFF4")).
			Background(lipgloss.Color("#5E81AC")).
			Padding(0, 1)

	styleActive = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A3BE8C")).
			Bold(true)

	styleInactive = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#D8DEE9"))

	stylePinned = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#EBCB8B"))

	styleGroup = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#81A1C1")).
			Bold(true)

	styleHelp = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#4C566A")).
			Italic(true)

	styleDetail = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#4C566A")).
			Padding(0, 1)

	styleErr = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#BF616A")).
			Bold(true)

	styleOK = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A3BE8C"))
)

// ─── Context item (list.Item implementation) ──────────────────────────────────

type contextItem struct {
	ci       core.ContextInfo
	meta     store.ContextMeta
	selected bool
}

func (c contextItem) Title() string {
	prefix := "  "
	if c.ci.IsCurrent {
		prefix = styleActive.Render("✔ ")
	}
	pinMark := ""
	if c.meta.Pinned {
		pinMark = stylePinned.Render(" ★")
	}
	return prefix + c.ci.Name + pinMark
}

func (c contextItem) Description() string {
	parts := []string{c.ci.ClusterName}
	if c.ci.Namespace != "" {
		parts = append(parts, c.ci.Namespace)
	}
	if c.meta.Group != "" {
		parts = append(parts, styleGroup.Render("["+c.meta.Group+"]"))
	}
	if c.ci.CloudProvider != "" {
		parts = append(parts, c.ci.CloudProvider)
	}
	return strings.Join(parts, " · ")
}

func (c contextItem) FilterValue() string {
	return c.ci.Name + " " + c.ci.ClusterName + " " + c.meta.Group
}

// ─── App model ────────────────────────────────────────────────────────────────

type state int

const (
	stateList state = iota
	stateDetail
	stateRename
	stateConfirmDelete
	stateGroupInput
	stateNamespaceList
)

type model struct {
	mgr      *core.Manager
	st       *store.Store
	op       *ops.Ops
	list     list.Model
	detail   string
	state    state
	input    textinput.Model
	inputCtx string // context being operated on
	err      string
	width    int
	height   int
	items    []contextItem
}

// ─── Messages ─────────────────────────────────────────────────────────────────

type refreshMsg struct{}
type errMsg struct{ err error }
type switchedMsg struct{ name string }

// ─── Key bindings ─────────────────────────────────────────────────────────────

var keys = struct {
	Switch, Rename, Delete, Group, Label, Pin, Namespace, Edit, Quit, Help key.Binding
}{
	Switch:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "switch")),
	Rename:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "rename")),
	Delete:    key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
	Group:     key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "group")),
	Label:     key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "label")),
	Pin:       key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pin")),
	Namespace: key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "namespace")),
	Edit:      key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
	Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
}

// ─── Init ─────────────────────────────────────────────────────────────────────

func newModel(mgr *core.Manager, st *store.Store, op *ops.Ops) model {
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#88C0D0")).
		Bold(true)

	l := list.New(nil, delegate, 60, 20)
	l.Title = "kcm — kubeconfig manager"
	l.Styles.Title = styleTitle
	l.SetShowHelp(false)
	l.SetFilteringEnabled(true)

	ti := textinput.New()
	ti.CharLimit = 128

	m := model{mgr: mgr, st: st, op: op, list: l, input: ti}
	m.loadItems()
	return m
}

func (m *model) loadItems() {
	contexts := m.mgr.AllContexts()
	metaMap, _ := m.st.AllMeta()

	items := make([]list.Item, 0, len(contexts))
	m.items = make([]contextItem, 0, len(contexts))
	for _, ci := range contexts {
		it := contextItem{ci: ci, meta: metaMap[ci.Name]}
		items = append(items, it)
		m.items = append(m.items, it)
	}
	m.list.SetItems(items)
}

func (m model) Init() tea.Cmd {
	return nil
}

// ─── Update ───────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.list.SetWidth(msg.Width / 2)
		m.list.SetHeight(msg.Height - 4)

	case refreshMsg:
		m.loadItems()
		m.err = ""

	case errMsg:
		m.err = msg.err.Error()
		m.state = stateList

	case switchedMsg:
		m.err = ""
		m.loadItems()

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	var cmd tea.Cmd
	switch m.state {
	case stateList:
		m.list, cmd = m.list.Update(msg)
	case stateRename, stateGroupInput:
		m.input, cmd = m.input.Update(msg)
	}
	return m, cmd
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.state {
	case stateList:
		return m.handleListKey(msg)
	case stateRename:
		return m.handleRenameKey(msg)
	case stateGroupInput:
		return m.handleGroupKey(msg)
	case stateConfirmDelete:
		return m.handleConfirmDeleteKey(msg)
	}
	return m, nil
}

func (m model) selectedItem() (contextItem, bool) {
	if item := m.list.SelectedItem(); item != nil {
		if ci, ok := item.(contextItem); ok {
			return ci, true
		}
	}
	return contextItem{}, false
}

func (m model) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, keys.Switch):
		ci, ok := m.selectedItem()
		if !ok {
			break
		}
		return m, func() tea.Msg {
			if err := m.op.Use(ci.ci.Name); err != nil {
				return errMsg{err}
			}
			return switchedMsg{ci.ci.Name}
		}

	case key.Matches(msg, keys.Rename):
		ci, ok := m.selectedItem()
		if !ok {
			break
		}
		m.inputCtx = ci.ci.Name
		m.input.SetValue(ci.ci.Name)
		m.input.Focus()
		m.input.Placeholder = "new name"
		m.state = stateRename
		return m, textinput.Blink

	case key.Matches(msg, keys.Delete):
		ci, ok := m.selectedItem()
		if !ok {
			break
		}
		m.inputCtx = ci.ci.Name
		m.state = stateConfirmDelete
		return m, nil

	case key.Matches(msg, keys.Group):
		ci, ok := m.selectedItem()
		if !ok {
			break
		}
		m.inputCtx = ci.ci.Name
		m.input.SetValue("")
		m.input.Focus()
		m.input.Placeholder = "group name"
		m.state = stateGroupInput
		return m, textinput.Blink

	case key.Matches(msg, keys.Pin):
		ci, ok := m.selectedItem()
		if !ok {
			break
		}
		return m, func() tea.Msg {
			if err := m.op.SetPinned(ci.ci.Name, !ci.meta.Pinned); err != nil {
				return errMsg{err}
			}
			return refreshMsg{}
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m model) handleRenameKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		newName := strings.TrimSpace(m.input.Value())
		if newName == "" || newName == m.inputCtx {
			m.state = stateList
			return m, nil
		}
		old := m.inputCtx
		m.state = stateList
		return m, func() tea.Msg {
			if err := m.op.Rename(old, newName); err != nil {
				return errMsg{err}
			}
			return refreshMsg{}
		}
	case tea.KeyEsc:
		m.state = stateList
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) handleGroupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		group := strings.TrimSpace(m.input.Value())
		ctx := m.inputCtx
		m.state = stateList
		return m, func() tea.Msg {
			if err := m.op.SetGroup(ctx, group); err != nil {
				return errMsg{err}
			}
			return refreshMsg{}
		}
	case tea.KeyEsc:
		m.state = stateList
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) handleConfirmDeleteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		ctx := m.inputCtx
		m.state = stateList
		return m, func() tea.Msg {
			if err := m.op.Delete(ctx, false); err != nil {
				return errMsg{err}
			}
			return refreshMsg{}
		}
	default:
		m.state = stateList
		return m, nil
	}
}

// ─── View ─────────────────────────────────────────────────────────────────────

func (m model) View() string {
	var sb strings.Builder

	// Error bar.
	if m.err != "" {
		sb.WriteString(styleErr.Render("Error: "+m.err) + "\n")
	}

	switch m.state {
	case stateRename:
		sb.WriteString(styleTitle.Render("Rename context") + "\n\n")
		sb.WriteString(m.input.View() + "\n")
		sb.WriteString(styleHelp.Render("enter to confirm · esc to cancel"))
		return sb.String()

	case stateGroupInput:
		sb.WriteString(styleTitle.Render("Set group for: "+m.inputCtx) + "\n\n")
		sb.WriteString(m.input.View() + "\n")
		sb.WriteString(styleHelp.Render("enter to confirm · esc to cancel"))
		return sb.String()

	case stateConfirmDelete:
		sb.WriteString(styleErr.Render(fmt.Sprintf("Delete %q? [y/N]", m.inputCtx)))
		return sb.String()
	}

	// Main list view.
	listView := m.list.View()

	// Detail panel for selected item.
	detail := ""
	if ci, ok := m.selectedItem(); ok {
		detail = buildDetail(ci)
	}

	// Two-pane layout.
	var content string
	if m.width > 80 && detail != "" {
		content = lipgloss.JoinHorizontal(
			lipgloss.Top,
			listView,
			"  ",
			styleDetail.Width(m.width/2-4).Render(detail),
		)
	} else {
		content = listView
	}

	sb.WriteString(content + "\n")
	sb.WriteString(styleHelp.Render(
		"j/k navigate · enter switch · r rename · d delete · g group · p pin · / filter · q quit",
	))
	return sb.String()
}

func buildDetail(ci contextItem) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Name:      %s\n", ci.ci.Name)
	fmt.Fprintf(&sb, "Cluster:   %s\n", ci.ci.ClusterName)
	fmt.Fprintf(&sb, "User:      %s\n", ci.ci.UserName)
	fmt.Fprintf(&sb, "Namespace: %s\n", ci.ci.Namespace)
	fmt.Fprintf(&sb, "Server:    %s\n", ci.ci.Server)
	fmt.Fprintf(&sb, "Source:    %s\n", ci.ci.SourceFile)
	if ci.meta.Group != "" {
		fmt.Fprintf(&sb, "Group:     %s\n", ci.meta.Group)
	}
	if ci.meta.Color != "" {
		fmt.Fprintf(&sb, "Color:     %s\n", ci.meta.Color)
	}
	if ci.meta.Pinned {
		fmt.Fprintf(&sb, "Pinned:    ★\n")
	}
	if ci.meta.LastUsedAt != nil {
		fmt.Fprintf(&sb, "Last used: %s\n", ci.meta.LastUsedAt.Format(time.RFC1123))
	}
	if ci.ci.CloudProvider != "" {
		fmt.Fprintf(&sb, "Cloud:     %s %s\n", ci.ci.CloudProvider, ci.ci.CloudRegion)
	}
	if len(ci.meta.Labels) > 0 {
		fmt.Fprintf(&sb, "Labels:\n")
		for k, v := range ci.meta.Labels {
			fmt.Fprintf(&sb, "  %s=%s\n", k, v)
		}
	}
	return sb.String()
}

// ─── Entry point ──────────────────────────────────────────────────────────────

// Run launches the TUI.
func Run(mgr *core.Manager, st *store.Store, op *ops.Ops) error {
	m := newModel(mgr, st, op)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	if err != nil && err.Error() == "program exited" {
		return nil
	}
	// Restore terminal cursor on exit.
	fmt.Fprint(os.Stderr, "\033[?25h")
	return err
}
