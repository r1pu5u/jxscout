package agentcli

import (
	"context"
	"embed"
	"fmt"
	astanalyzer "github.com/francisconeves97/jxscout/internal/modules/ast-analyzer"
	"os"
	"os/exec"
	"path/filepath"
)

//go:embed dist/agent-cli.js
var bundle embed.FS

// Run executes the embedded standalone CLI with Bun, preserving exit status.
func Run(args []string) int {
	dir, err := os.MkdirTemp("", "jxscout-agent-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(dir)
	entries, _ := bundle.ReadDir("dist")
	for _, entry := range entries {
		data, _ := bundle.ReadFile("dist/" + entry.Name())
		if err := os.WriteFile(filepath.Join(dir, entry.Name()), data, 0600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	cmd := exec.CommandContext(context.Background(), "bun", append([]string{filepath.Join(dir, "agent-cli.js")}, args...)...)
	native, err := astanalyzer.NativeParser()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	nativePath := filepath.Join(dir, "parser.node")
	if err := os.WriteFile(nativePath, native, 0600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cmd.Env = append(os.Environ(), "NAPI_RS_NATIVE_LIBRARY_PATH="+nativePath)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			return e.ExitCode()
		}
		return 1
	}
	return 0
}
