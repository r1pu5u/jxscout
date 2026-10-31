// Package webcrack provides an in-place JavaScript formatting mode.
package webcrack

import (
	"bytes"
	"context"
	"fmt"
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
	cmd := exec.CommandContext(ctx, "webcrack", file, "--no-unpack")
	var code, diagnostics bytes.Buffer
	cmd.Stdout, cmd.Stderr = &code, &diagnostics
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("webcrack: %w: %.2000s", err, diagnostics.String())
	}
	if code.Len() == 0 {
		return fmt.Errorf("webcrack returned empty JavaScript")
	}
	info, err := os.Stat(file)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(file), ".jxscout-format-*.js")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err = temp.Write(code.Bytes()); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Chmod(info.Mode().Perm()); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), file)
}
