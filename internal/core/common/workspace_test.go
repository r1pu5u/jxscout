package common

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDiscoverWorkspaceFromProjectAndOutputRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "target", "foo", "jxscout")
	if err := os.MkdirAll(filepath.Join(root, "artifacts", "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"version":1,"project":"foo","scope":["*foo*"]}`), 0644); err != nil {
		t.Fatal(err)
	}

	for _, start := range []string{
		filepath.Join(base, "target", "foo"),
		filepath.Join(root, "artifacts", "nested"),
		root,
	} {
		gotRoot, config, found, err := DiscoverWorkspace(start)
		if err != nil {
			t.Fatalf("DiscoverWorkspace(%q): %v", start, err)
		}
		if !found || gotRoot != root {
			t.Fatalf("DiscoverWorkspace(%q) = (%q, found=%v), want root %q", start, gotRoot, found, root)
		}
		if config.Version != 1 || config.Project != "foo" || !reflect.DeepEqual(config.Scope, []string{"*foo*"}) {
			t.Fatalf("unexpected workspace config: %#v", config)
		}
	}
}

func TestWorkspaceDirectoryHelpersAndDataDirPrecedence(t *testing.T) {
	t.Setenv("JXSCOUT_PROJECT_ROOT", "/workspace/jxscout")
	t.Setenv("JXSCOUT_DATA_DIR", "")
	if got := GetWorkingDirectory("foo"); got != "/workspace/jxscout" {
		t.Fatalf("GetWorkingDirectory() = %q, want workspace root", got)
	}
	if got := GetPrivateDirectoryRoot(); got != filepath.Join("/workspace/jxscout", ".state") {
		t.Fatalf("GetPrivateDirectoryRoot() = %q", got)
	}
	if got := GetPrivateDirectory("foo"); got != filepath.Join("/workspace/jxscout", ".state", "foo") {
		t.Fatalf("GetPrivateDirectory() = %q", got)
	}

	t.Setenv("JXSCOUT_DATA_DIR", "/explicit/data")
	if got := GetWorkingDirectory("foo"); got != filepath.Join("/explicit/data", "foo") {
		t.Fatalf("data dir did not override workspace: %q", got)
	}
	if got := GetPrivateDirectoryRoot(); got != filepath.Join("/explicit/data", ".state") {
		t.Fatalf("private root did not honor data dir: %q", got)
	}
}

func TestWorkspaceDetectionIgnoresExecutableNamedJXScout(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "jxscout"), []byte("binary"), 0700); err != nil {
		t.Fatal(err)
	}
	_, _, found, err := DiscoverWorkspace(dir)
	if err != nil || found {
		t.Fatalf("got found=%v err=%v for a regular executable", found, err)
	}
}
