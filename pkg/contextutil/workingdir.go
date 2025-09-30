package contextutil

import "context"

// Context key for session working directory
type contextKey string

const workingDirKey contextKey = "session_working_dir"

// WithWorkingDir adds the working directory to the context
func WithWorkingDir(ctx context.Context, workingDir string) context.Context {
	return context.WithValue(ctx, workingDirKey, workingDir)
}

// GetWorkingDir retrieves the working directory from the context
// Returns an empty string if not set
func GetWorkingDir(ctx context.Context) string {
	if wd, ok := ctx.Value(workingDirKey).(string); ok {
		return wd
	}
	return ""
}
