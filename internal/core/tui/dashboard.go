package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	assetservice "github.com/francisconeves97/jxscout/internal/core/asset-service"
	"github.com/francisconeves97/jxscout/internal/core/common"
)

var (
	ink      = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	muted    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	accent   = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	good     = lipgloss.NewStyle().Foreground(lipgloss.Color("114"))
	selected = lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Background(lipgloss.Color("24")).Bold(true)
)

func clip(s string, width int) string {
	return ansi.Truncate(strings.ReplaceAll(s, "\n", " "), max(0, width), "…")
}
func fit(s string, width, height int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > height {
		lines = lines[:max(0, height)]
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(0, width), "…")
	}
	return lipgloss.NewStyle().Width(max(0, width)).Height(max(0, height)).MaxWidth(max(0, width)).MaxHeight(max(0, height)).Render(strings.Join(lines, "\n"))
}
func box(title, content string, width, height int) string {
	innerW, innerH := max(1, width-4), max(1, height-2)
	body := accent.Bold(true).Render(clip(title, innerW)) + "\n" + content
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("238")).Padding(0, 1).Render(fit(body, innerW, innerH))
}

type dashboardDataMsg struct {
	project, root string
	assets        []assetservice.Asset
	total         int
	err           error
}

type findingsDataMsg struct {
	project   string
	root      string
	requestID uint64
	matches   []AnalysisMatch
	err       error
}

func (t *TUI) refreshFindings() tea.Cmd {
	if t.findingsRefreshPending || time.Since(t.findingsLastRefresh) < 3*time.Second {
		return nil
	}
	source, ok := t.jxscout.(analysisResultsSource)
	if !ok {
		return nil
	}
	scout := t.jxscout
	project := scout.GetOptions().ProjectName
	root := common.GetWorkingDirectory(project)
	ctxBase := scout.Ctx()
	t.findingsRequestID++
	requestID := t.findingsRequestID
	t.findingsRefreshPending = true
	t.findingsLastRefresh = time.Now()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctxBase, 3*time.Second)
		defer cancel()
		matches, err := source.GetAnalysisMatches(ctx)
		return findingsDataMsg{project: project, root: root, requestID: requestID, matches: matches, err: err}
	}
}

func (t *TUI) refreshDashboard() tea.Cmd {
	if t.refreshPending {
		return nil
	}
	t.refreshPending = true
	scout := t.jxscout
	project := scout.GetOptions().ProjectName
	root := common.GetWorkingDirectory(project)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(scout.Ctx(), 2*time.Second)
		defer cancel()
		assets, total, err := scout.GetAssetService().GetAssets(ctx, assetservice.GetAssetsParams{ProjectName: project, Page: 1, PageSize: 30})
		return dashboardDataMsg{project: project, root: root, assets: assets, total: total, err: err}
	}
}
func (t *TUI) openPanel(panel string) {
	t.panel, t.panelIndex, t.panelError = panel, 0, ""
	t.logsPanelShown = panel == "logs"
	t.editing = false
	t.input.Reset()
	t.input.Blur()
	if panel == "findings" {
		t.panelIndex = 0
	}
	if panel == "projects" {
		projects, err := common.ListProjects()
		if err != nil {
			t.panelError = err.Error()
			return
		}
		t.projects = projects
		for i, p := range projects {
			if p.Name == t.jxscout.GetOptions().ProjectName {
				t.panelIndex = i
			}
		}
	}
}
func (t *TUI) closePanel() { t.openPanel("") }
func (t *TUI) startCommand() tea.Cmd {
	t.commandMode = true
	t.output = ""
	t.input.Reset()
	t.input.Prompt = ": "
	t.input.Placeholder = "Command — help, assets, config…"
	return t.input.Focus()
}
func (t *TUI) scopeAddMode() tea.Cmd {
	t.editing = true
	t.input.Reset()
	t.input.Prompt = "+ "
	t.input.Placeholder = "*.example.com/*"
	return t.input.Focus()
}
func (t *TUI) panelUpdate(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		t.input, cmd = t.input.Update(msg)
		return cmd
	}
	if key.Type == tea.KeyEsc {
		if t.editing {
			t.editing = false
			t.input.Reset()
			t.input.Blur()
		} else {
			t.closePanel()
		}
		return nil
	}
	count := len(t.projects)
	if t.panel == "scope" {
		count = len(t.jxscout.GetOptions().ScopePatterns)
	}
	if t.editing {
		if key.Type == tea.KeyEnter {
			pattern := strings.TrimSpace(t.input.Value())
			if pattern == "" {
				return nil
			}
			_, err := t.commands["scope"].Execute([]string{"add", pattern})
			t.panelError = ""
			if err != nil {
				t.panelError = err.Error()
				return nil
			}
			t.editing = false
			t.input.Reset()
			t.input.Blur()
			t.notice = "Scope pattern added"
			t.output = ""
			return nil
		}
		var cmd tea.Cmd
		t.input, cmd = t.input.Update(msg)
		return cmd
	}
	switch key.Type {
	case tea.KeyUp:
		if t.panelIndex > 0 {
			t.panelIndex--
		}
		return nil
	case tea.KeyDown:
		if t.panelIndex+1 < count {
			t.panelIndex++
		}
		return nil
	case tea.KeyEnter:
		if t.panel == "projects" && count > 0 {
			name := t.projects[t.panelIndex].Name
			_, err := t.commands["project"].Execute([]string{name})
			if err != nil {
				t.panelError = err.Error()
				return nil
			}
			t.assets = nil
			t.assetTotal = 0
			t.findings = nil
			t.findingCount = 0
			t.findingsRefreshPending = false
			t.findingsLastRefresh = time.Time{}
			t.findingsRequestID++
			t.dataError = ""
			t.notice = "Switched to " + name
			t.output = ""
			t.closePanel()
			return tea.Batch(t.refreshDashboard(), t.refreshFindings())
		}
	case tea.KeyCtrlD, tea.KeyDelete:
		if t.panel == "scope" && count > 0 {
			_, err := t.commands["scope"].Execute([]string{"remove", t.jxscout.GetOptions().ScopePatterns[t.panelIndex]})
			t.panelError = ""
			if err != nil {
				t.panelError = err.Error()
			} else {
				t.notice = "Scope pattern removed"
				t.output = ""
			}
			if t.panelIndex > 0 {
				t.panelIndex--
			}
		}
	case tea.KeyRunes:
		if string(key.Runes) == "a" && t.panel == "scope" {
			return t.scopeAddMode()
		}
	}
	return nil
}
func (t *TUI) nav(width int) string {
	active := 0
	if t.panel == "projects" {
		active = 1
	}
	if t.panel == "scope" {
		active = 2
	}
	if t.logsPanelShown {
		active = 3
	}
	if t.panel == "findings" {
		active = 4
	}
	names := []string{"1 Capture", "2 Projects", "3 Scope", "4 Activity", "5 Findings"}
	gap := " "
	if width < 60 {
		names = []string{"1 Feed", "2 Proj", "3 Scope", "4 Log", "5 Findings"}
		gap = ""
	}
	if width < 42 {
		names = []string{"1", "2", "3", "4", "5"}
	}
	tabs := make([]string, len(names))
	for i, n := range names {
		if i == active {
			if width < 60 {
				tabs[i] = selected.Render(n)
			} else {
				tabs[i] = selected.Padding(0, 1).Render(n)
			}
		} else {
			if width < 60 {
				tabs[i] = muted.Render(n)
			} else {
				tabs[i] = muted.Padding(0, 1).Render(n)
			}
		}
	}
	return strings.Join(tabs, gap)
}

func (t *TUI) findingsView(width, height int) string {
	rows := []string{muted.Render("Analysis matches · review in context; not confirmed vulnerabilities."), ""}
	if t.dataError != "" {
		rows = append(rows, lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Render(clip(t.dataError, width-6)), "")
	}
	if len(t.findings) == 0 {
		rows = append(rows, ink.Bold(true).Render("No analysis matches yet."), muted.Render("Matches appear after JavaScript files are analyzed."))
	} else {
		counts := make(map[string]int)
		for _, f := range t.findings {
			counts[f.Analyzer]++
		}
		types := make([]string, 0, len(counts))
		for kind := range counts {
			types = append(types, kind)
		}
		sort.Strings(types)
		rows = append(rows, accent.Bold(true).Render(fmt.Sprintf("%d matches across analyzed files", len(t.findings))))
		for _, kind := range types {
			rows = append(rows, muted.Render(fmt.Sprintf("  %-24s %d", kind, counts[kind])))
		}
		rows = append(rows, "")
		for _, f := range t.findings {
			where := f.AssetPath
			if f.Line > 0 {
				where += fmt.Sprintf(":%d", f.Line)
			}
			rows = append(rows, accent.Render(clip(f.Analyzer, 22))+"  "+ink.Render(clip(f.Value, max(8, width-35))), muted.Render("    "+clip(where, width-10)))
		}
	}
	available := max(1, height-4)
	start := min(max(0, t.panelIndex), max(0, len(rows)-available))
	end := min(len(rows), start+available)
	return box("FINDINGS  /  STATIC ANALYSIS", strings.Join(rows[start:end], "\n"), width, height)
}
func (t *TUI) captureView(width, height int) string {
	opts := t.jxscout.GetOptions()
	mode := opts.WebcrackMode
	if mode == "" {
		mode = "auto"
	}
	metrics := fmt.Sprintf("%s captured files    %s analysis matches    %s scope patterns    webcrack %s", accent.Bold(true).Render(fmt.Sprint(t.assetTotal)), accent.Bold(true).Render(fmt.Sprint(t.findingCount)), accent.Bold(true).Render(fmt.Sprint(len(opts.ScopePatterns))), ink.Render(mode))
	var rows []string
	listWidth := width
	split := width >= 100
	if split {
		listWidth = width - 35
	}
	rows = nil
	if t.dataError != "" {
		rows = append(rows, lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Render(clip(t.dataError, listWidth-6)))
	}
	if len(t.assets) == 0 {
		rows = append(rows, "", ink.Bold(true).Render("Waiting for JavaScript"), "", muted.Render("Browse your target through Burp or Caido."), muted.Render("Files and discovered chunks appear here."))
	} else {
		urlWidth := max(8, listWidth-22)
		rows = append(rows, muted.Render(fmt.Sprintf("%-6s %-*s %s", "TYPE", urlWidth, "URL", "STATE")), "")
		for _, a := range t.assets {
			kind := "JS"
			if a.IsInlineJS {
				kind = "INLINE"
			}
			if a.IsChunkDiscovered != nil && *a.IsChunkDiscovered {
				kind = "CHUNK"
			}
			state := "saved"
			if a.IsBeautified {
				state = "ready"
			}
			rows = append(rows, accent.Render(fmt.Sprintf("%-6s", kind))+" "+lipgloss.NewStyle().Width(urlWidth).Render(clip(a.URL, urlWidth))+" "+muted.Render(state))
		}
	}
	main := box("RECENT FILES", strings.Join(rows, "\n"), listWidth, max(5, height-2))
	if split {
		detail := muted.Render("ACTIVE SCOPE") + "\n"
		if len(opts.ScopePatterns) == 0 {
			detail += "All URLs\n"
		}
		for _, pattern := range opts.ScopePatterns {
			detail += clip(pattern, 29) + "\n"
		}
		detail += "\n" + muted.Render("CAPTURE SOURCE") + "\nBurp Suite / Caido\n" + clip(fmt.Sprintf("%s:%d", opts.Hostname, opts.Port), 29) + "\n\n" + muted.Render("PROCESSING") + "\nChunk discovery enabled\nWebcrack: " + mode + "\n\n" + accent.Render("3  Edit scope") + "\n" + accent.Render("2  Switch project")
		main = lipgloss.JoinHorizontal(lipgloss.Top, main, " ", box("PROJECT", detail, width-listWidth-1, max(5, height-2)))
	}
	return fit(metrics, width, 1) + "\n\n" + main
}
func (t *TUI) projectView(width, height int) string {
	listWidth := width
	split := width >= 86
	if split {
		listWidth = width / 2
	}
	available := max(1, height-5)
	start := max(0, t.panelIndex-available+1)
	var rows []string
	for i := start; i < len(t.projects) && len(rows) < available; i++ {
		p := t.projects[i]
		name := p.Name
		if p.Name == t.jxscout.GetOptions().ProjectName {
			name += "  ● active"
		}
		line := "  " + clip(name, listWidth-6)
		if i == t.panelIndex {
			line = selected.Width(listWidth - 4).Render("› " + clip(name, listWidth-6))
		}
		rows = append(rows, line)
	}
	if len(t.projects) == 0 {
		rows = []string{muted.Render("No initialized projects."), "", "Run jxscout init in a target folder."}
	}
	left := box("CHOOSE PROJECT", strings.Join(rows, "\n"), listWidth, height)
	if !split {
		return left
	}
	details := muted.Render("Select a project to see its output folder.")
	if len(t.projects) > 0 {
		p := t.projects[t.panelIndex]
		details = ink.Bold(true).Render(p.Name) + "\n\n" + muted.Render("OUTPUT FOLDER") + "\n" + clip(p.Root, width-listWidth-6) + "\n\n" + muted.Render("SCOPE") + "\n"
		if len(p.Scope) == 0 {
			details += "All URLs"
		}
		for _, s := range p.Scope {
			details += clip(s, width-listWidth-6) + "\n"
		}
		details += "\n" + muted.Render("Enter switches capture to this folder.\nThe Burp connection stays on the same port.")
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, " ", box("PROJECT DETAILS", details, width-listWidth-1, height))
}
func (t *TUI) scopeView(width, height int) string {
	patterns := t.jxscout.GetOptions().ScopePatterns
	listHeight := height
	if t.editing {
		listHeight -= 4
	}
	var rows []string
	available := max(1, listHeight-5)
	start := max(0, t.panelIndex-available+1)
	for i := start; i < len(patterns) && len(rows) < available; i++ {
		row := "  " + clip(patterns[i], width-6)
		if i == t.panelIndex {
			row = selected.Width(width - 4).Render("› " + clip(patterns[i], width-6))
		}
		rows = append(rows, row)
	}
	if len(patterns) == 0 {
		rows = []string{"", ink.Render("All URLs are accepted."), muted.Render("Press a to add a scope pattern.")}
	}
	rows = append(rows, "", muted.Render("Match full URLs · any matching pattern is accepted"))
	content := box("URL SCOPE  /  "+t.jxscout.GetOptions().ProjectName, strings.Join(rows, "\n"), width, max(3, listHeight))
	if t.editing {
		content += "\n" + box("ADD PATTERN", t.input.View(), width, 4)
	}
	return content
}
func (t *TUI) renderDashboard() string {
	w, h := t.width, t.height
	if w == 0 {
		w = 100
	}
	if h == 0 {
		h = 30
	}
	if w < 38 || h < 18 {
		return fit("JXScout\n\nEnlarge the terminal to at least 38 × 18.\nCtrl+C quits.", w, h)
	}
	width := w - 4
	opts := t.jxscout.GetOptions()
	wordmark := accent.Bold(true).Render("JXSCOUT") + "  " + muted.Render("/ ") + ink.Bold(true).Render(clip(opts.ProjectName, max(8, width-45)))
	status := good.Render("● LISTENING") + "  " + muted.Render(fmt.Sprintf("%s:%d", opts.Hostname, opts.Port))
	if width < 60 {
		wordmark = accent.Bold(true).Render("JXSCOUT") + "  " + ink.Render(clip(opts.ProjectName, max(4, width-26)))
		status = good.Render("● LIVE") + muted.Render(fmt.Sprintf(" :%d", opts.Port))
	}
	top := wordmark + strings.Repeat(" ", max(1, width-lipgloss.Width(wordmark)-lipgloss.Width(status))) + status
	path := muted.Render(clip(common.GetWorkingDirectory(opts.ProjectName), width))
	header := top + "\n" + path + "\n\n" + t.nav(width) + "\n"
	bodyHeight := h - 10
	var body, keys string
	switch {
	case t.output != "":
		t.resultViewport.Width = width - 4
		t.resultViewport.Height = max(1, bodyHeight-3)
		content := ansi.Wrap(t.output, t.resultViewport.Width, "")
		if t.resultContent != content {
			t.resultViewport.SetContent(content)
			t.resultContent = content
			t.resultViewport.GotoTop()
		}
		body = box("COMMAND OUTPUT", t.resultViewport.View(), width, bodyHeight)
		keys = "Esc close  ·  ↑/↓ scroll  ·  : command"
	case t.logsPanelShown:
		title := "ACTIVITY  /  FOLLOWING"
		if !t.autoScroll {
			title = "ACTIVITY  /  PAUSED"
		}
		body = box(title, t.logsPanelViewport.View(), width, bodyHeight)
		keys = "↑/↓ scroll  ·  s follow/pause  ·  c clear  ·  Esc capture"
	case t.panel == "projects":
		body = t.projectView(width, bodyHeight)
		keys = "↑/↓ select  ·  Enter switch  ·  Esc capture"
	case t.panel == "scope":
		body = t.scopeView(width, bodyHeight)
		keys = "a add pattern  ·  ↑/↓ select  ·  Del remove  ·  Esc capture"
		if t.editing {
			keys = "Enter save pattern  ·  Esc cancel"
		}
	case t.panel == "findings":
		body = t.findingsView(width, bodyHeight)
		keys = "↑/↓ scroll  ·  Esc capture  ·  5 refresh"
	default:
		body = t.captureView(width, bodyHeight)
		keys = "Tab next view  ·  : command  ·  Ctrl+C quit"
	}
	// Command entry and feedback always occupy the same row, preventing layout jumps.
	feedback := muted.Render("Capture continues while you change views.")
	if t.notice != "" {
		feedback = good.Render(clip(t.notice, width))
	}
	if t.panelError != "" {
		feedback = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Render(clip(t.panelError, width))
	}
	if t.commandMode {
		feedback = t.input.View()
		keys = "Enter run  ·  Esc cancel  ·  ↑/↓ command history"
	}
	frame := fit(header, width, 5) + "\n" + fit(body, width, bodyHeight) + "\n" + fit(feedback, width, 1) + "\n" + fit(muted.Render(keys), width, 1)
	return lipgloss.NewStyle().Padding(1, 2).Render(fit(frame, width, h-2))
}
