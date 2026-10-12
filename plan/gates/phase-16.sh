#!/usr/bin/env bash
set -euo pipefail
source "$NMC_ROOT/plan/gate-lib.sh"
need_file internal/git/git.go
need_tests ./internal/git 'TestEmptyRepositoryHasEmptyHistory|TestGitReadWriteWorkflow'
need_tests ./internal/app 'TestGitHistoryCanCommitWithoutExistingCommits|TestGitHistoryCanSwitchBranchesAndReloadBothPanels|TestF3OnParentShowsLocalGitHistory'
need_build
need_suite
