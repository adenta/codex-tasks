#!/bin/sh
set -eu
case "$1" in
  _clipboard-image)
    sleep 0.1
    case "$0" in
      *text-clipboard) printf '%s\n' '{"image":false}' ;;
      *) printf '%s\n' '{"image":true,"path":"/tmp/fixture.png","url":""}' ;;
    esac ;;
  _discard-images) exit 0 ;;
  targets) printf '%s\n' '{"targets":[{"host":"workstation","local":true},{"host":"grace"}]}' ;;
  projects) printf '%s\n' '{"projects":[]}' ;;
  create)
    prompt=$(cat)
    sleep 0.2
    case "$prompt" in
      images)
        case " $* " in *' --image /tmp/fixture.png '*) ;; *) exit 9;; esac
        printf '%s\n' '{"input_accepted":true,"task":{"id":"image-id"}}' ;;
      background)
        case " $* " in *' --wait-history '*) exit 9;; esac
        printf '%s\n' '{"input_accepted":true,"task":{"id":"background-id"}}' ;;
      foreground)
        case " $* " in *' --wait-history '*) ;; *) exit 9;; esac
        printf '%s\n' '{"input_accepted":true,"history_ready":true,"task":{"id":"foreground-id"}}' ;;
      failed) printf '%s\n' '{"outcome":"failed","error":"Rejected before creation"}'; exit 1 ;;
      uncertain) printf '%s\n' '{"outcome":"unknown","error":"Connection lost"}'; exit 1 ;;
      *) exit 9 ;;
    esac ;;
  *) exit 9 ;;
esac
