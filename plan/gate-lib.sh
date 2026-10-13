#!/usr/bin/env bash

set -euo pipefail

: "${NMC_ROOT:?NMC_ROOT must point at the repository root}"
: "${NMC_GATE_CACHE:?NMC_GATE_CACHE must point at a writable cache directory}"

mkdir -p "$NMC_GATE_CACHE"

fail() {
	printf 'FAIL: %s\n' "$*" >&2
	return 1
}

need_file() {
	[[ -f "$NMC_ROOT/$1" ]] || fail "required file is missing: $1"
}

need_grep() {
	local pattern=$1
	local file=$2
	grep -Eq "$pattern" "$NMC_ROOT/$file" || fail "pattern '$pattern' not found in $file"
}

cache_key() {
	printf '%s' "$1" | cksum | awk '{print $1}'
}

run_cached() {
	local key=$1
	shift
	local marker="$NMC_GATE_CACHE/$key.status"
	local output="$NMC_GATE_CACHE/$key.output"
	if [[ -f "$marker" ]]; then
		if [[ $(<"$marker") == 0 ]]; then
			printf 'cached check passed: %s\n' "$*"
			return 0
		fi
		cat "$output"
		return 1
	fi
	local status=0
	(cd "$NMC_ROOT" && "$@") >"$output" 2>&1 || status=$?
	cat "$output"
	printf '%s' "$status" >"$marker"
	return "$status"
}

need_tests() {
	local package=$1
	local pattern=$2
	local dir="$NMC_ROOT/${package#./}"
	[[ -d "$dir" ]] || fail "test package is missing: $package"
	grep -REq "func ${pattern}\\(" "$dir" --include='*_test.go' ||
		fail "no test in $package matches: $pattern"
	local key
	key=$(cache_key "tests:$package:$pattern")
	run_cached "$key" go test "$package" -run "$pattern" -count=1
}

need_build() {
	run_cached build go build ./...
}

need_suite() {
	run_cached suite go test ./...
}
