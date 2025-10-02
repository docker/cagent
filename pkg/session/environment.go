package session

import (
	"fmt"
	"runtime"
)

// getEnvironmentInfo returns formatted environment information including
// working directory, git repository status, and platform information
func getEnvironmentInfo(workingDir string) string {
	return fmt.Sprintf(`Here is useful information about the environment you are running in:
	<env>
	Working directory: %s
	Is directory a git repo: %s
	Platform: %s
	</env>`, workingDir, boolToYesNo(isGitRepo(workingDir)), runtime.GOOS)
}

// boolToYesNo converts a boolean to "Yes" or "No" string
func boolToYesNo(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}
