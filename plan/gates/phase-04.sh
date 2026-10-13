#!/usr/bin/env bash
set -euo pipefail
source "$NMC_ROOT/plan/gate-lib.sh"
need_file internal/viewer/viewer.go
need_tests ./internal/viewer 'TestBinaryContentOpensInHexAndSearchesBytes|TestOpenReportsWhenTheFileExceedsTheLimit'
need_tests ./internal/app 'TestF3OpensViewerAndViewerSearchesThenCloses'
need_build
need_suite
