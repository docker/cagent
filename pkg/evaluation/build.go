package evaluation

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

var (
	//go:embed Dockerfile.template
	dockerfileTmpl string

	//go:embed Dockerfile.custom.template
	dockerfileCustomTmpl string

	dockerfileTemplate       = template.Must(template.New("Dockerfile").Parse(dockerfileTmpl))
	dockerfileCustomTemplate = template.Must(template.New("DockerfileCustom").Parse(dockerfileCustomTmpl))
)

// getOrBuildImage returns a cached image ID or builds a new one.
// Images are cached by working directory to avoid redundant builds.
func (r *Runner) getOrBuildImage(ctx context.Context, workingDir string) (string, error) {
	r.imageCacheMu.Lock()
	if imageID, ok := r.imageCache[workingDir]; ok {
		r.imageCacheMu.Unlock()
		return imageID, nil
	}
	r.imageCacheMu.Unlock()

	imageID, err := r.buildEvalImage(ctx, workingDir)
	if err != nil {
		return "", err
	}

	r.imageCacheMu.Lock()
	r.imageCache[workingDir] = imageID
	r.imageCacheMu.Unlock()

	return imageID, nil
}

func (r *Runner) buildEvalImage(ctx context.Context, workingDir string) (string, error) {
	var buildContext string
	var data struct {
		CopyWorkingDir bool
		BaseImage      string
	}

	if workingDir == "" {
		buildContext = r.EvalsDir
		data.CopyWorkingDir = false
	} else {
		buildContext = filepath.Join(r.EvalsDir, "working_dirs", workingDir)
		if _, err := os.Stat(buildContext); os.IsNotExist(err) {
			return "", fmt.Errorf("working directory not found: %s", buildContext)
		}
		data.CopyWorkingDir = true
	}

	// Choose template based on whether a custom base image is provided
	tmpl := dockerfileTemplate
	if r.BaseImage != "" {
		tmpl = dockerfileCustomTemplate
		data.BaseImage = r.BaseImage
	}

	var dockerfile bytes.Buffer
	if err := tmpl.Execute(&dockerfile, data); err != nil {
		return "", fmt.Errorf("executing dockerfile template: %w", err)
	}

	cmd := exec.CommandContext(ctx, "docker", "build", "-q", "-f-", ".")
	cmd.Dir = buildContext
	cmd.Stdin = &dockerfile

	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("docker build failed: %s", string(exitErr.Stderr))
		}
		return "", fmt.Errorf("docker build failed: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}
