package versionutil

import (
	"errors"
	"fmt"
	"runtime"
	"strings"

	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/go-kit/kit/log"
)

// These fields are populated by govvv at compile-time in main and must be set in here to be shared across all packages.
var (
	Version   string
	GitCommit string
	BuildDate string
	GitState  string
)

func Initialize(version string, gitCommit string, buildDate string, gitState string) {
	Version = version
	GitCommit = gitCommit
	BuildDate = buildDate
	GitState = gitState
}

// VersionString builds a compact version string in format:
// vVERSION/git@GitCommit[-State].
func VersionString() string {
	return fmt.Sprintf("v%s/git@%s-%s", Version, GitCommit, GitState)

}

// DetailedVersionString returns a detailed version string including version
// number, git commit, build date, source tree state and the go runtime version.
func DetailedVersionString() string {
	// e.g. v2.2.0 git:03669cef-clean build:2016-07-22T16:22:26.556103000+00:00 go:go1.6.2
	return fmt.Sprintf("v%s git:%s-%s build:%s %s", Version, GitCommit, GitState, BuildDate, runtime.Version())
}

// Extracts the installed version of run command from the service definition.
// Current extracting it from the ExecStart field that includes the version as a substring.
func ExtractFromServiceDefinition(content string, ctx *log.Context) (string, error) {
	ctx.Log("message", "extracting version from service definition "+content)
	firstSplit := strings.Split(string(content), fmt.Sprintf("ExecStart=%s/%s-", constants.WaAgentDirectory, constants.RunCommandExtensionName))
	if len(firstSplit) < 2 {
		return "", errors.New("wrong service definition found. Missing field " + fmt.Sprintf("ExecStart=%s/%s-", constants.WaAgentDirectory, constants.RunCommandExtensionName))
	}

	secondSplit := strings.Split(firstSplit[1], "/bin/immediate-run-command-handler")
	installedVersion := secondSplit[0]
	ctx.Log("message", "current installed version: "+installedVersion)
	return installedVersion, nil
}
