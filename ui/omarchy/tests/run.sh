#!/bin/sh
set -eu
src=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
test_root=$(mktemp -d /tmp/codex-launcher-test.XXXXXX)
trap 'rm -rf "$test_root"' EXIT
ln -s /usr/share/omarchy/shell/Commons "$test_root/Commons"
ln -s /usr/share/omarchy/shell/Ui "$test_root/Ui"
ln -s "$src" "$test_root/Launcher"
cp "$src/tests/Fixture.qml" "$test_root/shell.qml"
QT_QPA_PLATFORM=offscreen QT_QPA_PLATFORMTHEME=generic QT_QUICK_CONTROLS_STYLE=Basic XDG_RUNTIME_DIR="$test_root" CODEX_TASKS_LAUNCHER_SETTINGS="$test_root/test.ini" timeout 15 quickshell -p "$test_root/shell.qml" >"$test_root/log" 2>&1
cat "$test_root/log"
grep -q LAUNCHER_FIXTURE_PASS "$test_root/log"
! grep -q 'Error:' "$test_root/log"
grep -q mode=checkout "$test_root/test.ini"
cp "$src/tests/Delivery.qml" "$test_root/shell.qml"
QT_QPA_PLATFORM=offscreen QT_QPA_PLATFORMTHEME=generic QT_QUICK_CONTROLS_STYLE=Basic XDG_RUNTIME_DIR="$test_root" CODEX_TASKS_LAUNCHER_SETTINGS="$test_root/delivery.ini" LAUNCHER_TEST_CLI="$src/tests/fake-cli.sh" timeout 15 quickshell -p "$test_root/shell.qml" >"$test_root/delivery.log" 2>&1
cat "$test_root/delivery.log"
grep -q DELIVERY_FIXTURE_PASS "$test_root/delivery.log"
! grep -q 'Error:' "$test_root/delivery.log"
cp "$src/tests/Environments.qml" "$test_root/shell.qml"
QT_QPA_PLATFORM=offscreen QT_QPA_PLATFORMTHEME=generic QT_QUICK_CONTROLS_STYLE=Basic XDG_RUNTIME_DIR="$test_root" CODEX_TASKS_LAUNCHER_SETTINGS="$test_root/environments.ini" LAUNCHER_TEST_CLI="$src/tests/fake-cli.sh" timeout 15 quickshell -p "$test_root/shell.qml" >"$test_root/environments.log" 2>&1
cat "$test_root/environments.log"
grep -q ENVIRONMENT_FIXTURE_PASS "$test_root/environments.log"
! grep -q 'Error:' "$test_root/environments.log"
cp "$src/tests/Images.qml" "$test_root/shell.qml"
ln -s "$src/tests/fake-cli.sh" "$test_root/text-clipboard"
QT_QPA_PLATFORM=offscreen QT_QPA_PLATFORMTHEME=generic QT_QUICK_CONTROLS_STYLE=Basic XDG_RUNTIME_DIR="$test_root" CODEX_TASKS_LAUNCHER_SETTINGS="$test_root/images.ini" LAUNCHER_TEST_CLI="$src/tests/fake-cli.sh" LAUNCHER_TEST_TEXT_CLI="$test_root/text-clipboard" timeout 15 quickshell -p "$test_root/shell.qml" >"$test_root/images.log" 2>&1
cat "$test_root/images.log"
grep -q IMAGE_FIXTURE_PASS "$test_root/images.log"
! grep -q 'Error:' "$test_root/images.log"
node "$src/tests/inference.test.cjs"
cp "$src/tests/Inference.qml" "$test_root/shell.qml"
QT_QPA_PLATFORM=offscreen QT_QPA_PLATFORMTHEME=generic QT_QUICK_CONTROLS_STYLE=Basic XDG_RUNTIME_DIR="$test_root" timeout 15 quickshell -p "$test_root/shell.qml" >"$test_root/inference.log" 2>&1
cat "$test_root/inference.log"
grep -q INFERENCE_QML_PASS "$test_root/inference.log"
! grep -q 'Error:' "$test_root/inference.log"
