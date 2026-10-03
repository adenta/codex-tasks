package tasks

import (
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/adenta/codex-tasks/internal/endpoints"
	"github.com/google/uuid"
)

const maxImages = 8
const maxImageBytes = 10 << 20
const maxImagesBytes = 40 << 20
const imageRetention = 7 * 24 * time.Hour

func imageRoot(p endpoints.Config) string {
	return filepath.Join(p.CodexHome, "codex-tasks", "attachments")
}

// Only our private, UUID-named directories are eligible for expiry. No daemon
// or user-selected source file is involved in cache cleanup.
func newImageDir(p endpoints.Config, prefix string) (string, error) {
	root := imageRoot(p)
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || !strings.HasPrefix(name, "send-") {
			continue
		}
		if _, err := uuid.Parse(name[strings.IndexByte(name, '-')+1:]); err != nil {
			continue
		}
		info, err := entry.Info()
		if err == nil && time.Since(info.ModTime()) > imageRetention {
			_ = os.RemoveAll(filepath.Join(root, name))
		}
	}
	dir := filepath.Join(root, prefix+uuid.NewString())
	return dir, os.Mkdir(dir, 0700)
}

func imageInfo(path string) (int64, string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, "", err
	}
	if !info.Mode().IsRegular() {
		return 0, "", fmt.Errorf("images must be regular files")
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return 0, "", err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > maxImageBytes {
		return 0, "", fmt.Errorf("images must be regular files of 1 byte–10 MiB")
	}
	cfg, format, err := image.DecodeConfig(io.LimitReader(f, maxImageBytes))
	if err != nil || (format != "png" && format != "jpeg") {
		return 0, "", fmt.Errorf("images must be PNG or JPEG")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 40_000_000 {
		return 0, "", fmt.Errorf("image exceeds 40 megapixels")
	}
	ext := ".png"
	if format == "jpeg" {
		ext = ".jpg"
	}
	return info.Size(), ext, nil
}

func validateImageFiles(paths []string) error {
	if len(paths) > maxImages {
		return fmt.Errorf("at most 8 images per message")
	}
	var total int64
	for _, path := range paths {
		n, _, err := imageInfo(path)
		if err != nil {
			return err
		}
		total += n
	}
	if total > maxImagesBytes {
		return fmt.Errorf("images exceed 40 MiB combined")
	}
	return nil
}

// Snapshot caller-owned files before stock file upload. Original source files
// are never deleted.
func stageImages(p endpoints.Config, paths []string) (dir string, staged []string, err error) {
	if err = validateImageFiles(paths); err != nil {
		return
	}
	dir, err = newImageDir(p, "send-")
	if err != nil {
		return
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dir)
		}
	}()
	for i, path := range paths {
		var ext string
		_, ext, err = imageInfo(path)
		if err != nil {
			return
		}
		dest := filepath.Join(dir, strconv.Itoa(i)+ext)
		err = copyImage(path, dest)
		if err != nil {
			return
		}
		staged = append(staged, dest)
	}
	err = validateImageFiles(staged)
	return
}

func copyImage(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(out, io.LimitReader(in, maxImageBytes+1))
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if n > maxImageBytes {
		return fmt.Errorf("image exceeds 10 MiB")
	}
	return closeErr
}
