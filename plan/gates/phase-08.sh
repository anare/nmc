#!/usr/bin/env bash
set -euo pipefail
source "$NMC_ROOT/plan/gate-lib.sh"
need_file internal/sshfs/fs.go
need_tests ./internal/sshfs 'TestParseGNUListing|TestParseBSDListingAndSymlinkRecords|TestParseLSListingPreservesNamesAndSymlinkTargets|TestParseBrokenSymlinkRecord|TestRemoteOperationsQuotePathsAndPreserveRemoteErrors'
need_tests ./internal/app 'TestCtrlBackslashConnectsAndDisconnectsRemotePanel'
need_build
need_suite
