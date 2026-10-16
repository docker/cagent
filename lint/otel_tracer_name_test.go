package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/dgageot/rubocop-go/config"
	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/coptest"
	"github.com/dgageot/rubocop-go/runner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOTelTracerNameFlagsCagent(t *testing.T) {
	t.Parallel()
	src := `package p
import "go.opentelemetry.io/otel"
func f() { _ = otel.Tracer("cagent") }
`
	offenses := coptest.RunTyped(t, OTelTracerName, src)
	require.Len(t, offenses, 1)
	assert.Equal(t, "Lint/OTelTracerName", offenses[0].CopName)
	assert.Contains(t, offenses[0].Message, `otel.Tracer(AppName)`)
}

func TestOTelTracerNameFlagsLocalAppName(t *testing.T) {
	t.Parallel()
	// A local const named AppName from an unrelated package must not bypass the cop.
	src := `package p
import "go.opentelemetry.io/otel"
const AppName = "docker-agent"
func f() { _ = otel.Tracer(AppName) }
`
	offenses := coptest.RunTyped(t, OTelTracerName, src)
	require.Len(t, offenses, 1)
	assert.Contains(t, offenses[0].Message, `got "docker-agent"`)
}

func TestOTelTracerNameAllowsVersionAppName(t *testing.T) {
	t.Parallel()
	src := `package p
import (
	"go.opentelemetry.io/otel"
	"github.com/docker/docker-agent/pkg/version"
)
func f() { _ = otel.Tracer(version.AppName) }
`
	assert.Empty(t, coptest.RunTyped(t, OTelTracerName, src))
}

func TestPackageImportPath(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ path, want string }{
		"module":          {modulePath, modulePath},
		"import":          {cmdRootPkgPath, cmdRootPkgPath},
		"relative":        {"cmd/root", cmdRootPkgPath},
		"dot relative":    {"./cmd/root", cmdRootPkgPath},
		"native":          {filepath.Join("cmd", "root"), cmdRootPkgPath},
		"native relative": {"." + string(filepath.Separator) + filepath.Join("cmd", "root"), cmdRootPkgPath},
		"version":         {filepath.Join("pkg", "version"), versionPkgPath},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, packageImportPath(tc.path))
		})
	}
}

func TestOTelTracerNameRunnerAppName(t *testing.T) {
	t.Chdir(t.TempDir())
	files := map[string]string{
		"go.mod":                 "module github.com/docker/docker-agent\n\ngo 1.27\nrequire go.opentelemetry.io/otel v0.0.0\nreplace go.opentelemetry.io/otel => ./otel\n",
		"otel/go.mod":            "module go.opentelemetry.io/otel\n\ngo 1.27\n",
		"otel/otel.go":           "package otel\nfunc Tracer(string) {}\n",
		"pkg/version/version.go": "package version\nconst AppName = \"docker-agent\"\n",
		"cmd/root/name.go":       "package root\nimport \"github.com/docker/docker-agent/pkg/version\"\nconst AppName = version.AppName\n",
		"cmd/root/use.go":        "package root\nimport \"go.opentelemetry.io/otel\"\nfunc use() { otel.Tracer(AppName) }\n",
		"other/use.go":           "package other\nimport \"go.opentelemetry.io/otel\"\nconst AppName = \"docker-agent\"\nfunc use() { otel.Tracer(AppName) }\n",
	}
	for name, src := range files {
		require.NoError(t, os.MkdirAll(filepath.Dir(name), 0o700))
		require.NoError(t, os.WriteFile(name, []byte(src), 0o600))
	}
	var output bytes.Buffer
	r := runner.New([]cop.Cop{OTelTracerName}, config.DefaultConfig(), &output)
	count, err := r.Run([]string{"cmd/root", "other"})
	require.NoError(t, err)
	assert.Equal(t, 1, count, output.String())
	assert.Contains(t, output.String(), filepath.Join("other", "use.go"))
	assert.NotContains(t, output.String(), filepath.Join("cmd", "root", "use.go")+":")
}

func TestOTelTracerNameFlagsWrongName(t *testing.T) {
	t.Parallel()
	src := `package p
import "go.opentelemetry.io/otel"
func f() { _ = otel.Tracer("wrong") }
`
	offenses := coptest.RunTyped(t, OTelTracerName, src)
	require.Len(t, offenses, 1)
	assert.Contains(t, offenses[0].Message, `got "wrong"`)
}
