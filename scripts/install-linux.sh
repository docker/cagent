#!/usr/bin/env bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Script info
REPO="docker/cagent"
BINARY_NAME="cagent"
INSTALL_DIR="/usr/local/bin"

# Print colored message
print_msg() {
    local color=$1
    shift
    echo -e "${color}$@${NC}" >&2
}

# Detect architecture
detect_arch() {
    local arch
    arch=$(uname -m)

    case "$arch" in
        x86_64|amd64)
            echo "amd64"
            ;;
        aarch64|arm64)
            echo "arm64"
            ;;
        *)
            print_msg "$RED" "Error: Unsupported architecture: $arch"
            print_msg "$YELLOW" "Supported architectures: x86_64 (amd64), aarch64 (arm64)"
            exit 1
            ;;
    esac
}

# Detect OS
detect_os() {
    local os
    os=$(uname -s)

    case "$os" in
        Linux)
            echo "linux"
            ;;
        Darwin)
            print_msg "$RED" "Error: This script is for Linux only."
            echo "" >&2
            print_msg "$YELLOW" "For macOS, you have two options:"
            print_msg "$YELLOW" "  1. Download Docker Desktop: https://www.docker.com/products/docker-desktop"
            print_msg "$YELLOW" "     (cagent is included starting with version 4.49.0)"
            print_msg "$YELLOW" "  2. Use Homebrew: brew install cagent"
            exit 1
            ;;
        MINGW*|MSYS*|CYGWIN*)
            print_msg "$RED" "Error: This script is for Linux only."
            echo "" >&2
            print_msg "$YELLOW" "For Windows, download Docker Desktop: https://www.docker.com/products/docker-desktop"
            print_msg "$YELLOW" "(cagent is included starting with version 4.49.0)"
            exit 1
            ;;
        *)
            print_msg "$RED" "Error: Unsupported operating system: $os"
            print_msg "$YELLOW" "This script is for Linux only."
            print_msg "$YELLOW" "For other platforms, visit: https://github.com/$REPO"
            exit 1
            ;;
    esac
}

# Get latest release version
get_latest_version() {
    print_msg "$BLUE" "Fetching latest version..."

    # Try using gh CLI first if available
    if command -v gh &> /dev/null; then
        gh release list --repo "$REPO" --limit 1 --json tagName --jq '.[0].tagName' 2>/dev/null
    else
        # Fall back to curl and parse JSON
        curl -sSfL "https://api.github.com/repos/$REPO/releases/latest" |
            grep '"tag_name"' |
            sed -E 's/.*"tag_name": *"([^"]+)".*/\1/'
    fi
}

# Download and install
install_cagent() {
    local os arch version

    os=$(detect_os)
    arch=$(detect_arch)
    version=$(get_latest_version)

    if [ -z "$version" ]; then
        print_msg "$RED" "Error: Could not determine latest version"
        exit 1
    fi

    print_msg "$GREEN" "Installing cagent $version for $os/$arch..."

    local binary_name="${BINARY_NAME}-${os}-${arch}"
    local download_url="https://github.com/$REPO/releases/download/$version/$binary_name"
    local tmp_file="/tmp/$BINARY_NAME-$$"

    # Download binary
    print_msg "$BLUE" "Downloading from $download_url..."
    if ! curl -fsSL "$download_url" -o "$tmp_file"; then
        print_msg "$RED" "Error: Failed to download binary"
        exit 1
    fi

    # Make executable
    chmod +x "$tmp_file"

    # Determine install directory
    local target_dir="$INSTALL_DIR"
    local use_sudo=true

    # Check if we can write to /usr/local/bin
    if [ ! -w "$INSTALL_DIR" ]; then
        if ! command -v sudo &> /dev/null; then
            # No sudo available, use user's local bin
            target_dir="$HOME/.local/bin"
            use_sudo=false
            mkdir -p "$target_dir"
            print_msg "$YELLOW" "Installing to $target_dir (sudo not available)"
        else
            print_msg "$YELLOW" "Installing to $target_dir (requires sudo)"
        fi
    else
        use_sudo=false
    fi

    # Install binary
    local target_path="$target_dir/$BINARY_NAME"
    if [ "$use_sudo" = true ]; then
        sudo mv "$tmp_file" "$target_path"
        sudo chmod +x "$target_path"
    else
        mv "$tmp_file" "$target_path"
    fi

    print_msg "$GREEN" "✓ Successfully installed cagent to $target_path"

    # Check if directory is in PATH
    if [[ ":$PATH:" != *":$target_dir:"* ]]; then
        print_msg "$YELLOW" "⚠ Warning: $target_dir is not in your PATH"
        print_msg "$YELLOW" "Add it to your PATH by adding this to your ~/.bashrc or ~/.zshrc:"
        print_msg "$YELLOW" "  export PATH=\"$target_dir:\$PATH\""
    fi

    # Verify installation
    print_msg "$BLUE" "Verifying installation..."
    if command -v "$BINARY_NAME" &> /dev/null; then
        local installed_version
        installed_version=$("$BINARY_NAME" --version 2>&1 | head -n1)
        print_msg "$GREEN" "✓ $installed_version"
    else
        print_msg "$YELLOW" "⚠ Installation complete, but 'cagent' not found in PATH"
        print_msg "$YELLOW" "You may need to restart your shell or add $target_dir to PATH"
    fi

    # Print next steps
    echo "" >&2
    print_msg "$GREEN" "╔════════════════════════════════════════════════════════════╗"
    print_msg "$GREEN" "║            cagent installation complete!                   ║"
    print_msg "$GREEN" "╚════════════════════════════════════════════════════════════╝"
    echo ""
    print_msg "$BLUE" "Next steps:"
    print_msg "$BLUE" "  1. Set your API keys (at least one required):"
    print_msg "$BLUE" "     export OPENAI_API_KEY=your_key      # For OpenAI models"
    print_msg "$BLUE" "     export ANTHROPIC_API_KEY=your_key   # For Anthropic models"
    print_msg "$BLUE" "     export GOOGLE_API_KEY=your_key      # For Gemini models"
    echo "" >&2
    print_msg "$BLUE" "  2. Try it out:"
    print_msg "$BLUE" "     cagent run default \"What can you do?\""
    echo "" >&2
    print_msg "$BLUE" "  3. Learn more:"
    print_msg "$BLUE" "     https://github.com/$REPO"
    echo "" >&2
}

# Main
main() {
    print_msg "$GREEN" "╔════════════════════════════════════════════════════════════╗"
    print_msg "$GREEN" "║          Docker cagent Installation Script                 ║"
    print_msg "$GREEN" "╚════════════════════════════════════════════════════════════╝"
    echo "" >&2

    install_cagent
}

main
