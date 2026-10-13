#!/usr/bin/env bash
set -euo pipefail
source "$NMC_ROOT/plan/gate-lib.sh"
need_file internal/history/history.go
need_tests ./internal/history 'TestHistoryIsSeparatedByKindAndSavedAtomically|TestHistoryCursorRestoresTheCurrentDraft'
need_tests ./internal/dialog 'TestComboInputHistoryDropdownFilterAndForget'
need_build
need_suite
