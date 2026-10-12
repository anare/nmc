#!/usr/bin/env bash
set -euo pipefail
source "$NMC_ROOT/plan/gate-lib.sh"
need_file internal/editor/editor.go
need_tests ./internal/editor 'TestBinaryModeRoundTripsEveryByteExactly|TestTypingRunIsOneUndoStepAndRedoRestoresIt|TestMultiLineReplacementUndoRestoresAllLines|TestSaveWritesAtomicallyAndKeepsTabs'
need_build
need_suite
