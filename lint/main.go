// Package main runs shared and project-specific linting cops using rubocop-go.
//
// Usage: go run ./lint [path...]
package main

import (
	"fmt"
	"os"

	"github.com/dgageot/rubocop-go/config"
	"github.com/dgageot/rubocop-go/cop"
	rubocops "github.com/dgageot/rubocop-go/cops"
	"github.com/dgageot/rubocop-go/prog"
	"github.com/dgageot/rubocop-go/runner"
)

// Select shared cops explicitly so dependency upgrades cannot enable new rules.
// Project-specific cops remain declared in this package.
var cops = []cop.Cop{
	ConfigVersionImport,
	ConfigPackageName,
	ConfigVersionConstant,
	LatestImportsPredecessor,
	ConfigLatestTagConsistency,
	ConfigVersionsRegistered,
	TUIViewPurity,
	TUIKeyBindings,
	RuntimeEventRegistry,
	RuntimeSessionScoped,
	HookConfigSync,
	HookBuiltinsRegistered,
	HookBuiltinsDocumented,
	rubocops.NewLintHTTPRequestWithContext(cop.WithScope(outsideFrozenConfig)),
	rubocops.NewLintNoFatalOutsideMain(),
	rubocops.NewLintSlogContextual(),
	ToolArgumentsViaAIJSON,
	rubocops.NewLintConstructorPurity(),
	withDirectoryTypes(sharedFileCop(rubocops.NewLintConstructorCommandExec)),
	rubocops.NewLintConstructorNetworkIO(),
	rubocops.NewLintWrapErrors(),
	rubocops.NewLintErrorStringMatching(cop.WithScope(outsideFrozenConfig)),
	rubocops.NewLintDeferMutexUnlock(),
	rubocops.NewLintNewExpr(cop.WithScope(outsideFrozenConfig)),
	EnvironmentVariablePrefix,
	rubocops.NewLintNoStdoutInLibraries(cop.WithScope(cop.UnderDir("pkg"))),
	OTelTracerName,
	AtomicStateWrite,
	HTTPClientTransport,
	ToolsetSchemaRegistrySync,
	StatePathViaPathsPackage,
}

// programCops lists whole-program, inter-procedural cops. These run once over
// the entire loaded program rather than once per file.
var programCops = newProgramCops()

func newProgramCops() []prog.Cop {
	loader := &typedPackageLoader{}
	return []prog.Cop{
		withTypedFiles(loader.load, rubocops.NewLintErrorsAsType, true, cop.WithScope(outsideFrozenConfig)),
		withTypedFiles(loader.load, rubocops.NewLintMapsCopy, true, cop.WithScope(outsideFrozenConfig)),
		withTypedFiles(loader.load, rubocops.NewLintMapsClone, true, cop.WithScope(outsideFrozenConfig)),
		withTypedFiles(loader.load, rubocops.NewLintSlicesContains, true, cop.WithScope(outsideFrozenConfig)),
		withTypedFiles(loader.load, rubocops.NewLintSlicesEqual, true, cop.WithScope(outsideFrozenConfig)),
		withTypedFiles(loader.load, rubocops.NewLintSplitSeq, true, cop.WithScope(outsideFrozenConfig)),
		withTypedFiles(loader.load, rubocops.NewLintSortedMapKeys, true, cop.WithScope(outsideFrozenConfig)),
		withTypedFiles(loader.load, rubocops.NewLintWaitGroupGo, true, cop.WithScope(outsideFrozenConfig)),
		withTypedFiles(loader.load, rubocops.NewLintHTTPTestRequestWithContext, true, cop.WithScope(outsideFrozenConfig)),
		withTypedFiles(loader.load, rubocops.NewLintSlicesClone, false, cop.WithScope(outsideFrozenConfig)),
		rubocops.NewLintSortStableFunc(cop.WithScope(outsideFrozenConfig)),
		rubocops.NewLintPointerHelper(cop.WithScope(outsideFrozenConfig)),
		rubocops.NewLintReflectFields(cop.WithScope(outsideFrozenConfig)),
		rubocops.NewLintStdlibUUID(cop.WithScope(outsideFrozenConfig)),
		rubocops.NewLintURLClone(cop.WithScope(outsideFrozenConfig)),
		rubocops.NewLintJSONMarshalWrite(cop.WithScope(outsideFrozenConfig)),
		rubocops.NewLintBenchmarkLoop(cop.WithScope(outsideFrozenConfig)),
		rubocops.NewLintSplitTrimJoin(),
		rubocops.NewLintFieldsSeq(),
		rubocops.NewLintCutPrefix(cop.WithScope(outsideFrozenConfig)),
		rubocops.NewLintCutSuffix(cop.WithScope(outsideFrozenConfig)),
		rubocops.NewLintFieldsSeqLookup(),
		SlicesConcat,
		newChannelDirection(loader.load),
		CancellationLoop,
		ChannelStateCheck,
		ClosedChannelLoop,
		SessionStateAccessors,
		CancellationSafeResultSend,
		NonblockingChannelReplacement,
		BoundedCommandPipeLifetime,
		SilentLimitedRead,
		rubocops.NewLintStreamCloseSafety(),
		ExclusiveStreamLease,
		DrainRunStreamBeforeRelease,
		rubocops.NewLintContextConnectivity(),
	}
}

func main() {
	paths := os.Args[1:]
	if len(paths) == 0 {
		paths = []string{"."}
	}

	r := runner.New(cops, config.DefaultConfig(), os.Stdout).
		WithProgramCops(programCops)
	offenseCount, err := r.Run(paths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if offenseCount > 0 {
		os.Exit(1)
	}
}
