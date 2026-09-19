// Package catalog provides authenticated Modal model discovery.
package catalog

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const endpoint = "https://inference.us-west.modal.direct/v1/models"
const maxBytes = 16 << 20

type Reasoning struct {
	SupportedEfforts []string `json:"supported_efforts"`
}

type Model struct {
	Reasoning     *Reasoning `json:"reasoning,omitempty"`
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	ContextLength int        `json:"context_length"`
}

type Result struct {
	MetadataVersion int       `json:"metadata_version"`
	Models          []Model   `json:"models"`
	RefreshedAt     time.Time `json:"refreshed_at"`
	Warning         string    `json:"warning,omitempty"`
}

func normalize(models []Model) []Model {
	result := []Model{}
	seen := map[string]bool{}
	for _, m := range models {
		m.ID = strings.TrimSpace(m.ID)
		m.Name = strings.TrimSpace(m.Name)
		if m.ID == "" || m.ContextLength <= 0 || seen[m.ID] {
			continue
		}
		if m.Name == "" {
			m.Name = m.ID
		}
		seen[m.ID] = true
		result = append(result, m)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func decode(r io.Reader, target any) error {
	data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxBytes {
		return fmt.Errorf("catalog exceeds %d bytes", maxBytes)
	}
	return json.Unmarshal(data, target)
}

func readCache(path string) (Result, error) {
	var result Result
	f, err := os.Open(path)
	if err != nil {
		return result, err
	}
	defer f.Close()
	if err = decode(f, &result); err != nil {
		return result, err
	}
	result.Models = normalize(result.Models)
	result.Warning = ""
	if len(result.Models) == 0 || result.RefreshedAt.IsZero() {
		return result, fmt.Errorf("invalid catalog cache")
	}
	return result, nil
}

func writeCache(path string, result Result) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".modal-models-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = json.NewEncoder(f).Encode(result); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func fetch(ctx context.Context, client *http.Client, url string) (Result, error) {
	var result Result
	token, err := proxyToken(ctx)
	if err != nil {
		return result, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return result, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("catalog HTTP status %d", resp.StatusCode)
	}
	var payload struct {
		Data []struct {
			Model
			ReasoningOptions []struct {
				Type   string   `json:"type"`
				Values []string `json:"values"`
			} `json:"reasoning_options"`
		} `json:"data"`
	}
	if err = decode(resp.Body, &payload); err != nil {
		return result, fmt.Errorf("invalid catalog: %w", err)
	}
	models := make([]Model, 0, len(payload.Data))
	for _, raw := range payload.Data {
		m := raw.Model
		m.Reasoning = nil
		for _, option := range raw.ReasoningOptions {
			if option.Type == "effort" {
				m.Reasoning = &Reasoning{SupportedEfforts: option.Values}
				break
			}
		}
		models = append(models, m)
	}
	result.Models = normalize(models)
	if len(result.Models) == 0 {
		return result, fmt.Errorf("catalog has no usable models")
	}
	result.MetadataVersion = 1
	result.RefreshedAt = time.Now().UTC()
	return result, nil
}

func load(ctx context.Context, client *http.Client, url, path string, refresh bool) (Result, error) {
	cached, cacheErr := readCache(path)
	if cacheErr == nil && cached.MetadataVersion >= 1 && !refresh {
		return cached, nil
	}
	result, err := fetch(ctx, client, url)
	if err != nil {
		if cacheErr == nil {
			cached.Warning = "Could not refresh models; showing cached models: " + err.Error()
			if cached.MetadataVersion < 1 {
				cached.MetadataVersion = 1 // Attempt migration once; explicit refresh retries.
				if saveErr := writeCache(path, cached); saveErr != nil {
					cached.Warning += "; could not save migration attempt: " + saveErr.Error()
				}
			}
			return cached, nil
		}
		return Result{}, err
	}
	if err = writeCache(path, result); err != nil {
		result.Warning = "Models loaded, but could not save cache: " + err.Error()
	}
	return result, nil
}

// proxyToken reads the caller's configured inference credential without exposing
// keyring errors (which can contain sensitive command output).
func proxyToken(ctx context.Context) (string, error) {
	token := strings.TrimSpace(os.Getenv("MODAL_PROXY_TOKEN"))
	if token == "" {
		output, err := exec.CommandContext(ctx, "secret-tool", "lookup", "application", "codex-tasks", "provider", "modal").Output()
		if err != nil {
			return "", fmt.Errorf("Modal credential unavailable: set MODAL_PROXY_TOKEN or unlock the desktop keyring")
		}
		token = strings.TrimSpace(string(output))
	}
	if !strings.HasPrefix(token, "wk-") || !strings.Contains(token, ".ws-") || strings.ContainsAny(token, " \r\n\t") {
		return "", fmt.Errorf("Modal requires a combined proxy token (wk-….ws-…)")
	}
	return token, nil
}

// Run handles the local models command independently of task configuration.
func Run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("models", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "JSON output")
	refresh := fs.Bool("refresh", false, "refresh the Modal workspace catalog")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "models accepts only --json and --refresh")
		return 2
	}
	dir, err := os.UserCacheDir()
	var result Result
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		result, err = load(ctx, &http.Client{Timeout: 15 * time.Second}, endpoint, filepath.Join(dir, "codex-tasks", "modal-models.json"), *refresh)
	}
	if err != nil {
		if *asJSON {
			json.NewEncoder(stdout).Encode(map[string]string{"error": err.Error()})
		} else {
			fmt.Fprintln(stderr, err)
		}
		return 1
	}
	if *asJSON {
		if err = json.NewEncoder(stdout).Encode(result); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	} else {
		for _, m := range result.Models {
			fmt.Fprintf(stdout, "%s\t%s\t%d\n", m.ID, m.Name, m.ContextLength)
		}
		if result.Warning != "" {
			fmt.Fprintln(stderr, result.Warning)
		}
	}
	return 0
}
