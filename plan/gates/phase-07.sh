#!/usr/bin/env bash
set -euo pipefail
source "$NMC_ROOT/plan/gate-lib.sh"
need_file internal/vfs/vfs.go
need_file internal/vfs/mem.go
need_file internal/vfs/cached.go
need_tests ./internal/vfs 'TestMemFilesystemSupportsDirectoriesAndFiles|TestCachedFilesystemReturnsCopiesAndInvalidatesAfterWrites|TestCachedFilesystemCachesSmallContentAndInvalidates'
need_tests ./internal/panel 'TestPanelDrawsFullLayoutAtEightyByTwentyFour|TestPanelDrawsAtFortyByTenAndKeepsCursorVisible'
need_build
need_suite
