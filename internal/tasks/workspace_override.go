package tasks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// copyLocalOverride prepares local guidance before thread/start loads it.
// It deliberately does not copy other untracked files or run environment setup.
func copyLocalOverride(ctx context.Context, sourceRoot, destinationRoot string) error {
	const name = "AGENTS.override.md"
	source := filepath.Join(sourceRoot, name)
	info, err := os.Lstat(source)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	cmd := exec.CommandContext(ctx, "git", "-C", sourceRoot, "check-ignore", "-q", "--", name)
	if err := cmd.Run(); err != nil {
		var status *exec.ExitError
		if errors.As(err, &status) && status.ExitCode() == 1 {
			return nil
		}
		return fmt.Errorf("check override ignore status: %w", err)
	}
	destination := filepath.Join(destinationRoot, name)
	if _, err := os.Lstat(destination); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if os.IsExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return errors.Join(err, os.Remove(destination))
	}
	return nil
}
