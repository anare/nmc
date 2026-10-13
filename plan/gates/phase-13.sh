#!/usr/bin/env bash
set -euo pipefail
source "$NMC_ROOT/plan/gate-lib.sh"
need_file internal/vfs/vfs.go
need_tests ./internal/vfs 'TestEntryClassificationPrefersFileKindAndBackupName'
need_tests ./internal/ui 'TestDefaultThemeGivesEveryFileClassAUniquePalette'
need_tests ./internal/panel 'TestPanelFooterShowsSymlinkAndHardLinkDetails|TestDisplayNameKeepsTheIndicatorWhenTruncated'
need_build
need_suite
