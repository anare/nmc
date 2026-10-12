#!/usr/bin/env bash
set -euo pipefail
source "$NMC_ROOT/plan/gate-lib.sh"
need_file internal/keymap/keymap.go
need_grep 'ctrlXPending' internal/app/app.go
need_tests ./internal/keymap 'TestLoadSaveRetainSettingsAndOnlyWriteChangedBindings|TestRebindRefusesReservedAndMovesBindingWithinMode'
need_tests ./internal/app 'TestF2SettingsPersistHiddenFilePreference|TestShortcutEditorBindsPersistsAndRejectsReservedKeys'
need_build
need_suite
