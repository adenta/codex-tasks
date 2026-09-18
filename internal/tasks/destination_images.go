package tasks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/adenta/codex-tasks/internal/endpoints"
	"github.com/google/uuid"
)

func (s *service) executeWithImages(ctx context.Context, p endpoints.Config, o Options, r *Result) error {
	if len(o.Images) == 0 {
		return s.execute(ctx, o, r)
	}
	dir, images, err := stageImages(p, o.Images)
	if err != nil {
		r.ErrorCategory = "invalid_image"
		return err
	}
	defer os.RemoveAll(dir)
	root := filepath.Join(s.home, "codex-tasks", "attachments")
	if err = s.mkdir(ctx, root, true); err != nil {
		return err
	}
	info, err := s.stat(ctx, root, false)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("attachment root must be a real directory")
	}
	if _, err = s.command(ctx, "", true, "chmod", "0700", "--", root); err != nil {
		return err
	}
	entries, err := s.readDir(ctx, root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.FileName, "send-") {
			continue
		}
		if _, e := uuid.Parse(strings.TrimPrefix(entry.FileName, "send-")); e != nil {
			continue
		}
		path := filepath.Join(root, entry.FileName)
		info, e := s.stat(ctx, path, false)
		if e != nil {
			return e
		}
		if info.IsDir() && time.Since(info.ModTime()) > imageRetention {
			if e = s.remove(ctx, path, true); e != nil {
				return e
			}
		}
	}
	remoteDir := filepath.Join(root, "send-"+uuid.NewString())
	if err = s.mkdir(ctx, remoteDir, false); err != nil {
		return err
	}
	defer func() {
		// Retain attachments after acceptance or an uncertain write; never replay.
		if !s.uncertain && !r.InputAccepted {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if e := s.remove(cleanup, remoteDir, true); e != nil {
				r.AttachmentDirectory = remoteDir
			}
		} else {
			r.AttachmentDirectory = remoteDir
		}
	}()
	o.Images = nil
	for _, path := range images {
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		target := filepath.Join(remoteDir, filepath.Base(path))
		if e = s.writeFile(ctx, target, b); e != nil {
			r.ErrorCategory = "image_upload_failed"
			return e
		}
		if _, e = s.command(ctx, "", true, "chmod", "0600", "--", target); e != nil {
			return e
		}
		o.Images = append(o.Images, target)
	}
	return s.execute(ctx, o, r)
}
