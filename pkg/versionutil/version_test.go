package versionutil

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

var (
	systemdUnitConfigurationTemplateTest = `[Unit]
	Description=Managed RunCommand Service
	
	[Service]
	User=root
	Restart=always
	RestartSec=5
	WorkingDirectory=/var/lib/waagent/Microsoft.CPlat.Core.RunCommandHandlerLinux-%run_command_version_placeholder%
	ExecStart=/var/lib/waagent/Microsoft.CPlat.Core.RunCommandHandlerLinux-%run_command_version_placeholder%/bin/immediate-run-command-handler
	StandardOutput=append:/var/log/azure/run-command-handler/ImmediateRunCommandService.log
	StandardError=append:/var/log/azure/run-command-handler/ImmediateRunCommandService.log
	
	[Install]
	WantedBy=multi-user.target`
)

func TestVersionString(t *testing.T) {
	defer resetStrings()

	Initialize("1.0.0", "03669cef", "DATE", "dirty")
	require.Equal(t, "v1.0.0/git@03669cef-dirty", VersionString())
}

func TestDetailedVersionString(t *testing.T) {
	defer resetStrings()

	goVersion := runtime.Version()
	Initialize("1.0.0", "03669cef", "DATE", "dirty")
	require.Equal(t, "v1.0.0 git:03669cef-dirty build:DATE "+goVersion, DetailedVersionString())
}

func TestSuccessfulVersionExtraction(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	versionsToTest := []string{"0.0.0", "1.0.0", "1.3.5", "1.3.7", "2.2.0", "1000.1000.1000"}
	for _, installedVersion := range versionsToTest {
		extractedVersion, err := ExtractFromServiceDefinition(getServiceDefinitionWithVersion(installedVersion), ctx)
		require.Nil(t, err, "provided service definition should be valid")
		require.Equal(t, installedVersion, extractedVersion)
	}
}

func TestFailToExtractVersion(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	versionsToTest := []string{"", "invalidDefinition"}
	for _, content := range versionsToTest {
		extractedVersion, err := ExtractFromServiceDefinition(content, ctx)
		require.NotNil(t, err, "extracting the version from invalid service definition should return an error")
		require.Equal(t, "", extractedVersion)
	}
}

func getServiceDefinitionWithVersion(version string) string {
	definition := strings.ReplaceAll(systemdUnitConfigurationTemplateTest, "%run_command_version_placeholder%", version)
	return definition
}

func resetStrings() { Version, GitCommit, BuildDate, GitState = "", "", "", "" }
