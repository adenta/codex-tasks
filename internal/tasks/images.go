package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/url"
	"os"
	"os/exec"
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
		if !entry.IsDir() || !(strings.HasPrefix(name, "draft-") || strings.HasPrefix(name, "send-")) {
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

func ownedImageDir(p endpoints.Config, dir, prefix string) bool {
	if filepath.Dir(dir) != imageRoot(p) || !strings.HasPrefix(filepath.Base(dir), prefix) {
		return false
	}
	_, err := uuid.Parse(strings.TrimPrefix(filepath.Base(dir), prefix))
	return err == nil
}

func localImagePath(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
		value = value[1 : len(value)-1]
	}
	if strings.HasPrefix(value, "file:") {
		u, err := url.Parse(value)
		if err != nil || u.Scheme != "file" || (u.Host != "" && u.Host != "localhost") || u.RawQuery != "" || u.Fragment != "" {
			return ""
		}
		value = u.Path
	}
	if strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		value = filepath.Join(home, value[2:])
	}
	if !filepath.IsAbs(value) || strings.ContainsAny(value, "\x00\r\n") {
		return ""
	}
	return value
}

// Import a private copy so removing a draft never removes the selected file.
func importImage(p endpoints.Config, value string, stdout io.Writer) error {
	source := localImagePath(value)
	if source == "" {
		return fmt.Errorf("select a local image file")
	}
	_, ext, err := imageInfo(source)
	if err != nil {
		return err
	}
	dir, err := newImageDir(p, "draft-")
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(dir)
		}
	}()
	path := filepath.Join(dir, "image"+ext)
	if err = copyImage(source, path); err != nil {
		return err
	}
	if _, _, err = imageInfo(path); err != nil {
		return err
	}
	err = json.NewEncoder(stdout).Encode(map[string]any{"image": true, "path": path, "url": (&url.URL{Scheme: "file", Path: path}).String()})
	ok = err == nil
	return err
}

func clipboardImage(ctx context.Context, p endpoints.Config, stdout io.Writer) error {
	types := exec.CommandContext(ctx, "wl-paste", "--list-types")
	var available limitedBuffer
	types.Stdout = &available
	if err := types.Run(); err != nil {
		return fmt.Errorf("cannot read clipboard; wl-paste and a Wayland session are required")
	}
	mime := ""
	for _, candidate := range []string{"image/png", "image/jpeg"} {
		for _, line := range strings.Split(string(available.data), "\n") {
			if line == candidate {
				mime = candidate
				break
			}
		}
		if mime != "" {
			break
		}
	}
	if mime == "" {
		for _, line := range strings.Split(string(available.data), "\n") {
			if strings.HasPrefix(line, "image/") {
				return fmt.Errorf("clipboard images must be PNG or JPEG")
			}
		}
		for _, candidate := range []string{"text/uri-list", "text/plain;charset=utf-8", "text/plain", "UTF8_STRING"} {
			if !strings.Contains("\n"+string(available.data)+"\n", "\n"+candidate+"\n") {
				continue
			}
			cmd := exec.CommandContext(ctx, "wl-paste", "--no-newline", "--type", candidate)
			var content limitedBuffer
			cmd.Stdout = &content
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("could not read clipboard text")
			}
			path := localImagePath(string(content.data))
			ext := strings.ToLower(filepath.Ext(path))
			if path != "" && (ext == ".png" || ext == ".jpg" || ext == ".jpeg") {
				return importImage(p, path, stdout)
			}
			break
		}
		return json.NewEncoder(stdout).Encode(map[string]any{"image": false})
	}
	dir, err := newImageDir(p, "draft-")
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(dir)
		}
	}()
	ext := ".png"
	if mime == "image/jpeg" {
		ext = ".jpg"
	}
	path := filepath.Join(dir, "clipboard"+ext)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "wl-paste", "--no-newline", "--type", mime)
	cmd.Stdout = &imageWriter{w: file}
	runErr := cmd.Run()
	closeErr := file.Close()
	if runErr != nil {
		return fmt.Errorf("clipboard image could not be read (maximum 10 MiB)")
	}
	if closeErr != nil {
		return closeErr
	}
	if _, _, err = imageInfo(path); err != nil {
		return err
	}
	err = json.NewEncoder(stdout).Encode(map[string]any{"image": true, "path": path, "url": (&url.URL{Scheme: "file", Path: path}).String()})
	ok = err == nil
	return err
}

type imageWriter struct {
	w io.Writer
	n int
}

func (w *imageWriter) Write(b []byte) (int, error) {
	if w.n+len(b) > maxImageBytes {
		return 0, fmt.Errorf("image exceeds 10 MiB")
	}
	n, err := w.w.Write(b)
	w.n += n
	return n, err
}

func discardDrafts(p endpoints.Config, paths []string) error {
	for _, path := range paths {
		dir := filepath.Dir(path)
		if !ownedImageDir(p, dir, "draft-") {
			return fmt.Errorf("not a launcher draft")
		}
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
	}
	return nil
}

// Bound clipboard utility output before interpreting it.
type limitedBuffer struct{ data []byte }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > 8<<20 {
		return 0, fmt.Errorf("utility output exceeds limit")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}
