package common

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Project stores the output folder chosen by init, independently of cwd.
type Project struct {
	Name  string   `json:"name"`
	Root  string   `json:"root"`
	Scope []string `json:"scope"`
}
type projectRegistry struct {
	Version  int                `json:"version"`
	Projects map[string]Project `json:"projects"`
}

func ProjectsFile() string {
	if file := os.Getenv("JXSCOUT_PROJECTS_FILE"); file != "" {
		return file
	}
	return filepath.Join(GetHome(), ".config", "jxscout", "projects.json")
}
func readProjects() (projectRegistry, error) {
	registry := projectRegistry{Version: 1, Projects: map[string]Project{}}
	data, err := os.ReadFile(ProjectsFile())
	if errors.Is(err, os.ErrNotExist) {
		return registry, nil
	}
	if err != nil {
		return registry, err
	}
	if err := json.Unmarshal(data, &registry); err != nil {
		return registry, fmt.Errorf("read project registry: %w", err)
	}
	if registry.Version != 1 || registry.Projects == nil {
		return registry, fmt.Errorf("invalid project registry: %s", ProjectsFile())
	}
	return registry, nil
}
func RegisteredProject(name string) (Project, bool, error) {
	registry, err := readProjects()
	if err != nil {
		return Project{}, false, err
	}
	project, found := registry.Projects[name]
	if !found {
		return Project{}, false, nil
	}
	if !filepath.IsAbs(project.Root) {
		return Project{}, false, fmt.Errorf("invalid output folder for project %s", name)
	}
	project.Name = name
	// Read the workspace marker for current scopes, including older workspaces.
	cfg, exists, err := readWorkspaceConfig(filepath.Join(project.Root, "config.json"))
	if err != nil {
		return Project{}, false, err
	}
	if !exists {
		return Project{}, false, fmt.Errorf("project %s output folder is missing its config; initialize it again: %s", name, project.Root)
	}
	if cfg.Project == name {
		project.Scope = cfg.Scope
	}
	return project, true, nil
}
func ListProjects() ([]Project, error) {
	registry, err := readProjects()
	if err != nil {
		return nil, err
	}
	projects := make([]Project, 0, len(registry.Projects))
	for name, project := range registry.Projects {
		project.Name = name
		projects = append(projects, project)
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].Name < projects[j].Name })
	return projects, nil
}
func RegisterProject(name, root string, scope []string) error {
	if err := ValidateProjectName(name); err != nil {
		return err
	}
	registry, err := readProjects()
	if err != nil {
		return err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	if scope == nil {
		scope = []string{}
	}
	registry.Projects[name] = Project{Name: name, Root: root, Scope: scope}
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	return writeJSONAtomic(ProjectsFile(), data)
}
func writeJSONAtomic(file string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(file), ".jxscout-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err = temp.Write(append(data, '\n')); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), file)
}
