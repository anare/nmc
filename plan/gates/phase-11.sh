#!/usr/bin/env bash
set -euo pipefail
source "$NMC_ROOT/plan/gate-lib.sh"
need_file internal/keymap/keymap.go
need_tests ./internal/keymap 'TestRebindRefusesReservedAndMovesBindingWithinMode|TestLoadSaveRetainSettingsAndOnlyWriteChangedBindings'
need_tests ./internal/app 'TestPanelCommandsFollowReboundKeymap|TestFunctionKeyHintsFollowCurrentBindings|TestShortcutEditorBindsPersistsAndRejectsReservedKeys'
need_build
need_suite
