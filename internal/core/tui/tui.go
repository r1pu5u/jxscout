package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	assetservice "github.com/francisconeves97/jxscout/internal/core/asset-service"
	"github.com/francisconeves97/jxscout/internal/core/common"
	"github.com/francisconeves97/jxscout/internal/modules/overrides"
	jxscouttypes "github.com/francisconeves97/jxscout/pkg/types"
	"github.com/muesli/reflow/wordwrap"
)

type Command struct {
	Name        string
	ShortName   string
	Description string
	Usage       string
	Execute     func(args []string) (tea.Cmd, error)
}

type TUI struct {
	input                  textinput.Model
	output                 string
	commands               map[string]Command
	history                []string
	historyIndex           int
	logsPanelShown         bool
	logsPanelViewport      viewport.Model
	logsPanelViewportReady bool
	autoScroll             bool
	jxscout                JXScout
	height                 int
	commandMode            bool
	editing                bool
	notice                 string
	refreshPending         bool
	assets                 []assetservice.Asset
	assetTotal             int
	dataError              string
	resultViewport         viewport.Model
	resultContent          string
	width                  int
	panel                  string
	panelIndex             int
	panelError             string
	projects               []common.Project
	findings               []AnalysisMatch
	findingCount           int
	findingsRefreshPending bool
	findingsLastRefresh    time.Time
	findingsRequestID      uint64
}

// AnalysisMatch is a static AST analyzer observation, not a confirmed vulnerability.
type AnalysisMatch struct {
	AssetPath, Analyzer, Value string
	Line, Column               int
}

type analysisResultsSource interface {
	GetAnalysisMatches(context.Context) ([]AnalysisMatch, error)
}

type LogBuffer interface {
	String() string
	Clear()
}

type JXScout interface {
	Stop() error
	GetOptions() jxscouttypes.Options
	GetLogBuffer() LogBuffer
	Restart(options jxscouttypes.Options) (JXScout, error)
	GetOverridesModule() overrides.OverridesModule
	Ctx() context.Context
	GetAssetService() jxscouttypes.AssetService
	TruncateTables() error
}

func New(jxscout JXScout) *TUI {
	t := &TUI{
		input:             textinput.New(),
		resultViewport:    viewport.New(80, 15),
		logsPanelViewport: viewport.New(80, 15),
		commands:          map[string]Command{},
		history:           []string{},
		historyIndex:      -1,
		logsPanelShown:    false,
		autoScroll:        true,
		jxscout:           jxscout,
	}
	t.input.Prompt = "> "
	t.input.Placeholder = "Enter command..."
	t.input.Blur()
	t.RegisterDefaultCommands()
	return t
}

func (t *TUI) writeLineToOutput(line string) {
	outputBuilder := strings.Builder{}

	outputBuilder.WriteString(t.output)
	outputBuilder.WriteString(fmt.Sprintf("%s\n", line))

	t.output = outputBuilder.String()
}

func (t *TUI) addToHistory(cmd string) {
	t.history = append(t.history, cmd)
	t.historyIndex = len(t.history)
}

type LogsTickMsg time.Time

func (t *TUI) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, logsTickerCmd(), t.refreshDashboard(), t.refreshFindings())
}

func logsTickerCmd() tea.Cmd {
	return tea.Every(530*time.Millisecond, func(t time.Time) tea.Msg {
		return LogsTickMsg(t)
	})
}

func (t *TUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		t.width, t.height = msg.Width, msg.Height
		t.input.Width = max(4, msg.Width-10)
		t.logsPanelViewport.Width = max(1, msg.Width-8)
		t.logsPanelViewport.Height = max(1, msg.Height-13)
		t.logsPanelViewportReady = true
		t.logsPanelViewport.SetContent(wordwrap.String(t.jxscout.GetLogBuffer().String(), t.logsPanelViewport.Width))
	case dashboardDataMsg:
		t.refreshPending = false
		if msg.project == t.jxscout.GetOptions().ProjectName && msg.root == common.GetWorkingDirectory(msg.project) {
			t.assets, t.assetTotal = msg.assets, msg.total
			t.dataError = ""
			if msg.err != nil {
				t.dataError = msg.err.Error()
			}
		}
	case findingsDataMsg:
		if msg.requestID == t.findingsRequestID {
			t.findingsRefreshPending = false
		}
		if msg.requestID == t.findingsRequestID && msg.project == t.jxscout.GetOptions().ProjectName && msg.root == common.GetWorkingDirectory(msg.project) {
			t.findings = msg.matches
			t.findingCount = len(msg.matches)
			if msg.err != nil {
				t.dataError = msg.err.Error()
			}
		}
	case LogsTickMsg:
		cmds = append(cmds, logsTickerCmd(), t.refreshDashboard(), t.refreshFindings())
		if t.logsPanelShown && t.logsPanelViewportReady {
			t.logsPanelViewport.SetContent(wordwrap.String(t.jxscout.GetLogBuffer().String(), t.logsPanelViewport.Width))
			if t.autoScroll {
				t.logsPanelViewport.GotoBottom()
			}
		}
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			cmd, err := t.ExecuteCommand("exit")
			if err != nil {
				t.panelError = err.Error()
			}
			return t, cmd
		}
		if t.commandMode {
			switch msg.Type {
			case tea.KeyEsc:
				t.commandMode = false
				t.input.Reset()
				t.input.Blur()
				return t, nil
			case tea.KeyEnter:
				command := strings.TrimSpace(t.input.Value())
				t.commandMode = false
				t.input.Reset()
				t.input.Blur()
				if command != "" {
					t.output = ""
					t.notice = ""
					t.addToHistory(command)
					cmd, err := t.ExecuteCommand(command)
					if err != nil {
						t.writeLineToOutput(fmt.Sprintf("Error: %s", err))
					}
					cmds = append(cmds, cmd)
				}
				return t, tea.Batch(cmds...)
			case tea.KeyUp:
				if t.historyIndex > 0 {
					t.historyIndex--
					t.input.SetValue(t.history[t.historyIndex])
				}
				return t, nil
			case tea.KeyDown:
				if t.historyIndex < len(t.history)-1 {
					t.historyIndex++
					t.input.SetValue(t.history[t.historyIndex])
				} else {
					t.input.Reset()
				}
				return t, nil
			}
			var cmd tea.Cmd
			t.input, cmd = t.input.Update(msg)
			return t, cmd
		}
		if !t.editing {
			target := ""
			navigate := false
			switch msg.Type {
			case tea.KeyF2:
				target = "projects"
				navigate = true
			case tea.KeyF3:
				target = "scope"
				navigate = true
			case tea.KeyF4:
				target = "logs"
				navigate = true
			case tea.KeyF5:
				target = "findings"
				navigate = true
			case tea.KeyTab, tea.KeyShiftTab:
				pages := []string{"", "projects", "scope", "logs", "findings"}
				index := 0
				for i, p := range pages {
					if p == t.panel {
						index = i
					}
				}
				step := 1
				if msg.Type == tea.KeyShiftTab {
					step = 4
				}
				target = pages[(index+step)%len(pages)]
				navigate = true
			case tea.KeyRunes:
				switch string(msg.Runes) {
				case "1":
					target = ""
					navigate = true
				case "2":
					target = "projects"
					navigate = true
				case "3":
					target = "scope"
					navigate = true
				case "4":
					target = "logs"
					navigate = true
				case "5":
					target = "findings"
					navigate = true
				case ":":
					return t, t.startCommand()
				case "?":
					t.output = t.GetHelp()
					return t, nil
				}
			}
			if navigate {
				t.output = ""
				t.openPanel(target)
				if target == "findings" {
					return t, t.refreshFindings()
				}
				return t, nil
			}
		}
		if t.output != "" {
			if msg.Type == tea.KeyEsc {
				t.output = ""
				return t, nil
			}
			var cmd tea.Cmd
			t.resultViewport, cmd = t.resultViewport.Update(msg)
			return t, cmd
		}
		if t.logsPanelShown {
			if msg.Type == tea.KeyEsc {
				t.closePanel()
				return t, nil
			}
			if msg.Type == tea.KeyRunes {
				switch string(msg.Runes) {
				case "s":
					t.autoScroll = !t.autoScroll
					return t, nil
				case "c":
					t.jxscout.GetLogBuffer().Clear()
					t.logsPanelViewport.SetContent("")
					return t, nil
				}
			}
			if msg.Type == tea.KeyUp || msg.Type == tea.KeyPgUp {
				t.autoScroll = false
			}
			var cmd tea.Cmd
			t.logsPanelViewport, cmd = t.logsPanelViewport.Update(msg)
			return t, cmd
		}
		if t.panel != "" {
			if t.panel == "findings" {
				switch msg.Type {
				case tea.KeyUp:
					if t.panelIndex > 0 {
						t.panelIndex--
					}
					return t, nil
				case tea.KeyDown:
					t.panelIndex++
					return t, nil
				case tea.KeyRunes:
					if string(msg.Runes) == "5" {
						return t, t.refreshFindings()
					}
				}
			}
			return t, t.panelUpdate(msg)
		}
		if msg.Type == tea.KeyRunes {
			// Preserve direct command entry for existing users, while keeping the home view clean.
			cmd := t.startCommand()
			t.input.SetValue(string(msg.Runes))
			return t, cmd
		}
	}
	if t.editing || t.commandMode {
		var cmd tea.Cmd
		t.input, cmd = t.input.Update(msg)
		cmds = append(cmds, cmd)
	}
	return t, tea.Batch(cmds...)
}

func (t *TUI) View() string { return t.renderDashboard() }
