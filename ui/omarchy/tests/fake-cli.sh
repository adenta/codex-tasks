#!/bin/sh
set -eu
case "$1" in
  models) printf '%s\n' '{"models":[{"id":"test/model","name":"Test model","context_length":32000}],"refreshed_at":"2026-09-18T00:00:00Z"}' ;;
  targets) printf '%s\n' '{"targets":[{"host":"workstation","local":true},{"host":"grace"}]}' ;;
  projects) printf '%s\n' '{"projects":[]}' ;;
  create)
    prompt=$(cat)
    sleep 0.2
    case "$prompt" in
      background)
        case " $* " in *' --wait-history '*) exit 9;; esac
        printf '%s\n' '{"input_accepted":true,"task":{"id":"background-id"}}' ;;
      foreground)
        case " $* " in *' --model-provider openrouter --model test/model --model-context-window 32000 '*) ;; *) exit 9;; esac
        case " $* " in *' --wait-history '*) ;; *) exit 9;; esac
        printf '%s\n' '{"input_accepted":true,"history_ready":true,"task":{"id":"foreground-id"}}' ;;
      failed) printf '%s\n' '{"outcome":"failed","error":"Rejected before creation"}'; exit 1 ;;
      uncertain) printf '%s\n' '{"outcome":"unknown","error":"Connection lost"}'; exit 1 ;;
      *) exit 9 ;;
    esac ;;
  *) exit 9 ;;
esac
