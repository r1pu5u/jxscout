// Package webcrack provides an in-place JavaScript formatting mode.
package webcrack

import (
	"context"
	"fmt"
	"github.com/francisconeves97/jxscout/internal/core/common"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Format disables bundle extraction and replaces the input only after success.
// stdout contains JavaScript; webcrack diagnostics remain on stderr.
func Format(ctx context.Context, file string) error {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	info, err := os.Stat(file)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(file), ".jxscout-format-*.js")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	cmd := exec.CommandContext(ctx, "webcrack", file, "--no-unpack")
	diagnostics := common.NewTailBuffer(8192)
	// An *os.File lets the child stream stdout directly to disk. The original
	// script is replaced only after successful completion and a nonempty result.
	cmd.Stdout, cmd.Stderr = temp, diagnostics
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("webcrack: %w: %.2000s", err, diagnostics.String())
	}
	output, err := temp.Stat()
	if err != nil {
		return err
	}
	if output.Size() == 0 {
		return fmt.Errorf("webcrack returned empty JavaScript")
	}
	if err = temp.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), file)
}
