package main

import (
	"os"

	"github.com/Azure/run-command-handler-linux/internal/immediateruncommand"
	"github.com/Azure/run-command-handler-linux/pkg/versionutil"
	"github.com/go-kit/kit/log"
)

// These fields are populated by govvv at compile-time.
var (
	Version   string
	GitCommit string
	BuildDate string
	GitState  string
)

// Entry Point for ImmediateRunCommand
func main() {
	// After starting the program, vars from versionutil.go must be set in order to share those values across the program.
	versionutil.Initialize(Version, GitCommit, BuildDate, GitState)

	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(
		os.Stdout))).With("time", log.DefaultTimestamp).With("version", versionutil.VersionString())
	ctx = ctx.With("operation", "runService")
	immediateruncommand.StartImmediateRunCommand(ctx)
}
