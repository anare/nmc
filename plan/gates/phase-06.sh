#!/usr/bin/env bash
set -euo pipefail
source "$NMC_ROOT/plan/gate-lib.sh"
need_file internal/shell/session.go
need_tests ./internal/shell 'TestInteractiveSessionRunsCommandsThroughPseudoTerminal'
need_tests ./internal/app 'TestCtrlOTogglesPersistentInteractiveShellSession'
need_build
need_suite
