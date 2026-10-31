package tui

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/francisconeves97/jxscout/internal/core/common"
	"github.com/francisconeves97/jxscout/pkg/constants"
	jxscouttypes "github.com/francisconeves97/jxscout/pkg/types"
)

type projectScout struct {
	JXScout
	options  jxscouttypes.Options
	restarts int
}

type projectLog string

func (b *projectLog) String() string { return string(*b) }
func (b *projectLog) Clear()         { *b = "" }
func (s *projectScout) GetLogBuffer() LogBuffer {
	b := projectLog("12:03:04 INFO  Saved application.js\n12:03:05 INFO  Discovered 8 chunks")
	return &b
}

func (s *projectScout) GetOptions() jxscouttypes.Options { return s.options }
func (s *projectScout) Restart(options jxscouttypes.Options) (JXScout, error) {
	s.options = options
	s.restarts++
	if err := os.MkdirAll(common.GetPrivateDirectory(options.ProjectName), 0755); err != nil {
		return nil, err
	}
	return s, nil
}

func TestProjectCommand(t *testing.T) {
	base := t.TempDir()
	t.Setenv("JXSCOUT_PROJECTS_FILE", filepath.Join(base, "projects.json"))
	root := filepath.Join(base, "jxscout")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JXSCOUT_DATA_DIR", "")
	t.Setenv("JXSCOUT_PROJECT_ROOT", root)
	if err := os.WriteFile(filepath.Join(root, "config.json"), []byte(`{"version":1,"project":"old","scope":["*old*"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	scout := &projectScout{options: jxscouttypes.Options{ProjectName: "old", Hostname: "127.0.0.1", Port: 4444, WebcrackMode: "off", ScopePatterns: []string{"*old*"}}}
	ui := New(scout)
	if _, err := ui.ExecuteCommand("project"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ui.output, "Project: old") || scout.restarts != 0 {
		t.Fatal("show project should not restart")
	}
	if _, err := ui.ExecuteCommand("project ../escape"); err == nil {
		t.Fatal("invalid name accepted")
	}
	if scout.restarts != 0 {
		t.Fatal("invalid project restarted")
	}
	if _, err := ui.ExecuteCommand("project fresh"); err != nil {
		t.Fatal(err)
	}
	if scout.options.ProjectName != "fresh" || scout.options.Port != 4444 || scout.options.WebcrackMode != "off" {
		t.Fatalf("settings lost: %+v", scout.options)
	}
	_, cfg, found, err := common.DiscoverWorkspace(root)
	if err != nil || !found || cfg.Project != "fresh" || cfg.Scope[0] != "*old*" {
		t.Fatalf("workspace not updated: %+v %v", cfg, err)
	}
	// Restore a saved project without moving the Burp listener. Explicit edits win.
	dir := common.GetPrivateDirectory("saved")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, constants.ConfigFileName), []byte("project-name: saved\nhostname: localhost\nport: 3333\nscope:\n  - '*saved*'\nwebcrack: auto\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ui.ExecuteCommand("config project=saved debug=true"); err != nil {
		t.Fatal(err)
	}
	if scout.options.Port != 4444 || scout.options.Hostname != "127.0.0.1" || !scout.options.Debug || scout.options.ScopePatterns[0] != "*saved*" {
		t.Fatalf("incorrect saved options: %+v", scout.options)
	}
}

func TestDashboardSwitchesToInitializedFolderAndEditsScope(t *testing.T) {
	base := t.TempDir()
	t.Setenv("JXSCOUT_PROJECTS_FILE", filepath.Join(base, "projects.json"))
	t.Setenv("JXSCOUT_DATA_DIR", "")
	roots := map[string]string{}
	for _, name := range []string{"alpha", "lippo"} {
		root := filepath.Join(base, name, "jxscout")
		roots[name] = root
		if err := os.MkdirAll(root, 0755); err != nil {
			t.Fatal(err)
		}
		config := fmt.Sprintf(`{"version":1,"project":%q,"scope":[%q]}`, name, "*"+name+"*")
		if err := os.WriteFile(filepath.Join(root, "config.json"), []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
		if err := common.RegisterProject(name, root, []string{"*" + name + "*"}); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("JXSCOUT_PROJECT_ROOT", roots["alpha"])
	scout := &projectScout{options: jxscouttypes.Options{ProjectName: "alpha", Hostname: "127.0.0.1", Port: 4444, ScopePatterns: []string{"*alpha*"}}}
	ui := New(scout)
	ui.Update(tea.KeyMsg{Type: tea.KeyF2})
	if !strings.Contains(ui.View(), "CHOOSE PROJECT") {
		t.Fatal("project chooser did not open")
	}
	ui.Update(tea.KeyMsg{Type: tea.KeyDown})
	ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if scout.options.ProjectName != "lippo" || common.GetWorkingDirectory("lippo") != roots["lippo"] || scout.options.ScopePatterns[0] != "*lippo*" || scout.options.Port != 4444 {
		t.Fatalf("incorrect project selection: %+v output=%s", scout.options, common.GetWorkingDirectory("lippo"))
	}
	ui.Update(tea.KeyMsg{Type: tea.KeyF3})
	ui.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	ui.input.SetValue("*.lippomalls.com/*")
	ui.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(scout.options.ScopePatterns) != 2 {
		t.Fatalf("scope not added: %+v", scout.options.ScopePatterns)
	}
	project, found, err := common.RegisteredProject("lippo")
	if err != nil || !found || len(project.Scope) != 2 {
		t.Fatalf("scope not persisted: %+v %v", project, err)
	}
	ui.Update(tea.KeyMsg{Type: tea.KeyDown})
	ui.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if len(scout.options.ScopePatterns) != 1 {
		t.Fatal("scope not removed")
	}
	ui.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if ui.panel != "" {
		t.Fatal("scope panel did not close")
	}
	_, alpha, _, err := common.DiscoverWorkspace(roots["alpha"])
	if err != nil || alpha.Project != "alpha" || len(alpha.Scope) != 1 {
		t.Fatal("switch changed original project")
	}
}
