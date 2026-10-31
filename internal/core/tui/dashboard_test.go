package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	assetservice "github.com/francisconeves97/jxscout/internal/core/asset-service"
	"github.com/francisconeves97/jxscout/internal/core/common"
	jxscouttypes "github.com/francisconeves97/jxscout/pkg/types"
	"github.com/muesli/termenv"
)

type analysisProjectScout struct {
	*projectScout
	reads int
}

func (s *analysisProjectScout) Ctx() context.Context { return context.Background() }
func (s *analysisProjectScout) GetAnalysisMatches(context.Context) ([]AnalysisMatch, error) {
	s.reads++
	return []AnalysisMatch{{AssetPath: "main.js", Analyzer: "secret", Value: "token", Line: 12}}, nil
}

func TestDashboardLayoutAndNavigation(t *testing.T) {
	if os.Getenv("JXSCOUT_TUI_SNAPSHOT_DIR") != "" {
		previous := lipgloss.ColorProfile()
		lipgloss.SetColorProfile(termenv.ANSI256)
		defer lipgloss.SetColorProfile(previous)
	}
	t.Setenv("JXSCOUT_PROJECTS_FILE", filepath.Join(t.TempDir(), "projects.json"))
	t.Setenv("JXSCOUT_PROJECT_ROOT", "/home/analyst/targets/example.com/jxscout")
	t.Setenv("JXSCOUT_DATA_DIR", "")
	scout := &projectScout{options: jxscouttypes.Options{ProjectName: "example.com", Hostname: "localhost", Port: 3333, WebcrackMode: "auto", ScopePatterns: []string{"*.example.com/*", "*cdn.example.com*"}}}
	ui := New(scout)
	if lipgloss.Width(ui.nav(38)) > 38 {
		t.Fatal("compact navigation should fit the minimum dashboard width")
	}
	chunk := true
	ui.assetTotal = 128
	ui.findingCount = 7
	ui.assets = []assetservice.Asset{
		{URL: "https://www.example.com/assets/main.7cb1a320.js", IsBeautified: true},
		{URL: "https://www.example.com/assets/827.83f2b972.js", IsChunkDiscovered: &chunk, IsBeautified: true},
		{URL: "https://www.example.com/assets/vendor.e0c2f12a.js", IsBeautified: true},
	}
	for _, size := range [][2]int{{120, 28}, {80, 24}, {42, 18}, {24, 10}} {
		ui.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for _, page := range []string{"", "projects", "scope", "logs", "command"} {
			ui.output = ""
			ui.commandMode = false
			ui.openPanel(page)
			if page == "projects" {
				ui.projects = []common.Project{{Name: "example.com", Root: "/home/analyst/targets/example.com/jxscout", Scope: scout.options.ScopePatterns}, {Name: "example.com", Root: "/home/analyst/targets/demo/jxscout"}}
			}
			if page == "scope" {
				ui.scopeAddMode()
				ui.input.SetValue("*.new-domain.com/*")
			}
			if page == "command" {
				ui.output = strings.Repeat("A long command result: "+strings.Repeat("x", 160)+"\n", 100)
			}
			view := ui.View()
			if lipgloss.Width(view) > size[0] || lipgloss.Height(view) > size[1] {
				t.Fatalf("%s at %v rendered %dx%d", page, size, lipgloss.Width(view), lipgloss.Height(view))
			}
			if dir := os.Getenv("JXSCOUT_TUI_SNAPSHOT_DIR"); dir != "" && size[0] == 120 {
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				name := page
				if name == "" {
					name = "capture"
				}
				if err := os.WriteFile(filepath.Join(dir, name+".ansi"), []byte(view), 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	ui.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	ui.output = ""
	ui.closePanel()
	ui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	if ui.panel != "scope" || ui.editing {
		t.Fatal("3 should open scope in browse mode")
	}
	ui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("5")})
	if ui.panel != "findings" {
		t.Fatal("5 should open findings")
	}
	ui.findings = []AnalysisMatch{{AssetPath: "main.js", Analyzer: "secret", Value: "token = ...", Line: 12}}
	findingsView := ui.View()
	if !strings.Contains(findingsView, "not confirmed vulnerabilities") || !strings.Contains(findingsView, "secret") || !strings.Contains(findingsView, "main.js:12") {
		t.Fatal("findings view should explain match status and show analyzer, code, and location")
	}
	ui.closePanel()
	ui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	ui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if !ui.editing {
		t.Fatal("a should focus the scope form")
	}
	ui.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if ui.editing || ui.panel != "scope" {
		t.Fatal("Esc should cancel editing without leaving scope")
	}
	ui.Update(tea.KeyMsg{Type: tea.KeyTab})
	if !ui.logsPanelShown {
		t.Fatal("Tab should navigate to activity")
	}
	ui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	if !ui.commandMode {
		t.Fatal("colon should open command entry")
	}
	ui.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if ui.commandMode {
		t.Fatal("Esc should dismiss command entry")
	}
}

func TestFindingsRefreshIsGuardedAndThrottled(t *testing.T) {
	t.Setenv("JXSCOUT_PROJECTS_FILE", filepath.Join(t.TempDir(), "projects.json"))
	t.Setenv("JXSCOUT_PROJECT_ROOT", t.TempDir())
	scout := &analysisProjectScout{projectScout: &projectScout{options: jxscouttypes.Options{ProjectName: "lippo"}}}
	ui := New(scout)
	cmd := ui.refreshFindings()
	if cmd == nil || ui.refreshFindings() != nil {
		t.Fatal("parallel findings reads should be suppressed")
	}
	msg := cmd().(findingsDataMsg)
	ui.Update(msg)
	if scout.reads != 1 || ui.findingCount != 1 {
		t.Fatalf("expected one read and persisted count, got reads=%d count=%d", scout.reads, ui.findingCount)
	}
	if ui.refreshFindings() != nil {
		t.Fatal("findings read should be throttled after completion")
	}
}
