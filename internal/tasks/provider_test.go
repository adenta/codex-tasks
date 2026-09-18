package tasks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreationPresetAndInitialInput(t *testing.T) {
	for _, tc := range []struct{ provider, model, want string }{
		{"openrouter", "vendor/model", "vendor/model@preset/codex-tasks"},
		{"openrouter", "vendor/model@preset/codex-tasks", "vendor/model@preset/codex-tasks"},
		{"custom", "vendor/model", "vendor/model"},
		{"", "", "configured-model"},
	} {
		t.Run(tc.provider+tc.model, func(t *testing.T) {
			o := opts("create", "")
			o.CWD, o.Projectless = t.TempDir(), true
			o.ModelProvider, o.Model, o.ContextWindow = tc.provider, tc.model, 32000
			o.Message = "hello"
			o.Images = []string{"/fixture/image.png"}
			f := &fakeRPC{handle: func(m string, p map[string]any) (any, error) {
				switch m {
				case "thread/start":
					if tc.model != "" && p["model"] != tc.want {
						t.Fatalf("wrong effective model: %#v", p)
					}
					if tc.model == "" && p["model"] != nil {
						t.Fatal("overrode server default")
					}
					if p["config"].(map[string]any)["model_context_window"] != 32000 {
						t.Fatal("lost context limit")
					}
					return map[string]any{"thread": storedTask(), "model": tc.want, "modelProvider": tc.provider}, nil
				case "turn/start":
					input := p["input"].([]any)
					if len(input) != 2 || input[0].(map[string]any)["text"] != "hello" || input[1].(map[string]any)["path"] != o.Images[0] {
						t.Fatal("lost initial input")
					}
					return map[string]any{"turn": Turn{ID: "turn", Status: "completed"}}, nil
				default:
					return nil, fmt.Errorf("unexpected RPC: %s", m)
				}
			}}
			s := service{rpc: f, home: t.TempDir()}
			r := Result{}
			if err := s.create(context.Background(), o, &r); err != nil || !r.InputAccepted || r.Task.Model != tc.want || len(f.methods) != 2 {
				t.Fatalf("create: %v %+v", err, r)
			}
		})
	}
}

func TestConflictingPresetFailsBeforeMutation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, model := range []string{"vendor/model@preset/other", "@preset/codex-tasks", "vendor/model@preset/codex-tasks@preset/codex-tasks", "vendor/model@preset/"} {
		o := opts("create", "")
		o.Projectless, o.ModelProvider, o.Model = true, "openrouter", model
		f := &fakeRPC{handle: func(m string, p map[string]any) (any, error) { t.Fatalf("mutation attempted: %s", m); return nil, nil }}
		s := service{rpc: f, home: home}
		r := Result{}
		if err := s.create(context.Background(), o, &r); err == nil || r.Workspace != "" || r.Created {
			t.Fatalf("accepted conflict: %+v %v", r, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "Documents")); !os.IsNotExist(err) {
		t.Fatal("allocated projectless workspace before validation")
	}
}

func TestPresetFailureNeverFallsBack(t *testing.T) {
	for _, failure := range []string{"model-mismatch", "missing-preset", "inference-error"} {
		t.Run(failure, func(t *testing.T) {
			o := opts("create", "")
			o.CWD = t.TempDir()
			o.Projectless = true
			o.ModelProvider = "openrouter"
			o.Model = "vendor/model"
			o.Message = "hello"
			f := &fakeRPC{handle: func(m string, p map[string]any) (any, error) {
				if m == "thread/start" {
					if p["model"] != "vendor/model@preset/codex-tasks" {
						t.Fatal("unrestricted retry")
					}
					model := p["model"]
					if failure == "model-mismatch" {
						model = o.Model
					}
					return map[string]any{"thread": storedTask(), "model": model, "modelProvider": "openrouter"}, nil
				}
				if m == "turn/start" && failure != "model-mismatch" {
					return nil, fmt.Errorf("%s", failure)
				}
				t.Fatalf("unexpected retry/RPC %s", m)
				return nil, nil
			}}
			s := service{rpc: f, home: t.TempDir()}
			r := Result{}
			if err := s.create(context.Background(), o, &r); err == nil {
				t.Fatal("failure suppressed")
			}
			want := 2
			if failure == "model-mismatch" {
				want = 1
			}
			if len(f.methods) != want {
				t.Fatal("retried failed request")
			}
		})
	}
}

func TestProviderMismatchNeverStartsPaidTurn(t *testing.T) {
	o := opts("create", "")
	o.CWD = t.TempDir()
	o.Projectless = true
	o.Model = "vendor/model"
	o.ModelProvider = "custom"
	o.ContextWindow = 32000
	o.Message = "must not be sent"
	task := storedTask()
	f := &fakeRPC{handle: func(m string, p map[string]any) (any, error) {
		if m != "thread/start" {
			return nil, fmt.Errorf("unexpected RPC: %s", m)
		}
		if p["modelProvider"] != "custom" || p["model"] != "vendor/model" || p["serviceTier"] != "default" {
			t.Fatalf("wrong request: %#v", p)
		}
		if p["config"].(map[string]any)["model_context_window"] != 32000 {
			t.Fatal("context not supplied")
		}
		return map[string]any{"thread": task, "model": "vendor/model", "modelProvider": "wrong"}, nil
	}}
	s := service{rpc: f, home: t.TempDir()}
	r := Result{}
	err := s.create(context.Background(), o, &r)
	if err == nil || !strings.Contains(err.Error(), "provider") || r.Task == nil || len(f.methods) != 1 {
		t.Fatalf("unsafe mismatch handling: %v %+v", err, r)
	}
}
func TestProviderFlagValidation(t *testing.T) {
	for _, args := range [][]string{{"create", "--projectless", "--model-provider", "custom"}, {"create", "--projectless", "--model-context-window", "32000"}, {"list", "--model-provider", "custom"}} {
		if _, err := parse(args, strings.NewReader("")); err == nil {
			t.Fatalf("accepted invalid flags: %v", args)
		}
	}
	o, err := parse([]string{"create", "--projectless", "--model-provider", "custom", "--model", "vendor/model", "--model-context-window", "32000"}, strings.NewReader(""))
	if err != nil || o.ModelProvider != "custom" || o.ContextWindow != 32000 {
		t.Fatalf("%+v %v", o, err)
	}
}

func TestCreationReasoningEffort(t *testing.T) {
	for _, effort := range []string{"", "low"} {
		for _, replyEffort := range []string{"", "low", "high"} {
			t.Run(effort+"/"+replyEffort, func(t *testing.T) {
				o := opts("create", "")
				o.CWD = t.TempDir()
				o.Projectless = true
				o.ReasoningEffort = effort
				o.ContextWindow = 32000
				o.Message = "hello"
				f := &fakeRPC{handle: func(m string, p map[string]any) (any, error) {
					if m == "thread/start" {
						config := p["config"].(map[string]any)
						if config["model_context_window"] != 32000 {
							t.Fatal("lost context")
						}
						if effort == "" {
							if _, ok := config["model_reasoning_effort"]; ok {
								t.Fatal("default override")
							}
						} else if config["model_reasoning_effort"] != effort {
							t.Fatal("lost effort")
						}
						task := storedTask()
						task.ReasoningEffort = nil
						reply := map[string]any{"thread": task, "model": "configured-model"}
						if replyEffort != "" {
							reply["reasoningEffort"] = replyEffort
						}
						return reply, nil
					}
					if m == "turn/start" {
						return map[string]any{"turn": Turn{ID: "turn", Status: "completed"}}, nil
					}
					return nil, fmt.Errorf("unexpected %s", m)
				}}
				s := service{rpc: f, home: t.TempDir()}
				r := Result{}
				err := s.create(context.Background(), o, &r)
				mismatch := effort != "" && effort != replyEffort
				if mismatch {
					if err == nil || r.InputAccepted || len(f.methods) != 1 || r.Task == nil {
						t.Fatalf("unsafe mismatch: %+v %v", r, err)
					}
				} else if err != nil || !r.InputAccepted {
					t.Fatalf("creation failed: %+v %v", r, err)
				}
			})
		}
	}
}

func TestReasoningEffortFlag(t *testing.T) {
	for _, value := range []string{"low", "max", "xhigh", "none"} {
		o, err := parse([]string{"create", "--projectless", "--reasoning-effort", value}, strings.NewReader(""))
		if err != nil || o.ReasoningEffort != value {
			t.Fatalf("%+v %v", o, err)
		}
	}
	for _, args := range [][]string{{"list", "--reasoning-effort", "low"}, {"create", "--projectless", "--reasoning-effort", "bad value"}, {"create", "--projectless", "--reasoning-effort", "default"}} {
		if _, err := parse(args, strings.NewReader("")); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
