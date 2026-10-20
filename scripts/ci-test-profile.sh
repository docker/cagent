#!/usr/bin/env bash
set -euo pipefail

: "${RUNNER_TEMP:?Run this diagnostic on a hosted CI runner}"
export PROFILE_DIR="$RUNNER_TEMP/ci-test-profile"
mkdir -p "$PROFILE_DIR"

# Match Taskfile test environments without passing live provider credentials.
set -a
# shellcheck disable=SC1091
source .env.test
set +a

time_phase() {
  local phase="$1" start end status=0
  shift
  {
    printf '## Phase: %s\n\n' "$phase"
    printf '| Phase | Wall time | Exit status |\n| --- | ---: | ---: |\n'
  } >> "$GITHUB_STEP_SUMMARY"
  start=$(date +%s)
  if "$@"; then
    status=0
  else
    status=$?
  fi
  end=$(date +%s)
  printf '%s\t%s\t%s\n' "$phase" "$((end - start))" "$status" >> "$PROFILE_DIR/phases.tsv"
  printf '| %s | %ss | %s |\n' "$phase" "$((end - start))" "$status" >> "$GITHUB_STEP_SUMMARY"
  return "$status"
}

execute_tests() {
  go test -count=1 -json ./... | "$PROFILE_DIR/gotestreport" -out "$PROFILE_DIR/$PROFILE_PASS.json"
}

case "${1:?Specify a profiling phase}" in
  environment)
    {
      printf 'Commit: %s\nCache scenario: %s\nRunner: %s/%s\n' "$GITHUB_SHA" "$PROFILE_CACHE" "$RUNNER_OS" "$RUNNER_ARCH"
      printf 'Image: %s %s\n' "${ImageOS:-unknown}" "${ImageVersion:-unknown}"
      printf 'Logical CPUs: '
      nproc
      go version
      go env -json GOOS GOARCH GOFLAGS GOTOOLCHAIN GOROOT GOPATH GOCACHE GOMODCACHE CGO_ENABLED
      printf 'GOMAXPROCS override: %s\n' "${GOMAXPROCS:-unset (Go default)}"
      printf 'HOME=%s\nUSERPROFILE=%s\nTEMP=%s\nTMP=%s\n' "${HOME:-}" "${USERPROFILE:-}" "${TEMP:-}" "${TMP:-}"
      df -h . "$RUNNER_TEMP"
      if [[ "$RUNNER_OS" == Linux ]]; then
        cat /proc/meminfo
        lscpu
        for path in /sys/fs/cgroup/cpu.max /sys/fs/cgroup/memory.max; do
          if [[ -f "$path" ]]; then printf '%s: ' "$path"; cat "$path"; fi
        done
      else
        pwsh -NoProfile -Command 'Get-CimInstance Win32_ComputerSystem | Select-Object NumberOfLogicalProcessors,TotalPhysicalMemory; Get-CimInstance Win32_Processor | Select-Object Name,NumberOfCores,NumberOfLogicalProcessors'
      fi
    } | tee "$PROFILE_DIR/environment.txt"
    {
      printf '## Hosted test profile: %s / %s\n\n' "$RUNNER_OS" "$PROFILE_CACHE"
      printf 'Both test passes use -count=1 after a compile-only prewarm; go test still includes linking, vet, and test setup. Module download is measured separately.\n\n'
    } >> "$GITHUB_STEP_SUMMARY"
    ;;
  download)
    time_phase download go mod download
    ;;
  build)
    time_phase build bash scripts/build.sh
    ;;
  compile)
    output=/dev/null
    if [[ "$RUNNER_OS" == Windows ]]; then output=NUL; fi
    time_phase compile go test -c -o "$output" ./...
    go build -o "$PROFILE_DIR/gotestreport" ./scripts/gotestreport
    ;;
  execute-1|execute-2)
    export PROFILE_PASS="$1"
    time_phase "$1" execute_tests
    ;;
  *)
    printf 'Unknown phase: %s\n' "$1" >&2
    exit 2
    ;;
esac
