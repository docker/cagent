#!/bin/bash
# MacBook Pro Professional Tools Launcher (Improved)
# Run VS Code/Cursor extension installers with validation and enhanced UX.
set -e
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# Color & formatting
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
GREEN='\033[32m'
RED='\033[31m'
YELLOW='\033[33m'
BLUE='\033[34m'
BOLD='\033[1m'
RESET='\033[0m'
info() { echo -e "${BLUE}ℹ${RESET}  $*"; }
success() { echo -e "${GREEN}✅${RESET} $*"; }
warn() { echo -e "${YELLOW}⚠️${RESET}  $*"; }
error() { echo -e "${RED}❌${RESET} $*"; }
header() { echo -e "\n${BOLD}${BLUE}$*${RESET}\n"; }
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# Global settings
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
DRY_RUN=false
VERBOSE=false
BUNDLES_ARRAY=(essential docker python web mobile)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"
# Resolve CLI: prefer `code`, fall back to `cursor`
CODE_CMD="$(command -v code 2>/dev/null || command -v cursor 2>/dev/null || echo "")"
if [[ -z "$CODE_CMD" ]]; then
  error "'code' or 'cursor' not found in PATH"
  info "Install VS Code or Cursor and run: Shell Command: Install 'code' command in PATH"
  exit 1
fi
export CODE_CMD
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# Preflight checks
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
check_editor_version() {
  local version
  version=$($CODE_CMD --version 2>/dev/null | head -1) || version="unknown"
  info "Using $($CODE_CMD --version 2>/dev/null | grep -oE 'Code|Cursor') $version"
}
validate_bundles() {
  local missing=()

  for bundle in "${BUNDLES_ARRAY[@]}"; do
    if [[ ! -f "install-${bundle}.sh" ]]; then
      missing+=("$bundle")
    fi
  done

  if [[ ${#missing[@]} -gt 0 ]]; then
    error "Missing bundle scripts: ${missing[*]}"
    error "Expected files: install-{${BUNDLES_ARRAY[*]}}.sh"
    return 1
  fi

  success "All bundle scripts found"
}
preflight() {
  header "Preflight Checks"
  check_editor_version
  validate_bundles
}
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# Run logic
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
run() {
  local name="$1"
  local script="$2"

  if [[ ! -f "$script" ]]; then
    error "Not found: $script (aborting)"
    return 1
  fi

  info "Running $name..."

  if [[ "$DRY_RUN" == "true" ]]; then
    warn "[DRY RUN] Would execute: $script"
    return 0
  fi

  if bash "$script"; then
    success "$name completed"
  else
    error "$name failed (exit code: $?)"
    return 1
  fi
}
run_all() {
  local failed=0

  header "Running All Bundles"

  for bundle in "${BUNDLES_ARRAY[@]}"; do
    run "$(echo "$bundle" | sed 's/^./\U&/')" "install-${bundle}.sh" || ((failed++))
  done

  echo ""
  if [[ $failed -eq 0 ]]; then
    success "All bundles completed successfully"
  else
    error "$failed bundle(s) failed"
    return 1
  fi
}
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# Menu & CLI
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
show_help() {
  cat <<EOF
${BOLD}MacBook Pro Professional Tools${RESET}
Usage: $0 [OPTIONS] [BUNDLE...]
${BOLD}Bundles:${RESET}
  essential   Prettier, ESLint, GitLens, Tailwind, Error Lens, themes
  docker      Docker, Kubernetes, Terraform, Remote SSH/Containers
  python      Python, Pylance, Jupyter, data science, testing
  web         React, Vue, Angular, Svelte, HTML/CSS, REST, Live Server
  mobile      React Native, Flutter, Swift, Android
${BOLD}Options:${RESET}
  --dry-run   Show what would run without executing
  --verbose   Print detailed output
  -h, --help  Show this help message
${BOLD}Examples:${RESET}
  $0                # Interactive menu
  $0 all            # Install all bundles
  $0 essential web  # Install essential and web bundles
  $0 --dry-run all  # Preview all bundles without running
EOF
}
menu() {
  header "MacBook Pro Professional Tools"

  echo "  1) essential   — Prettier, ESLint, GitLens, Tailwind, Error Lens, themes"
  echo "  2) docker      — Docker, Kubernetes, Terraform, Remote SSH/Containers"
  echo "  3) python      — Python, Pylance, Jupyter, data science, testing"
  echo "  4) web         — React, Vue, Angular, Svelte, HTML/CSS, REST, Live Server"
  echo "  5) mobile      — React Native, Flutter, Swift, Android"
  echo "  6) all         — Run all of the above"
  echo "  q) quit        — Exit"
  echo ""

  local choice
  read -r -p "Choice (1-6, q to quit): " choice
  choice="${choice,,}"  # convert to lowercase

  case "$choice" in
    1) run "Essential" "install-essential.sh" ;;
    2) run "Docker" "install-docker.sh" ;;
    3) run "Python" "install-python.sh" ;;
    4) run "Web" "install-web-dev.sh" ;;
    5) run "Mobile" "install-mobile.sh" ;;
    6) run_all ;;
    q) info "Goodbye."; exit 0 ;;
    *) error "Invalid choice: $choice"; menu ;;
  esac
}
parse_args() {
  local bundles_to_run=()

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --dry-run) DRY_RUN=true; shift ;;
      --verbose) VERBOSE=true; shift ;;
      -h|--help) show_help; exit 0 ;;
      all) bundles_to_run=("${BUNDLES_ARRAY[@]}"); shift ;;
      essential|docker|python|web|mobile)
        bundles_to_run+=("$1")
        shift
        ;;
      *)
        error "Unknown option or bundle: $1"
        show_help
        exit 1
        ;;
    esac
  done

  if [[ ${#bundles_to_run[@]} -gt 0 ]]; then
    preflight

    [[ "$DRY_RUN" == "true" ]] && header "DRY RUN MODE"

    for bundle in "${bundles_to_run[@]}"; do
      run "$(echo "$bundle" | sed 's/^./\U&/')" "install-${bundle}.sh" || true
    done

    echo ""
    success "Done"
  else
    preflight
    menu
  fi
}
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# Main
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
if [[ $# -eq 0 ]]; then
  preflight
  menu
else
  parse_args "$@"
fi
