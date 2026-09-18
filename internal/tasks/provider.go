package tasks

import (
	"fmt"
	"strings"
)

// OpenRouter owns this preset's routing preferences. Edits to the saved preset
// affect subsequent requests, including existing tasks. To rename the preset,
// change this constant and deploy the CLI on execution hosts; routing changes
// within the same preset do not require a CLI deployment.
const openRouterPreset = "codex-tasks"

func creationModel(provider, model string) (string, error) {
	if provider != "openrouter" || model == "" {
		return model, nil
	}
	base, preset, qualified := strings.Cut(model, "@preset/")
	if qualified {
		if base == "" || preset != openRouterPreset {
			return "", fmt.Errorf("OpenRouter tasks require an explicit model and @preset/%s", openRouterPreset)
		}
		return model, nil
	}
	return model + "@preset/" + openRouterPreset, nil
}
