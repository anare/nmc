#!/usr/bin/env bash
set -euo pipefail
source "$NMC_ROOT/plan/gate-lib.sh"
need_file internal/app/app.go
need_grep 'foldShiftedFKey' internal/keymap/keymap.go
need_tests ./internal/app 'TestRenameCreateAndEditSymbolicLinkFlows'
need_tests ./internal/keymap 'TestParseStringAndEventNormaliseCtrlAndShiftedFunctionKeys'
need_build
need_suite
