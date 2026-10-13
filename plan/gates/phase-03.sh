#!/usr/bin/env bash
set -euo pipefail
source "$NMC_ROOT/plan/gate-lib.sh"
need_file internal/fileop/fileop.go
need_tests ./internal/fileop 'TestRunContinuesAfterOnePairFails|TestRunCopiesAcrossFilesystemsAndClampsProgress'
need_tests ./internal/app 'TestF5CopiesAndF8DeletesThroughFileOperationLayer|TestF7InputDialogCreatesAndSelectsDirectory'
need_build
need_suite
