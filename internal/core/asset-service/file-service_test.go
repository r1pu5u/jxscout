package assetservice

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJavaScriptOutputKeepsOneJSFile(t *testing.T) {
	root := t.TempDir()
	service := NewFileService(root, slog.New(slog.NewTextHandler(io.Discard, nil)))
	code := "const script = true;"
	for _, rawURL := range []string{"https://example.com/js/app.1234.js", "https://example.com/runtime/v1.1234?version=1"} {
		path, err := service.SaveInSubfolder(context.Background(), "original", SaveFileRequest{PathURL: rawURL, Content: &code, JavaScript: true})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".js.js") {
			t.Fatalf("bad script filename: %s", path)
		}
		if exists, err := service.FileExists(rawURL, "original"); err != nil || !exists {
			t.Fatalf("saved script not found: %v %v", exists, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "raw")); !os.IsNotExist(err) {
		t.Fatal("unexpected raw output")
	}
}
