#!/usr/bin/env bash
set -euo pipefail
source "$NMC_ROOT/plan/gate-lib.sh"
need_file internal/dialog/dialog.go
need_file internal/shell/run.go
need_tests ./internal/dialog 'TestInputDialogAcceptsAndCancels|TestTextInputEditingAndWordDeletion|TestProgressDialogClampsAndEscapeRequestsCancellation'
need_tests ./internal/app 'TestCommandLineOwnsEditingKeysOnlyWhenItHasText|TestCommandRunsInBackgroundAndOutputCanBeViewed'
need_build
need_suite
