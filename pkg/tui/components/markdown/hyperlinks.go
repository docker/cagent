package markdown

import (
	"fmt"
	"os"
	"runtime"
)

func supportsHyperlinks() bool {
	if version, found := os.LookupEnv("VTE_VERSION"); found {
		major, _ := parseVersion(version)
		return major > 5000
	}

	if term, found := os.LookupEnv("TERM_PROGRAM"); found {
		switch term {
		case "ghostty", "Tabby", "rio":
			return true
		case "Hyper":
			// Renders correctly but not clickable
			return false
		}

		major, minor := parseVersion(os.Getenv("TERM_PROGRAM_VERSION"))
		switch term {
		case "iTerm.app":
			return major > 3 || (major == 3 && minor >= 1)
		case "WezTerm":
			return major >= 20200620
		case "vscode":
			return major > 1 || (major == 1 && minor >= 72)
		}
	}

	if emulator, found := os.LookupEnv("TERMINAL_EMULATOR"); found {
		if emulator == "JetBrains-JediTerm" {
			return true
		}
	}

	if _, found := os.LookupEnv("ALACRITTY_WINDOW_ID"); found {
		return true
	}
	if _, found := os.LookupEnv("BYOBU_TERM"); found {
		return true
	}

	return runtime.GOOS == "windows"
}

func parseVersion(version string) (int, int) {
	var major, minor, patch int
	_, _ = fmt.Sscanf(version, "%d.%d.%d", &major, &minor, &patch)
	return major, minor
}
