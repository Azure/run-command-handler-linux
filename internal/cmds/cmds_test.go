package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/Azure/azure-extension-platform/pkg/extensionevents"
	"github.com/Azure/azure-extension-platform/pkg/handlerenv"
	"github.com/Azure/azure-extension-platform/pkg/logging"
	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/files"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/Azure/run-command-handler-linux/internal/settings"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/ahmetb/go-httpbin"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

func Test_CopyMrseqFiles_MrseqFilesAreCopied(t *testing.T) {
	currentExtensionVersionDirectory := "Microsoft.CPlat.Core.RunCommandHandlerLinux-1.3.8"
	os.Setenv(constants.ExtensionPathEnvName, currentExtensionVersionDirectory)
	os.Setenv(constants.ExtensionVersionUpdatingFromEnvName, "1.3.7")
	os.Setenv(constants.ExtensionVersionEnvName, "1.3.8")

	currentVersion := os.Getenv(constants.ExtensionVersionEnvName)
	previousVersion := os.Getenv(constants.ExtensionVersionUpdatingFromEnvName)
	previousExtensionVersionDirectory := strings.ReplaceAll(currentExtensionVersionDirectory, currentVersion, previousVersion)

	// Remove currentExtensionVersionDirectory if it exists
	_, err := os.Stat(currentExtensionVersionDirectory)
	if err == nil {
		os.RemoveAll(currentExtensionVersionDirectory)
	}

	// Remove previousExtensionVersionDirectory if it exists
	_, err = os.Stat(previousExtensionVersionDirectory)
	if err == nil {
		os.RemoveAll(previousExtensionVersionDirectory)
	}

	// Create previousExtensionVersionDirectory
	err = os.Mkdir(previousExtensionVersionDirectory, 0777)
	require.Nil(t, err)

	// Create currentExtensionVersionDirectory
	err = os.Mkdir(currentExtensionVersionDirectory, 0777)
	require.Nil(t, err)

	files, _ := ioutil.ReadDir(currentExtensionVersionDirectory)
	require.Equal(t, 0, len(files))

	createMrseqFile(filepath.Join(previousExtensionVersionDirectory, "1.mrseq"), "0", t)
	createMrseqFile(filepath.Join(previousExtensionVersionDirectory, "ABCD.mrseq"), "1", t)
	createMrseqFile(filepath.Join(previousExtensionVersionDirectory, "2345.mrseq"), "0", t)
	createMrseqFile(filepath.Join(previousExtensionVersionDirectory, "RC0804_0.mrseq"), "5", t)
	createMrseqFile(filepath.Join(previousExtensionVersionDirectory, "asdfsad.mrseq"), "20", t)
	os.Create(filepath.Join(previousExtensionVersionDirectory, "abc.txt")) // this should not be copied to currentExtensionVersionDirectory

	statusSubdirectory := "status"
	previousStatusDirectory := filepath.Join(previousExtensionVersionDirectory, statusSubdirectory)
	// Create previousStatusDirectory
	err = os.Mkdir(previousStatusDirectory, 0777)
	require.Nil(t, err)

	// Only two status files are available. The rest of the 3 status files should be created during Update operation which would have dummy status.
	// Dummy status files would be created to prevent poll status timeouts for already executed Run Commands after upgrade.
	os.Create(filepath.Join(previousStatusDirectory, "1.0.status"))
	os.Create(filepath.Join(previousStatusDirectory, "ABCD.1.status"))
	os.Create(filepath.Join(previousStatusDirectory, "abc.cs")) // this should not be copied to currentExtensionVersionDirectory

	tempDir, _ := os.MkdirTemp("", "deletecmd")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	extensionLogger := logging.New(nil)
	extensionEventManager := extensionevents.New(extensionLogger, &handlerEnvironment)
	err = CopyStateForUpdate(log.NewContext(log.NewNopLogger()), previousExtensionVersionDirectory, currentExtensionVersionDirectory, extensionEventManager)
	require.Nil(t, err)

	files, _ = ioutil.ReadDir(currentExtensionVersionDirectory)
	require.Equal(t, 6, len(files))
	statusDirectoryCount := 0
	mrseqFileCount := 0
	for _, file := range files {
		if file.IsDir() {
			require.Equal(t, "status", file.Name())
			statusDirectoryCount++
		} else {
			require.True(t, strings.HasSuffix(file.Name(), ".mrseq"))
			mrseqFileCount++
		}
	}
	require.Equal(t, 1, statusDirectoryCount)
	require.Equal(t, 5, mrseqFileCount)

	currentStatusDirectory := filepath.Join(currentExtensionVersionDirectory, statusSubdirectory)
	files, _ = ioutil.ReadDir(currentStatusDirectory)
	require.Equal(t, 5, len(files))
	for _, file := range files {
		require.True(t, strings.HasSuffix(file.Name(), ".status"))
	}

	// Clean up
	os.RemoveAll(currentExtensionVersionDirectory)
	os.RemoveAll(previousExtensionVersionDirectory)
}

func createMrseqFile(mrseqFilePath string, mrseqNum string, t *testing.T) {
	os.Create(mrseqFilePath)
	err := os.WriteFile(mrseqFilePath, []byte(mrseqNum), 0644)
	require.Nil(t, err)
}

func Test_commandsExist(t *testing.T) {
	// we expect these subcommands to be handled
	expect := []string{"install", "enable", "disable", "uninstall", "update"}
	for _, c := range expect {
		_, ok := Cmds[c]
		if !ok {
			t.Fatalf("cmd '%s' is not handled", c)
		}
	}
}

func Test_commands_shouldReportStatus(t *testing.T) {
	// - certain extension invocations are supposed to write 'N.status' files and some do not.

	// these subcommands should NOT report status
	require.False(t, Cmds["install"].ShouldReportStatus, "install should not report status")
	require.False(t, Cmds["uninstall"].ShouldReportStatus, "uninstall should not report status")

	// these subcommands SHOULD report status
	require.True(t, Cmds["enable"].ShouldReportStatus, "enable should report status")
	require.False(t, Cmds["disable"].ShouldReportStatus, "disable should report status")
	require.False(t, Cmds["update"].ShouldReportStatus, "update should report status")
}

func Test_checkAndSaveSeqNum_fails(t *testing.T) {
	// pass in invalid seqnum format
	_, err := checkAndSaveSeqNum(log.NewNopLogger(), 0, "/non/existing/dir")
	require.NotNil(t, err)
	require.Contains(t, err.Error(), `failed to save sequence number`)
}

func Test_checkAndSaveSeqNum(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	fp := filepath.Join(dir, "seqnum")
	defer os.RemoveAll(dir)

	nop := log.NewNopLogger()

	// no sequence number, 0 comes in.
	shouldExit, err := checkAndSaveSeqNum(nop, 0, fp)
	require.Nil(t, err)
	require.False(t, shouldExit)

	// file=0, seq=0 comes in. (should exit)
	shouldExit, err = checkAndSaveSeqNum(nop, 0, fp)
	require.Nil(t, err)
	require.True(t, shouldExit)

	// file=0, seq=1 comes in.
	shouldExit, err = checkAndSaveSeqNum(nop, 1, fp)
	require.Nil(t, err)
	require.False(t, shouldExit)

	// file=1, seq=1 comes in. (should exit)
	shouldExit, err = checkAndSaveSeqNum(nop, 1, fp)
	require.Nil(t, err)
	require.True(t, shouldExit)

	// file=1, seq=0 comes in. (should exit)
	shouldExit, err = checkAndSaveSeqNum(nop, 1, fp)
	require.Nil(t, err)
	require.True(t, shouldExit)
}

func Test_update_e2e_cmd(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "deletecmd")
	defer os.RemoveAll(tempDir)

	DataDir, _ = os.MkdirTemp("", "datadir")
	defer os.RemoveAll(DataDir)

	oldVersionDirectory := filepath.Join(tempDir, "Microsoft.CPlat.Core.RunCommandHandlerLinux-1.3.8")
	newVersionDirectory := filepath.Join(tempDir, "Microsoft.CPlat.Core.RunCommandHandlerLinux-1.3.9")
	err := os.Mkdir(oldVersionDirectory, 0755)
	require.Nil(t, err, "Could not create old version subdirectory")
	err = os.Mkdir(newVersionDirectory, 0755)
	require.Nil(t, err, "Could not create new version subdirectory")
	oldStatusPath := create_folder(t, oldVersionDirectory, constants.StatusFileDirectory)
	newStatusPath := create_folder(t, newVersionDirectory, constants.StatusFileDirectory)
	oldEventsPath := create_folder(t, oldVersionDirectory, constants.ExtensionEventsDirectory)
	newEventsPath := create_folder(t, newVersionDirectory, constants.ExtensionEventsDirectory)

	fakeEnv := types.HandlerEnvironment{}
	update_handler_env(&fakeEnv, oldStatusPath, oldVersionDirectory, oldEventsPath)

	// We start on the old version
	os.Setenv(constants.ExtensionPathEnvName, oldVersionDirectory)
	os.Setenv(constants.ExtensionVersionEnvName, "1.3.8")

	// Create two extensions
	enable_extension(t, fakeEnv, oldVersionDirectory, "happyChipmunk", true, 0)
	enable_extension(t, fakeEnv, oldVersionDirectory, "crazyChipmunk", true, 0)

	// Now, pretend that the extension was updated
	// Step 1: WALA calls Disable on our two extensions
	disable_extension(t, fakeEnv, oldVersionDirectory, "happyChipmunk")
	disable_extension(t, fakeEnv, oldVersionDirectory, "crazyChipmunk")

	// Step 2: WALA will call update
	os.Setenv(constants.ExtensionVersionEnvName, "1.3.9")
	os.Setenv(constants.ExtensionPathEnvName, newVersionDirectory)
	os.Setenv(constants.ExtensionVersionUpdatingFromEnvName, "1.3.8")
	update_handler_env(&fakeEnv, newStatusPath, newVersionDirectory, newEventsPath)
	update_handler(t, fakeEnv, tempDir)

	// Now, WALA will uninstall the old extension
	uninstall_handler(t, fakeEnv, tempDir)

	// Then, WALA will install the new extension
	install_handler(t, fakeEnv, tempDir)

	// Now call enable and verify we did NOT re-execute the script
	enable_extension(t, fakeEnv, newVersionDirectory, "happyChipmunk", false, 0)
	enable_extension(t, fakeEnv, newVersionDirectory, "crazyChipmunk", false, 0)
}

func Test_update_e23_non_problematic_version(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "deletecmd")
	defer os.RemoveAll(tempDir)

	DataDir, _ = os.MkdirTemp("", "datadir")
	defer os.RemoveAll(DataDir)

	oldVersionDirectory := filepath.Join(tempDir, "Microsoft.CPlat.Core.RunCommandHandlerLinux-1.3.26")
	newVersionDirectory := filepath.Join(tempDir, "Microsoft.CPlat.Core.RunCommandHandlerLinux-1.3.27")
	err := os.Mkdir(oldVersionDirectory, 0755)
	require.Nil(t, err, "Could not create old version subdirectory")
	err = os.Mkdir(newVersionDirectory, 0755)
	require.Nil(t, err, "Could not create new version subdirectory")
	oldStatusPath := create_folder(t, oldVersionDirectory, constants.StatusFileDirectory)
	newStatusPath := create_folder(t, newVersionDirectory, constants.StatusFileDirectory)
	oldEventsPath := create_folder(t, oldVersionDirectory, constants.ExtensionEventsDirectory)
	newEventsPath := create_folder(t, newVersionDirectory, constants.ExtensionEventsDirectory)

	fakeEnv := types.HandlerEnvironment{}
	update_handler_env(&fakeEnv, oldStatusPath, oldVersionDirectory, oldEventsPath)

	// We start on the old version
	os.Setenv(constants.ExtensionPathEnvName, oldVersionDirectory)
	os.Setenv(constants.ExtensionVersionEnvName, "1.3.26")

	// Create three extensions
	enable_extension(t, fakeEnv, oldVersionDirectory, "happyChipmunk", true, 0)
	enable_extension(t, fakeEnv, oldVersionDirectory, "crazyChipmunk", true, 0)
	enable_extension(t, fakeEnv, oldVersionDirectory, "stubbornChipmunk", true, 0)

	// Run one of them again to obtain multiple status files
	enable_extension(t, fakeEnv, oldVersionDirectory, "happyChipmunk", true, 1)

	// Now, pretend that the extension was updated
	// Step 1: WALA calls Disable on our two extensions
	disable_extension(t, fakeEnv, oldVersionDirectory, "happyChipmunk")
	disable_extension(t, fakeEnv, oldVersionDirectory, "crazyChipmunk")
	disable_extension(t, fakeEnv, oldVersionDirectory, "stubbornChipmunk")

	// Step 2: WALA will call update
	os.Setenv(constants.ExtensionVersionEnvName, "1.3.27")
	os.Setenv(constants.ExtensionPathEnvName, newVersionDirectory)
	os.Setenv(constants.ExtensionVersionUpdatingFromEnvName, "1.3.26")
	update_handler_env(&fakeEnv, newStatusPath, newVersionDirectory, newEventsPath)
	update_handler(t, fakeEnv, tempDir)

	// Now, WALA will uninstall the old extension
	uninstall_handler(t, fakeEnv, tempDir)

	// Then, WALA will install the new extension
	install_handler(t, fakeEnv, tempDir)

	// Now call enable and verify we did NOT re-execute the script
	enable_extension(t, fakeEnv, newVersionDirectory, "happyChipmunk", false, 1)
	enable_extension(t, fakeEnv, newVersionDirectory, "crazyChipmunk", false, 0)
	enable_extension(t, fakeEnv, newVersionDirectory, "stubbornChipmunk", false, 0)

	// Run them again with a higher seqNo to ensure they're now executed
	enable_extension(t, fakeEnv, newVersionDirectory, "happyChipmunk", true, 2)
	enable_extension(t, fakeEnv, newVersionDirectory, "crazyChipmunk", true, 1)
	enable_extension(t, fakeEnv, newVersionDirectory, "stubbornChipmunk", true, 1)
}

func Test_udpate_e2e_problematic_version(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "deletecmd")
	defer os.RemoveAll(tempDir)

	DataDir, _ = os.MkdirTemp("", "datadir")
	defer os.RemoveAll(DataDir)

	oldVersionDirectory := filepath.Join(tempDir, "Microsoft.CPlat.Core.RunCommandHandlerLinux-1.3.17")
	newVersionDirectory := filepath.Join(tempDir, "Microsoft.CPlat.Core.RunCommandHandlerLinux-1.3.18")
	err := os.Mkdir(oldVersionDirectory, 0755)
	require.Nil(t, err, "Could not create old version subdirectory")
	err = os.Mkdir(newVersionDirectory, 0755)
	require.Nil(t, err, "Could not create new version subdirectory")
	oldStatusPath := create_folder(t, oldVersionDirectory, constants.StatusFileDirectory)
	newStatusPath := create_folder(t, newVersionDirectory, constants.StatusFileDirectory)
	oldEventsPath := create_folder(t, oldVersionDirectory, constants.ExtensionEventsDirectory)
	newEventsPath := create_folder(t, newVersionDirectory, constants.ExtensionEventsDirectory)

	fakeEnv := types.HandlerEnvironment{}
	update_handler_env(&fakeEnv, oldStatusPath, oldVersionDirectory, oldEventsPath)

	// We start on the old version
	os.Setenv(constants.ExtensionPathEnvName, oldVersionDirectory)
	os.Setenv(constants.ExtensionVersionEnvName, "1.3.17")

	// Create three extensions
	enable_extension(t, fakeEnv, oldVersionDirectory, "happyChipmunk", true, 0)
	enable_extension(t, fakeEnv, oldVersionDirectory, "crazyChipmunk", true, 0)
	enable_extension(t, fakeEnv, oldVersionDirectory, "stubbornChipmunk", true, 0)

	// Run one of them again to obtain multiple status files
	enable_extension(t, fakeEnv, oldVersionDirectory, "happyChipmunk", true, 1)

	// Now, pretend that the extension was updated
	// Step 1: WALA calls Disable on our two extensions
	disable_extension(t, fakeEnv, oldVersionDirectory, "happyChipmunk")
	disable_extension(t, fakeEnv, oldVersionDirectory, "crazyChipmunk")
	disable_extension(t, fakeEnv, oldVersionDirectory, "stubbornChipmunk")

	// To simulate the bug existing in 1.3.17, delete the mrseq files
	// Don't disable the stubborn chipmunk to test a .mrseq file that for some reason wasn't deleted
	err = os.Remove(filepath.Join(oldVersionDirectory, "happyChipmunk"+constants.MrSeqFileExtension))
	require.Nil(t, err, "Could not delete happyChipmunk.mrseq")
	err = os.Remove(filepath.Join(oldVersionDirectory, "crazyChipmunk"+constants.MrSeqFileExtension))
	require.Nil(t, err, "Could not delete crazyChipmunk.mrseq")

	// Add a malformatted .status file just to mess with the logic
	os.WriteFile(filepath.Join(oldStatusPath, "this.is.a.bad.chipmunk.0.status"), []byte("0"), os.FileMode(0600))

	// Step 2: WALA will call update
	os.Setenv(constants.ExtensionVersionEnvName, "1.3.18")
	os.Setenv(constants.ExtensionPathEnvName, newVersionDirectory)
	os.Setenv(constants.ExtensionVersionUpdatingFromEnvName, "1.3.17")
	update_handler_env(&fakeEnv, newStatusPath, newVersionDirectory, newEventsPath)
	update_handler(t, fakeEnv, tempDir)

	// Now, WALA will uninstall the old extension
	uninstall_handler(t, fakeEnv, tempDir)

	// Then, WALA will install the new extension
	install_handler(t, fakeEnv, tempDir)

	// Now call enable and verify we did NOT re-execute the script
	enable_extension(t, fakeEnv, newVersionDirectory, "happyChipmunk", false, 1)
	enable_extension(t, fakeEnv, newVersionDirectory, "crazyChipmunk", false, 0)
	enable_extension(t, fakeEnv, newVersionDirectory, "stubbornChipmunk", false, 0)

	// Run them again with a higher seqNo to ensure they're now executed
	enable_extension(t, fakeEnv, newVersionDirectory, "happyChipmunk", true, 2)
	enable_extension(t, fakeEnv, newVersionDirectory, "crazyChipmunk", true, 1)
	enable_extension(t, fakeEnv, newVersionDirectory, "stubbornChipmunk", true, 1)
}

func create_folder(t *testing.T, versionDirectory string, folderName string) string {
	folderPath := filepath.Join(versionDirectory, folderName)
	err := os.Mkdir(folderPath, 0755)
	require.Nil(t, err, "Could not create folder "+folderName)
	return folderPath
}

func update_handler_env(fakeEnv *types.HandlerEnvironment, statusFolder string, configFolder string, eventsFolder string) {
	fakeEnv.HandlerEnvironment.StatusFolder = statusFolder
	fakeEnv.HandlerEnvironment.ConfigFolder = configFolder
	fakeEnv.HandlerEnvironment.EventsFolder = eventsFolder
}

func install_handler(t *testing.T, fakeEnv types.HandlerEnvironment, tempDir string) {
	generic_handler_call(t, fakeEnv, tempDir, "install", types.CmdInstallTemplate, CmdInstall)
}

func uninstall_handler(t *testing.T, fakeEnv types.HandlerEnvironment, tempDir string) {
	generic_handler_call(t, fakeEnv, tempDir, "uninstall", types.CmdUninstallTemplate, CmdUninstall)
}

func update_handler(t *testing.T, fakeEnv types.HandlerEnvironment, tempDir string) {
	generic_handler_call(t, fakeEnv, tempDir, "update", types.CmdUpdateTemplate, CmdUpdate)
}

func generic_handler_call(t *testing.T, fakeEnv types.HandlerEnvironment, tempDir string, cmdName string, cmdTemplate types.Cmd, cmd types.Cmd) {
	fakeInstanceView := types.RunCommandInstanceView{}

	metadata := types.NewRCMetadata("", 0, constants.DownloadFolder, tempDir)
	_, _, err, exitCode := cmd.Functions.Invoke(log.NewContext(log.NewNopLogger()), fakeEnv, &fakeInstanceView, metadata, cmdTemplate)
	require.Nil(t, err, cmdName+" command should run successfully")
	require.Equal(t, constants.ExitCode_Okay, exitCode)
}

func enable_extension(t *testing.T, fakeEnv types.HandlerEnvironment, tempDir string, extName string, shouldExecuteScript bool, seqNo int) {
	fakeInstanceView := types.RunCommandInstanceView{}
	wasCalled := false

	settingsCommon := settings.SettingsCommon{
		ExtensionName:           &extName,
		ProtectedSettingsBase64: "",
		SettingsCertThumbprint:  "SomeProtectedSettingsInBase64",
		PublicSettings: map[string]interface{}{
			"source": map[string]interface{}{
				"script": "echo Hello World!",
			},
			"runAsUser": "",
		},
	}

	handlerSettings := handlersettings.HandlerSettingsFile{
		RuntimeSettings: []handlersettings.RunTimeSettingsFile{
			{
				HandlerSettings: settingsCommon,
			},
		},
	}

	settingsFilePath := filepath.Join(tempDir, extName+"."+strconv.Itoa(seqNo)+".settings")
	file, err := os.Create(settingsFilePath)
	require.Nil(t, err, "Could not create settings file")
	encoder := json.NewEncoder(file)
	err = encoder.Encode(handlerSettings)
	require.Nil(t, err, "Could not serialze settings file")

	RunCmd = func(ctx *log.Context, dir, scriptFilePath string, cfg *handlersettings.HandlerSettings, metadata types.RCMetadata) (error, int) {
		wasCalled = true
		return nil, 0 // mock behavior
	}

	metadata := types.NewRCMetadata(extName, seqNo, constants.DownloadFolder, tempDir)
	metadata.MostRecentSequence = filepath.Join(tempDir, extName+constants.MrSeqFileExtension)
	metadata.SeqNum = seqNo

	// Call the EnablePre function (Enable is the only function that has one)
	ctx := log.NewContext(log.NewNopLogger())
	err = CmdEnable.Functions.Pre(ctx, fakeEnv, metadata, types.CmdEnableTemplate)

	// If we're not expecting the script to run, then EnablePre will return an error
	if shouldExecuteScript {
		require.Nil(t, err, "EnablePre failed")

		// Call the Enable function
		_, _, err, exitCode := CmdEnable.Functions.Invoke(ctx, fakeEnv, &fakeInstanceView, metadata, types.CmdEnableTemplate)
		require.Nil(t, err, "command should run successfully")
		require.Equal(t, constants.ExitCode_Okay, exitCode)

		// Verify whether we should have run the script
		require.Equal(t, shouldExecuteScript, wasCalled)
	} else {
		require.Equal(t, ErrAlreadyProcessed, err)
	}

	// Report status to create a status file
	err = CmdEnable.Functions.ReportStatus(ctx, fakeEnv, metadata, types.StatusSuccess, types.CmdEnableTemplate, "The chipmunk is satisfied")
	require.Nil(t, err, "ReportStatus failed")

	// Verify we have a .mrseq
	if _, err := os.Stat(metadata.MostRecentSequence); errors.Is(err, os.ErrNotExist) {
		require.Fail(t, extName+".mrseq should exist but does not")
	}
}

func disable_extension(t *testing.T, fakeEnv types.HandlerEnvironment, tempDir string, extName string) {
	fakeInstanceView := types.RunCommandInstanceView{}

	metadata := types.NewRCMetadata(extName, 0, constants.DownloadFolder, tempDir)
	metadata.MostRecentSequence = filepath.Join(tempDir, extName+constants.MrSeqFileExtension)
	_, _, err, _ := CmdDisable.Functions.Invoke(log.NewContext(log.NewNopLogger()), fakeEnv, &fakeInstanceView, metadata, types.CmdDisableTemplate)
	require.Nil(t, err)

	// The .mrseq should still be here
	if _, err := os.Stat(metadata.MostRecentSequence); errors.Is(err, os.ErrNotExist) {
		require.Fail(t, extName+".mrseq should exist but does not")
	}
}

func Test_runCmd_success(t *testing.T) {
	var script = "date"
	dir, err := os.MkdirTemp("", "")
	require.Nil(t, err)
	defer os.RemoveAll(dir)

	// Ensure that the script succeeds
	ExecCmdInDir = func(ctx *log.Context, scriptFilePath, workdir string, cfg *handlersettings.HandlerSettings) (error, int) {
		return nil, 0
	}
	metadata := types.NewRCMetadata("extName", 0, constants.DownloadFolder, DataDir)
	err, exitCode := runCmd(log.NewContext(log.NewNopLogger()), dir, "", &handlersettings.HandlerSettings{
		PublicSettings: handlersettings.PublicSettings{Source: &handlersettings.ScriptSource{Script: script}},
	}, metadata)
	require.Nil(t, err, "command should run successfully")
	require.Equal(t, constants.ExitCode_Okay, exitCode)

	// Check embedded script if saved to file
	_, err = os.Stat(filepath.Join(dir, "script.sh"))
	require.Nil(t, err, "script.sh should exist")
	content, err := ioutil.ReadFile(filepath.Join(dir, "script.sh"))
	require.Nil(t, err, "script.sh read failure")
	require.Equal(t, script, string(content))
}

func Test_runCmd_fail(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	defer os.RemoveAll(dir)

	// Ensure that the script fails
	ExecCmdInDir = func(ctx *log.Context, scriptFilePath, workdir string, cfg *handlersettings.HandlerSettings) (error, int) {
		return errors.New("the chipmunks have risen in revolt"), 42
	}

	metadata := types.NewRCMetadata("extName", 0, constants.DownloadFolder, DataDir)
	err, exitCode := runCmd(log.NewContext(log.NewNopLogger()), dir, "", &handlersettings.HandlerSettings{
		PublicSettings: handlersettings.PublicSettings{Source: &handlersettings.ScriptSource{Script: "non-existing-cmd"}},
	}, metadata)
	require.NotNil(t, err, "command terminated with exit status")
	require.Contains(t, err.Error(), "failed to execute command")
	require.NotEqual(t, constants.ExitCode_Okay, exitCode)
}

func Test_downloadScriptUri(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	defer os.RemoveAll(dir)

	srv := httptest.NewServer(httpbin.GetMux())
	defer srv.Close()

	downloadedFilePath, err := downloadScript(log.NewContext(log.NewNopLogger()),
		dir,
		&handlersettings.HandlerSettings{
			PublicSettings: handlersettings.PublicSettings{
				Source: &handlersettings.ScriptSource{ScriptURI: srv.URL + "/bytes/10"},
			},
		})
	require.Nil(t, err)

	// check the downloaded file
	fp := filepath.Join(dir, "10")
	require.Equal(t, fp, downloadedFilePath)
	_, err = os.Stat(fp)
	require.Nil(t, err, "%s is missing from download dir", fp)
}

func Test_downloadArtifacts_Invalid(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	defer os.RemoveAll(dir)

	srv := httptest.NewServer(httpbin.GetMux())
	defer srv.Close()

	// The count of public vs protected settings differs
	err = downloadArtifacts(log.NewContext(log.NewNopLogger()),
		dir,
		&handlersettings.HandlerSettings{
			PublicSettings: handlersettings.PublicSettings{
				Source: &handlersettings.ScriptSource{ScriptURI: srv.URL + "/bytes/10"},
				Artifacts: []handlersettings.PublicArtifactSource{
					{
						ArtifactId:  1,
						ArtifactUri: srv.URL + "/status/404",
						FileName:    "flipper",
					},
				},
			},
			ProtectedSettings: handlersettings.ProtectedSettings{
				Artifacts: []handlersettings.ProtectedArtifactSource{},
			},
		})

	require.NotNil(t, err)
	require.Contains(t, err.Error(), "RunCommand artifact download failed. Reason: Invalid artifact specification. This is a product bug.")

	// ArtifactIds don't match
	err = downloadArtifacts(log.NewContext(log.NewNopLogger()),
		dir,
		&handlersettings.HandlerSettings{
			PublicSettings: handlersettings.PublicSettings{
				Source: &handlersettings.ScriptSource{ScriptURI: srv.URL + "/bytes/10"},
				Artifacts: []handlersettings.PublicArtifactSource{
					{
						ArtifactId:  1,
						ArtifactUri: srv.URL + "/status/404",
						FileName:    "flipper",
					},
				},
			},
			ProtectedSettings: handlersettings.ProtectedSettings{
				Artifacts: []handlersettings.ProtectedArtifactSource{
					{
						ArtifactId: 2,
					},
				},
			},
		})

	require.NotNil(t, err)
	require.Contains(t, err.Error(), "RunCommand artifact download failed. Reason: Invalid artifact specification. This is a product bug.")
}

func Test_downloadArtifactsFail(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	defer os.RemoveAll(dir)

	srv := httptest.NewServer(httpbin.GetMux())
	defer srv.Close()

	err = downloadArtifacts(log.NewContext(log.NewNopLogger()),
		dir,
		&handlersettings.HandlerSettings{
			PublicSettings: handlersettings.PublicSettings{
				Source: &handlersettings.ScriptSource{ScriptURI: srv.URL + "/bytes/10"},
				Artifacts: []handlersettings.PublicArtifactSource{
					{
						ArtifactId:  1,
						ArtifactUri: srv.URL + "/status/404",
						FileName:    "flipper",
					},
				},
			},
			ProtectedSettings: handlersettings.ProtectedSettings{
				Artifacts: []handlersettings.ProtectedArtifactSource{
					{
						ArtifactId: 1,
					},
				},
			},
		})

	require.NotNil(t, err)
	require.Contains(t, err.Error(), "failed to download artifact")
}

func Test_downloadArtifacts(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	defer os.RemoveAll(dir)

	srv := httptest.NewServer(httpbin.GetMux())
	defer srv.Close()

	err = downloadArtifacts(log.NewContext(log.NewNopLogger()),
		dir,
		&handlersettings.HandlerSettings{
			PublicSettings: handlersettings.PublicSettings{
				Source: &handlersettings.ScriptSource{ScriptURI: srv.URL + "/bytes/10"},
				Artifacts: []handlersettings.PublicArtifactSource{
					{
						ArtifactId:  1,
						ArtifactUri: srv.URL + "/bytes/255",
						FileName:    "flipper",
					},
					{
						ArtifactId:  2,
						ArtifactUri: srv.URL + "/bytes/256",
					},
				},
			},
			ProtectedSettings: handlersettings.ProtectedSettings{
				Artifacts: []handlersettings.ProtectedArtifactSource{
					{
						ArtifactId: 1,
					},
					{
						ArtifactId: 2,
					},
				},
			},
		})
	require.Nil(t, err)

	// check the downloaded files
	fp := filepath.Join(dir, "flipper")
	_, err = os.Stat(fp)
	require.Nil(t, err, "%s is missing from download dir", fp)

	fp = filepath.Join(dir, "Artifact2")
	_, err = os.Stat(fp)
	require.Nil(t, err, "%s is missing from download dir", fp)
}

func Test_decodeScript(t *testing.T) {
	testSubject := "bHMK"
	s, info, err := decodeScript(testSubject)

	require.NoError(t, err)
	require.Equal(t, info, "4;3;gzip=0")
	require.Equal(t, s, "ls\n")
}

func Test_decodeScriptGzip(t *testing.T) {
	testSubject := "H4sIACD731kAA8sp5gIAfShLWgMAAAA="
	s, info, err := decodeScript(testSubject)

	require.NoError(t, err)
	require.Equal(t, info, "32;3;gzip=1")
	require.Equal(t, s, "ls\n")
}

func Test_downloadScriptUri_BySASFailsSucceedsByManagedIdentity(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	defer os.RemoveAll(dir)

	files.UseMockSASDownloadFailure = true
	handler := func(writer http.ResponseWriter, request *http.Request) {
		if strings.Contains(request.RequestURI, "/samplecontainer/sample.sh?SASToken") {
			writer.WriteHeader(http.StatusOK) // Download successful using managed identity
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	_, err = downloadScript(log.NewContext(log.NewNopLogger()),
		dir,
		&handlersettings.HandlerSettings{
			PublicSettings: handlersettings.PublicSettings{
				Source: &handlersettings.ScriptSource{ScriptURI: srv.URL + "/samplecontainer/sample.sh?SASToken"},
			},
			ProtectedSettings: handlersettings.ProtectedSettings{
				SourceSASToken: "SASToken",
				SourceManagedIdentity: &handlersettings.RunCommandManagedIdentity{
					ClientId: "00b64c6a-6dbf-41e0-8707-74132d5cf53f",
				},
			},
		})
	require.Nil(t, err)
	files.UseMockSASDownloadFailure = false
}

// This test just makes sure using TreatFailureAsDeploymentFailure flag, script is executed as expected.
// The interpretation of the result (Succeeded or Failed, when TreatFailureAsDeploymentFailure is true)
//
//	is done in main.go
func Test_TreatFailureAsDeploymentFailureIsTrue_Fails(t *testing.T) {
	var script = "ech HelloWorld" // ech is an unknown command. Sh returns error and 127 status code
	dir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	defer os.RemoveAll(dir)

	// Ensure that the script fails
	ExecCmdInDir = func(ctx *log.Context, scriptFilePath, workdir string, cfg *handlersettings.HandlerSettings) (error, int) {
		return errors.New("the chipmunks do not like the script"), 127
	}

	metadata := types.NewRCMetadata("extName", 0, constants.DownloadFolder, DataDir)
	err, exitCode := runCmd(log.NewContext(log.NewNopLogger()), dir, "", &handlersettings.HandlerSettings{
		PublicSettings: handlersettings.PublicSettings{Source: &handlersettings.ScriptSource{Script: script}, TreatFailureAsDeploymentFailure: true},
	}, metadata)
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "failed to execute command: the chipmunks do not like the script")
	require.NotEqual(t, constants.ExitCode_Okay, exitCode)
}

// This test just makes sure using TreatFailureAsDeploymentFailure flag, script is executed as expected.
// The interpretation of the result (Succeeded or Failed, when TreatFailureAsDeploymentFailure is true)
//
//	is done in main.go
func Test_TreatFailureAsDeploymentFailureIsTrue_SimpleScriptSucceeds(t *testing.T) {
	var script = "echo HelloWorld" // ech is an unknown command. Sh returns error and 127 status code
	dir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	defer os.RemoveAll(dir)

	// Ensure that the script succeeds
	ExecCmdInDir = func(ctx *log.Context, scriptFilePath, workdir string, cfg *handlersettings.HandlerSettings) (error, int) {
		return nil, 0
	}

	metadata := types.NewRCMetadata("extName", 0, constants.DownloadFolder, DataDir)
	err, exitCode := runCmd(log.NewContext(log.NewNopLogger()), dir, "", &handlersettings.HandlerSettings{
		PublicSettings: handlersettings.PublicSettings{Source: &handlersettings.ScriptSource{Script: script}, TreatFailureAsDeploymentFailure: false},
	}, metadata)
	require.Nil(t, err)
	require.Equal(t, constants.ExitCode_Okay, exitCode)
}

func TestPadTo(t *testing.T) {
	tests := []struct {
		name     string
		in       []int
		size     int
		expected []int
	}{
		{
			name:     "Input longer than size",
			in:       []int{1, 2, 3, 4},
			size:     2,
			expected: []int{1, 2},
		},
		{
			name:     "Input equal to size",
			in:       []int{1, 2, 3},
			size:     3,
			expected: []int{1, 2, 3},
		},
		{
			name:     "Input shorter than size",
			in:       []int{1, 2},
			size:     5,
			expected: []int{1, 2, 0, 0, 0},
		},
		{
			name:     "Empty input",
			in:       []int{},
			size:     3,
			expected: []int{0, 0, 0},
		},
		{
			name:     "Size zero",
			in:       []int{1, 2, 3},
			size:     0,
			expected: []int{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := padTo(tt.in, tt.size)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("padTo(%v, %d) = %v; expected %v", tt.in, tt.size, result, tt.expected)
			}
		})
	}
}

func TestSplitVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		in       string
		expected []int
	}{
		{
			name:     "simple-3-parts",
			in:       "1.2.3",
			expected: []int{1, 2, 3},
		},
		{
			name:     "single-part",
			in:       "42",
			expected: []int{42},
		},
		{
			name:     "leading-zeros",
			in:       "001.0002.00003",
			expected: []int{1, 2, 3},
		},
		{
			name:     "spaces-around-parts",
			in:       "  1 .  2  .  3  ",
			expected: []int{1, 2, 3},
		},
		{
			name:     "non-numeric-alpha",
			in:       "1.a.3",
			expected: []int{1, 0, 3},
		},
		{
			name:     "non-numeric-mixed",
			in:       "1.2beta.3",
			expected: []int{1, 0, 3},
		},
		{
			name: "empty-string",
			in:   "",
			// strings.Split("", ".") == []string{""} → p=="" → append 0
			expected: []int{0},
		},
		{
			name: "consecutive-dots-empty-components",
			in:   "1..3....5",
			// empty parts become 0
			expected: []int{1, 0, 3, 0, 0, 0, 5},
		},
		{
			name:     "trailing-dot",
			in:       "1.2.",
			expected: []int{1, 2, 0},
		},
		{
			name:     "leading-dot",
			in:       ".2.3",
			expected: []int{0, 2, 3},
		},
		{
			name: "very-large-number",
			in:   "2147483647.0",
			// Note: Go int is platform-dependent; still valid parsing.
			expected: []int{2147483647, 0},
		},
		{
			name:     "zeros-only",
			in:       "0.0.0",
			expected: []int{0, 0, 0},
		},
		{
			name: "whitespace-only-component",
			in:   "1.   .3",
			// TrimSpace makes middle part "", thus 0
			expected: []int{1, 0, 3},
		},
		{
			name:     "unicode-digits-are-not-ASCII-digits",
			in:       "１.2", // Note: first char is full-width '１' (U+FF11) → non-ASCII → 0
			expected: []int{0, 2},
		},
		{
			name: "dash-negative-like",
			in:   "1.-2.3",
			// '-' makes component non-numeric → 0
			expected: []int{1, 0, 3},
		},
		{
			name:     "plus-sign",
			in:       "+1.2",
			expected: []int{0, 2},
		},
		{
			name:     "long-many-parts",
			in:       "1.2.3.4.5.6.7.8.9",
			expected: []int{1, 2, 3, 4, 5, 6, 7, 8, 9},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := splitVersion(tt.in)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Fatalf("splitVersion(%q) = %v, want %v", tt.in, got, tt.expected)
			}
		})
	}
}

func TestCompareVersions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		a        string
		b        string
		expected int
	}{
		{
			name:     "equal-simple",
			a:        "1.2.3",
			b:        "1.2.3",
			expected: 0,
		},
		{
			name:     "equal-with-extra-zeros",
			a:        "1.2.3.0",
			b:        "1.2.3",
			expected: 0,
		},
		{
			name:     "a-greater-last-segment",
			a:        "1.2.3.4",
			b:        "1.2.3.3",
			expected: 1,
		},
		{
			name:     "b-greater-last-segment",
			a:        "1.2.3.3",
			b:        "1.2.3.4",
			expected: -1,
		},
		{
			name:     "a-greater-first-segment",
			a:        "2.0.0",
			b:        "1.9.9",
			expected: 1,
		},
		{
			name:     "b-greater-first-segment",
			a:        "1.9.9",
			b:        "2.0.0",
			expected: -1,
		},
		{
			name:     "normalize-length-a-shorter",
			a:        "1.2",
			b:        "1.2.0.1",
			expected: -1,
		},
		{
			name:     "normalize-length-b-shorter",
			a:        "1.2.0.1",
			b:        "1.2",
			expected: 1,
		},
		{
			name:     "leading-zeros-equal",
			a:        "01.002.0003",
			b:        "1.2.3",
			expected: 0,
		},
		{
			name:     "non-numeric-in-a",
			a:        "1.alpha.3",
			b:        "1.0.3",
			expected: 0, // alpha → 0
		},
		{
			name:     "non-numeric-in-b",
			a:        "1.2.3",
			b:        "1.beta.3",
			expected: 1, // beta → 0, so a > b
		},
		{
			name:     "empty-strings",
			a:        "",
			b:        "",
			expected: 0,
		},
		{
			name:     "empty-vs-non-empty",
			a:        "",
			b:        "0.0.0.1",
			expected: -1,
		},
		{
			name:     "longer-than-4-segments-ignored-after-4",
			a:        "1.2.3.4.999",
			b:        "1.2.3.4.0",
			expected: 0, // only first 4 segments matter
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := compareVersions(tt.a, tt.b)
			if got != tt.expected {
				t.Fatalf("compareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.expected)
			}
		})
	}
}

func TestHasMrseq(t *testing.T) {
	ctx := log.NewContext(log.NewNopLogger())

	t.Run("empty dir string returns false", func(t *testing.T) {
		if got := hasMrseq(ctx, ""); got {
			t.Fatalf("hasMrseq(ctx, \"\") = true; want false")
		}
	})

	t.Run("non-existent directory returns false", func(t *testing.T) {
		nonExistent := filepath.Join(t.TempDir(), "this-dir-does-not-exist")
		// Ensure it truly doesn't exist
		if _, err := os.Stat(nonExistent); !os.IsNotExist(err) {
			t.Fatalf("test setup: expected directory to not exist: %s", nonExistent)
		}
		if got := hasMrseq(ctx, nonExistent); got {
			t.Fatalf("hasMrseq(ctx, %q) = true; want false", nonExistent)
		}
	})

	t.Run("empty directory returns false", func(t *testing.T) {
		dir := t.TempDir()
		if got := hasMrseq(ctx, dir); got {
			t.Fatalf("hasMrseq(ctx, %q) = true; want false", dir)
		}
	})

	t.Run("directory with one .mrseq file returns true", func(t *testing.T) {
		dir := t.TempDir()
		f := filepath.Join(dir, "run1.mrseq")
		if err := os.WriteFile(f, []byte("dummy"), 0o644); err != nil {
			t.Fatalf("test setup: write %s: %v", f, err)
		}
		if got := hasMrseq(ctx, dir); !got {
			t.Fatalf("hasMrseq(ctx, %q) = false; want true", dir)
		}
	})

	t.Run("directory with multiple .mrseq files returns true", func(t *testing.T) {
		dir := t.TempDir()
		for i := 1; i <= 3; i++ {
			name := filepath.Join(dir, fmt.Sprintf("batch_%d.mrseq", i))
			if err := os.WriteFile(name, []byte("dummy"), 0o644); err != nil {
				t.Fatalf("test setup: write %s: %v", name, err)
			}
		}
		if got := hasMrseq(ctx, dir); !got {
			t.Fatalf("hasMrseq(ctx, %q) = false; want true", dir)
		}
	})

	t.Run("directory with non-mrseq files only returns false", func(t *testing.T) {
		dir := t.TempDir()
		others := []string{"a.txt", "b.mrseq.bak", "c.mrseqq", "d.MRSEQ"} // case-sensitive on most platforms
		for _, name := range others {
			path := filepath.Join(dir, name)
			if err := os.WriteFile(path, []byte("dummy"), 0o644); err != nil {
				t.Fatalf("test setup: write %s: %v", path, err)
			}
		}
		if got := hasMrseq(ctx, dir); got {
			t.Fatalf("hasMrseq(ctx, %q) = true; want false", dir)
		}
	})

	t.Run("non-recursive: file only in subdirectory does not count", func(t *testing.T) {
		dir := t.TempDir()
		sub := filepath.Join(dir, "sub")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatalf("test setup: mkdir %s: %v", sub, err)
		}
		f := filepath.Join(sub, "nested.mrseq")
		if err := os.WriteFile(f, []byte("dummy"), 0o644); err != nil {
			t.Fatalf("test setup: write %s: %v", f, err)
		}
		// Glob(dir, "*.mrseq") should not find files in subdir
		if got := hasMrseq(ctx, dir); got {
			t.Fatalf("hasMrseq(ctx, %q) = true; want false (non-recursive glob)", dir)
		}
	})

	// Optional: demonstrate that unrelated extensions don't affect the outcome when at least one *.mrseq exists.
	t.Run("mixed files: presence of .mrseq wins", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("dummy"), 0o644)
		_ = os.WriteFile(filepath.Join(dir, "b.log"), []byte("dummy"), 0o644)
		_ = os.WriteFile(filepath.Join(dir, "c.mrseq"), []byte("dummy"), 0o644)
		if got := hasMrseq(ctx, dir); !got {
			t.Fatalf("hasMrseq(ctx, %q) = false; want true", dir)
		}
	})
}

func makeDirWithMrseq(t *testing.T, dir string, addMrseq bool, version string) string {
	sub := filepath.Join(dir, version)
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("test setup: mkdir %s: %v", sub, err)
	}

	if addMrseq {
		f := filepath.Join(sub, "floopster.mrseq")
		if err := os.WriteFile(f, []byte("0"), 0o644); err != nil {
			t.Fatalf("setup: write %s: %v", f, err)
		}
	}
	return sub
}

func TestDetermineUpgradeVersionDirectories(t *testing.T) {
	tests := []struct {
		name             string
		firstVersion     string
		secondVersion    string
		firstHasMrseq    bool
		secondHasMrseq   bool
		expectedToSuffix string
		expectedFrom     string
	}{
		{
			name:             "first has mrseq, second does not → upgrade to second",
			firstVersion:     "1.0.0",
			secondVersion:    "2.0.0",
			firstHasMrseq:    true,
			secondHasMrseq:   false,
			expectedToSuffix: "second",
			expectedFrom:     "1.0.0",
		},
		{
			name:             "second has mrseq, first does not → upgrade to first",
			firstVersion:     "1.0.0",
			secondVersion:    "2.0.0",
			firstHasMrseq:    false,
			secondHasMrseq:   true,
			expectedToSuffix: "first",
			expectedFrom:     "2.0.0",
		},
		{
			name:             "neither has mrseq → choose higher version (second)",
			firstVersion:     "1.0.0",
			secondVersion:    "2.0.0",
			firstHasMrseq:    false,
			secondHasMrseq:   false,
			expectedToSuffix: "second",
			expectedFrom:     "1.0.0",
		},
		{
			name:             "both have mrseq → choose higher version (second)",
			firstVersion:     "1.0.0",
			secondVersion:    "2.0.0",
			firstHasMrseq:    true,
			secondHasMrseq:   true,
			expectedToSuffix: "second",
			expectedFrom:     "1.0.0",
		},
		{
			name:             "equal versions → choose first as upgradeTo",
			firstVersion:     "1.0.0",
			secondVersion:    "1.0.0",
			firstHasMrseq:    false,
			secondHasMrseq:   false,
			expectedToSuffix: "first",
			expectedFrom:     "1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Prepare directories
			dir := t.TempDir()
			firstDir := makeDirWithMrseq(t, dir, tt.firstHasMrseq, tt.firstVersion)
			secondDir := makeDirWithMrseq(t, dir, tt.secondHasMrseq, tt.secondVersion)

			// Simulate environment variables
			os.Setenv(constants.ExtensionVersionEnvName, tt.firstVersion)
			os.Setenv(constants.ExtensionVersionUpdatingFromEnvName, tt.secondVersion)
			os.Setenv(constants.ExtensionPathEnvName, firstDir)

			// Replace secondDir logic: mimic original code's substitution
			// (strings.ReplaceAll(firstDir, firstVersion, secondVersion))
			// For test simplicity, override secondDir directly
			// but ensure substitution works if versions appear in path
			if strings.Contains(firstDir, tt.firstVersion) {
				secondDir = strings.ReplaceAll(firstDir, tt.firstVersion, tt.secondVersion)
			}

			tempDir, _ := os.MkdirTemp("", "determineupgrade")
			defer os.RemoveAll(tempDir)
			handlerEnvironment := handlerenv.HandlerEnvironment{
				EventsFolder: tempDir,
			}

			extensionLogger := logging.New(nil)
			events := extensionevents.New(extensionLogger, &handlerEnvironment)
			ctx := log.NewContext(log.NewNopLogger())

			gotFromDir, gotToDir, gotFromVersion := determineUpgradeVersionDirectories(ctx, events)

			// Validate upgradeFromVersion
			if gotFromVersion != tt.expectedFrom {
				t.Errorf("upgradeFromVersion = %q; want %q", gotFromVersion, tt.expectedFrom)
			}

			// Validate which directory chosen as upgradeTo
			if tt.expectedToSuffix == "first" {
				if gotToDir != firstDir {
					t.Errorf("upgradeToDir = %q; want firstDir %q", gotToDir, firstDir)
				}
				if gotFromDir != secondDir {
					t.Errorf("upgradeFromDir = %q; want secondDir %q", gotFromDir, secondDir)
				}
			} else {
				if gotToDir != secondDir {
					t.Errorf("upgradeToDir = %q; want secondDir %q", gotToDir, secondDir)
				}
				if gotFromDir != firstDir {
					t.Errorf("upgradeFromDir = %q; want firstDir %q", gotFromDir, firstDir)
				}
			}
		})
	}
}
