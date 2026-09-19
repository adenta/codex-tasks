#!/bin/sh
set -eu
case "$1" in
  models) printf '%s\n' '{"models":[{"id":"test/model","name":"Test model","context_length":32000,"reasoning":{"supported_efforts":["high","low"]}}],"refreshed_at":"2026-09-18T00:00:00Z"}' ;;
  _clipboard-image|_import-image)
    sleep 0.1
    case "$0" in
      *text-clipboard) printf '%s\n' '{"image":false}' ;;
      *) printf '%s\n' '{"image":true,"path":"/tmp/fixture.png","url":""}' ;;
    esac ;;
  _discard-images) exit 0 ;;
  targets) printf '%s\n' '{"targets":[{"host":"workstation","local":true},{"host":"grace"}],"desktop_projects":[{"host":"grace","path":"/projects/one","name":"Fixture project"}]}' ;;
  projects) printf '%s\n' '{"projects":[]}' ;;
  environments)
    case " $* " in
      *' /projects/slow '*) sleep 0.4; printf '%s\n' '{"environment_git":true,"environments":[{"id":"stale.toml","name":"Stale"}]}' ;;
      *' /projects/multiple '*) printf '%s\n' '{"environment_git":true,"environments":[{"id":"one.toml","name":"One"},{"id":"two.toml","name":"Two"}]}' ;;
      *' /projects/plain '*) printf '%s\n' '{"environment_git":false}' ;;
      *' /projects/error '*) printf '%s\n' '{"error":"Discovery failed"}'; exit 1 ;;
      *) printf '%s\n' '{"environment_git":true,"environments":[{"id":"environment.toml","name":"Fixture environment"}]}' ;;
    esac ;;
  create)
    prompt=$(cat)
    sleep 0.2
    case "$prompt" in
      environment)
        case " $* " in *' --reasoning-effort low '*) ;; *) exit 9;; esac
        case " $* " in *' --environment environment.toml '*) ;; *) exit 9;; esac
        printf '%s\n' '{"input_accepted":true,"task":{"id":"environment-id"}}' ;;
      setup-failed)
        printf '%s\n' '{"outcome":"failed","error":"Setup failed; inspect worktree","worktree":"/tmp/retained-fixture","setup_status":"failed"}'; exit 1 ;;
      images)
        case " $* " in *' --image /tmp/fixture.png '*) ;; *) exit 9;; esac
        case " $* " in *' --model-provider openrouter --model fixture/model --model-context-window 32000 '*) ;; *) exit 9;; esac
        printf '%s\n' '{"input_accepted":true,"task":{"id":"image-id"}}' ;;
      background)
        case " $* " in *' --reasoning-effort'*) exit 9;; esac
        case " $* " in *' --wait-history '*) exit 9;; esac
        printf '%s\n' '{"input_accepted":true,"task":{"id":"background-id"}}' ;;
      foreground)
        case " $* " in *' --reasoning-effort low '*) ;; *) exit 9;; esac
        case " $* " in *' --model-provider openrouter --model test/model --model-context-window 32000 '*) ;; *) exit 9;; esac
        case " $* " in *' --wait-history '*) ;; *) exit 9;; esac
        printf '%s\n' '{"input_accepted":true,"history_ready":true,"task":{"id":"foreground-id"}}' ;;
      failed) printf '%s\n' '{"outcome":"failed","error":"Rejected before creation"}'; exit 1 ;;
      uncertain) printf '%s\n' '{"outcome":"unknown","error":"Connection lost"}'; exit 1 ;;
      *) exit 9 ;;
    esac ;;
  *) exit 9 ;;
esac
