package tasks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// Copy only ignored, regular local guidance; never overwrite existing guidance.
func (s *service) copyLocalOverride(ctx context.Context, sourceRoot, destinationRoot string) error {
	const name = "AGENTS.override.md"
	source := filepath.Join(sourceRoot, name)
	info, err := s.stat(ctx, source, false)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	r, err := s.commandResult(ctx, "", false, "git", "-C", sourceRoot, "check-ignore", "-q", "--", name)
	if err != nil {
		return err
	}
	if *r.ExitCode == 1 {
		return nil
	}
	if *r.ExitCode != 0 {
		return fmt.Errorf("cannot check override ignore status")
	}
	// cp -n does not follow or replace a preexisting destination, including a dangling symlink.
	_, err = s.command(ctx, "", true, "cp", "-n", "-P", "--preserve=mode", "--", source, filepath.Join(destinationRoot, name))
	return err
}
