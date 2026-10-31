package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/francisconeves97/jxscout/internal/core/common"
	"github.com/francisconeves97/jxscout/pkg/constants"
	jxscouttypes "github.com/francisconeves97/jxscout/pkg/types"
	"gopkg.in/yaml.v3"
)

// projectOptions restores saved processing settings while keeping the proxy
// connection stable. Registered projects restore their initialized output folder.
func (t *TUI) projectOptions(name string) (jxscouttypes.Options, error) {
	current := t.jxscout.GetOptions()
	if err := common.ValidateProjectName(name); err != nil {
		return current, err
	}
	if name == current.ProjectName {
		return current, nil
	}
	selected := current
	project, registered, err := common.RegisteredProject(name)
	if err != nil {
		return current, err
	}
	private := common.GetPrivateDirectory(name)
	if registered && os.Getenv("JXSCOUT_DATA_DIR") == "" {
		private = filepath.Join(project.Root, ".state", name)
	}
	data, err := os.ReadFile(filepath.Join(private, constants.ConfigFileName))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return current, fmt.Errorf("read project configuration: %w", err)
	}
	if err == nil {
		if err := yaml.Unmarshal(data, &selected); err != nil {
			return current, fmt.Errorf("decode project configuration: %w", err)
		}
	}
	if registered && os.Getenv("JXSCOUT_DATA_DIR") == "" {
		selected.ScopePatterns = project.Scope
	}
	selected.ProjectName = name
	selected.Hostname, selected.Port = current.Hostname, current.Port
	return selected, nil
}
