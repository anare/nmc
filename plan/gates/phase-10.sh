#!/usr/bin/env bash
set -euo pipefail
source "$NMC_ROOT/plan/gate-lib.sh"
need_file internal/find/find.go
need_tests ./internal/find 'TestSearchStreamsLiteralWholeWordHitsAndSkipsHiddenFiles|TestSearchRejectsInvalidGlobAndHonoursCancellation'
need_tests ./internal/app 'TestFindActionsUseFindKeymap'
need_build
need_suite
