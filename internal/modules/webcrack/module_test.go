package webcrack

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFormatReplacesOnlyJavaScriptAndPreservesInputOnFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "webcrack")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n[ \"$2\" = '--no-unpack' ] || exit 3\nprintf 'diagnostic only\\n' >&2\nprintf 'const formatted = true;\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	file := filepath.Join(dir, "app.js")
	if err := os.WriteFile(file, []byte("const formatted=!0"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Format(context.Background(), file); err != nil {
		t.Fatal(err)
	}
	code, _ := os.ReadFile(file)
	if string(code) != "const formatted = true;\n" {
		t.Fatalf("unexpected output: %q", code)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatalf("extra output: %v", entries)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf 'partial broken output'\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := Format(context.Background(), file); err == nil {
		t.Fatal("expected formatter failure")
	}
	after, _ := os.ReadFile(file)
	if string(after) != string(code) || strings.Contains(string(after), "partial") {
		t.Fatal("formatter failure changed file")
	}
}
