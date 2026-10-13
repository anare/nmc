#!/usr/bin/env bash
set -euo pipefail
source "$NMC_ROOT/plan/gate-lib.sh"
need_file internal/panel/panel.go
need_file internal/vfs/vfs.go
need_tests ./internal/panel 'TestPanelDrawsFullLayoutAtEightyByTwentyFour|TestPanelDrawsAtFortyByTenAndKeepsCursorVisible|TestPanelRestoresTheCursorWhenReturningToAParent|TestPanelCannotMarkTheParentAndMarkingAdvances'
need_tests ./internal/vfs 'TestLocalFilesystemListsParentAndSymlinkMetadata|TestMemFilesystemSupportsDirectoriesAndFiles'
need_build
need_suite
