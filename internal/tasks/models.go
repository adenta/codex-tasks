package tasks

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/adenta/codex-tasks/internal/catalog"
)

func modelCachePath(host, account string) string {
	base, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "codex-tasks", "modal-"+host+"-"+account+".json")
}

func (s *service) models(ctx context.Context, o Options, r *Result) error {
	var config struct {
		Config struct {
			Model     string `json:"model"`
			Provider  string `json:"model_provider"`
			Providers map[string]struct {
				BaseURL string `json:"base_url"`
			} `json:"model_providers"`
		} `json:"config"`
	}
	if err := s.call(ctx, "config/read", map[string]any{"includeLayers": false}, &config, false); err != nil {
		return err
	}
	r.DefaultModel = config.Config.Model
	r.DefaultProvider = config.Config.Provider
	if r.DefaultProvider == "" {
		r.DefaultProvider = "openai"
	}
	var page struct {
		Data []struct {
			Model     string `json:"model"`
			IsDefault bool   `json:"isDefault"`
		} `json:"data"`
	}
	if err := s.call(ctx, "model/list", map[string]any{"limit": 100}, &page, false); err == nil {
		for _, m := range page.Data {
			if m.IsDefault {
				r.SubscriptionModel = m.Model
				break
			}
		}
	}
	if r.DefaultProvider == "openai" && r.DefaultModel != "" {
		r.SubscriptionModel = r.DefaultModel
	}
	if r.DefaultModel == "" && r.DefaultProvider == "openai" {
		r.DefaultModel = r.SubscriptionModel
	}
	base := config.Config.Providers["modal"].BaseURL
	if base == "" {
		r.Warning = "Modal is not configured on this server."
		return nil
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("Modal catalog requires the configured local HTTP proxy")
	}
	result, err := catalog.Load(ctx, modelCachePath(r.Host, r.Account), o.Refresh, func(ctx context.Context) (catalog.Result, error) {
		body, err := s.command(ctx, "", false, "curl", "--fail", "--silent", "--show-error", "--max-time", "15", "--max-filesize", "16777216", strings.TrimRight(base, "/")+"/models")
		if err != nil {
			return catalog.Result{}, fmt.Errorf("Modal proxy catalog unavailable")
		}
		return catalog.Parse(strings.NewReader(body))
	})
	if err != nil {
		r.Warning = err.Error()
		return nil
	}
	r.Models = result.Models
	r.RefreshedAt = &result.RefreshedAt
	r.Warning = result.Warning
	return nil
}
