package commands

import (
	"encoding/json"
	"errors"
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
	os.Setenv(constants.VersionEnvName, "1.3.8")

	currentVersion := os.Getenv(constants.VersionEnvName)
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
	os.Setenv(constants.VersionEnvName, "1.3.8")

	// Create two extensions
	enable_extension(t, fakeEnv, oldVersionDirectory, "happyChipmunk", true, 0)
	enable_extension(t, fakeEnv, oldVersionDirectory, "crazyChipmunk", true, 0)

	// Now, pretend that the extension was updated
	// Step 1: WALA calls Disable on our two extensions
	disable_extension(t, fakeEnv, oldVersionDirectory, "happyChipmunk")
	disable_extension(t, fakeEnv, oldVersionDirectory, "crazyChipmunk")

	// Step 2: WALA will call update
	os.Setenv(constants.VersionEnvName, "1.3.9")
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
	os.Setenv(constants.VersionEnvName, "1.3.26")

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
	os.Setenv(constants.VersionEnvName, "1.3.27")
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
	os.Setenv(constants.VersionEnvName, "1.3.17")

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
	os.Setenv(constants.VersionEnvName, "1.3.18")
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
			expected: []int{1, -2, 3},
		},
		{
			name:     "plus-sign",
			in:       "+1.2",
			expected: []int{1, 2},
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

func Test_determineUpgradeVersionDirectories_Upgrade_PathContainsToVersion(t *testing.T) {
	/*
	   Upgrade: updatingFrom != toVersion
	   Path contains the toVersion -> replace toVersion with fromVersion for the "from" dir
	*/
	to := "2.5.0"
	from := "2.4.3"
	curr := "2.5.0" // current extension version value (doesn't affect upgrade/downgrade decision)
	dir := "/var/lib/waagent/My.Ext/" + to + "/"

	setEnvs(t, to, curr, from, dir)

	tempDir, _ := os.MkdirTemp("", "upgradetoversion")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	ctx := log.NewContext(log.NewNopLogger())
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	fromDir, toDir, gotFromVersion := determineUpgradeVersionDirectories(ctx, events)

	require.Equal(t, from, gotFromVersion, "upgradeFromVersion should be updating-from value on upgrade")
	require.Equal(t, dir, toDir, "when path contains toVersion, toDir is the given path")

	expectedFromDir := strings.ReplaceAll(dir, to, from)
	require.Equal(t, expectedFromDir, fromDir)
}

func Test_determineUpgradeVersionDirectories_Upgrade_PathContainsFromVersion(t *testing.T) {
	/*
	   Upgrade: updatingFrom != toVersion
	   Path contains the fromVersion -> replace fromVersion with toVersion for the "to" dir
	*/
	to := "3.0.1"
	from := "2.9.9"
	curr := "3.0.1"
	dir := "/opt/exts/My.Ext/" + from + "/bin"

	setEnvs(t, to, curr, from, dir)

	tempDir, _ := os.MkdirTemp("", "upgradefromversion")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	ctx := log.NewContext(log.NewNopLogger())
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	fromDir, toDir, gotFromVersion := determineUpgradeVersionDirectories(ctx, events)

	require.Equal(t, from, gotFromVersion)
	require.Equal(t, dir, fromDir, "when path does NOT contain toVersion, fromDir is the given path")

	expectedToDir := strings.ReplaceAll(dir, from, to)
	require.Equal(t, expectedToDir, toDir)
}

func Test_determineUpgradeVersionDirectories_Downgrade_PathContainsToVersion(t *testing.T) {
	/*
	   Downgrade: updatingFrom == toVersion
	   upgradeFromVersion becomes extensionVersionValue (curr)
	   Path contains toVersion -> replace toVersion with fromVersion (curr) for the "from" dir
	*/
	to := "2.1.0"
	curr := "2.3.0" // extensionVersionValue
	fromUpdating := to
	dir := "C:\\Packages\\Plugins\\My.Ext\\" + to + "\\"

	setEnvs(t, to, curr, fromUpdating, dir)

	tempDir, _ := os.MkdirTemp("", "downgradetoversion")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	ctx := log.NewContext(log.NewNopLogger())
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	fromDir, toDir, gotFromVersion := determineUpgradeVersionDirectories(ctx, events)

	require.Equal(t, curr, gotFromVersion, "on downgrade, fromVersion becomes the current extension version")
	require.Equal(t, dir, toDir)

	expectedFromDir := strings.ReplaceAll(dir, to, curr)
	require.Equal(t, expectedFromDir, fromDir)
}

func Test_determineUpgradeVersionDirectories_Downgrade_PathContainsFromVersion(t *testing.T) {
	/*
	   Downgrade: updatingFrom == toVersion
	   Path contains the computed fromVersion (curr), so toDir is replacement of fromVersion->toVersion
	*/
	to := "1.7.0"
	curr := "1.7.5"
	fromUpdating := to
	dir := "/extensions/handler/" + curr + "/"

	setEnvs(t, to, curr, fromUpdating, dir)

	tempDir, _ := os.MkdirTemp("", "downgradefromversion")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	ctx := log.NewContext(log.NewNopLogger())
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	fromDir, toDir, gotFromVersion := determineUpgradeVersionDirectories(ctx, events)

	require.Equal(t, curr, gotFromVersion)
	require.Equal(t, dir, fromDir)

	expectedToDir := strings.ReplaceAll(dir, curr, to)
	require.Equal(t, expectedToDir, toDir)
}

func Test_rehydrateMrSeqFiles_OpenStatusDirFails(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "OpenStatusDirFails")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	ctx := log.NewContext(log.NewNopLogger())
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	from := t.TempDir() // does not contain the status subdir
	to := t.TempDir()

	err := rehydrateMrSeqFilesForProblematicUpgrades(ctx, from, to, events)
	require.NotNil(t, err, "expected error when opening missing status dir")
	require.True(t, strings.Contains(err.Error(), "Failed to open status directory"), "unexpected error message: %s", err.Error())
}

func Test_rehydrateMrSeqFiles_ReadDirFails(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "ReadDirFails")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	ctx := log.NewContext(log.NewNopLogger())
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	from := filepath.Join(tempDir, "from")
	require.NoError(t, os.Mkdir(from, 0o755))
	to := filepath.Join(tempDir, "to")
	require.NoError(t, os.Mkdir(to, 0o755))

	// Create a *file* at "<from>/status" so os.Open succeeds but ReadDir fails.
	statusPath := filepath.Join(from, constants.StatusFileDirectory)
	require.NoError(t, os.WriteFile(statusPath, []byte("not a directory"), 0o600))

	err := rehydrateMrSeqFilesForProblematicUpgrades(ctx, from, to, events)
	require.NotNil(t, err, "expected error when ReadDir fails")
	require.True(t, strings.Contains(err.Error(), "could not read directory entries"), "unexpected error message: %s", err.Error())
}

func Test_rehydrateMrSeqFiles_IgnoresInvalidStatusFilename(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "IgnoresInvalidStatusFilename")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	ctx := log.NewContext(log.NewNopLogger())
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	from := filepath.Join(tempDir, "from")
	require.NoError(t, os.Mkdir(from, 0o755))
	to := filepath.Join(tempDir, "to")
	require.NoError(t, os.Mkdir(to, 0o755))

	// Proper status directory
	require.NoError(t, os.MkdirAll(filepath.Join(from, constants.StatusFileDirectory), 0o755))

	// Invalid filename (only two parts, missing seqNo). Should be ignored.
	invalid := filepath.Join(from, constants.StatusFileDirectory, "alpha"+constants.StatusFileExtension)
	require.NoError(t, os.WriteFile(invalid, []byte(""), 0o600))

	err := rehydrateMrSeqFilesForProblematicUpgrades(ctx, from, to, events)
	require.Nil(t, err, "unexpected error: %v", err)

	// No mrseq should be created for invalid filenames.
	_, err = os.Stat(filepath.Join(to, "alpha"+constants.MrSeqFileExtension))
	require.True(t, os.IsNotExist(err), "alpha%s should not have been created", constants.MrSeqFileExtension)
}

func Test_rehydrateMrSeqFiles_RehydrateMissingMrseq(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "RehydrateMissingMrseq")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	ctx := log.NewContext(log.NewNopLogger())
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	from := filepath.Join(tempDir, "from")
	require.NoError(t, os.Mkdir(from, 0o755))
	to := filepath.Join(tempDir, "to")
	require.NoError(t, os.Mkdir(to, 0o755))

	statusDir := filepath.Join(from, constants.StatusFileDirectory)
	if err := os.MkdirAll(statusDir, 0o755); err != nil {
		t.Fatalf("mkdir status: %v", err)
	}

	// alpha.5.status → should create to/alpha.mrseq with "5"
	alphaStatus := filepath.Join(statusDir, "alpha.5"+constants.StatusFileExtension)
	require.NoError(t, os.WriteFile(alphaStatus, []byte(""), 0o600))

	err := rehydrateMrSeqFilesForProblematicUpgrades(ctx, from, to, events)
	require.Nil(t, err, "unexpected error: %v", err)

	mrseqPath := filepath.Join(to, "alpha"+constants.MrSeqFileExtension)
	got := mustReadFile(t, mrseqPath)
	require.Equal(t, "5", got, "mrseq content = %q, want %q", got, "5")
}

func Test_rehydrateMrSeqFiles_UpdateExistingMrseqWhenHigherSeqFound(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "UpdateExistingMrseqWhenHigherSeqFound")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	ctx := log.NewContext(log.NewNopLogger())
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	from := filepath.Join(tempDir, "from")
	require.NoError(t, os.Mkdir(from, 0o755))
	to := filepath.Join(tempDir, "to")
	require.NoError(t, os.Mkdir(to, 0o755))

	statusDir := filepath.Join(from, constants.StatusFileDirectory)
	require.NoError(t, os.MkdirAll(statusDir, 0o755))

	// Existing mrseq=3
	mrseqPath := filepath.Join(to, "alpha"+constants.MrSeqFileExtension)
	require.NoError(t, os.WriteFile(mrseqPath, []byte("3"), 0o600))

	// Status reports seq=5 → should overwrite to "5"
	alphaStatus := filepath.Join(statusDir, "alpha.5"+constants.StatusFileExtension)
	require.NoError(t, os.WriteFile(alphaStatus, []byte(""), 0o600))

	err := rehydrateMrSeqFilesForProblematicUpgrades(ctx, from, to, events)
	require.Nil(t, err, "unexpected error: %v", err)

	got := mustReadFile(t, mrseqPath)
	require.Equal(t, "5", got, "mrseq content = %q, want %q", got, "5")
}

func Test_rehydrateMrSeqFiles_NoUpdateWhenExistingIsHigher(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "NoUpdateWhenExistingIsHigher")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	ctx := log.NewContext(log.NewNopLogger())
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	from := filepath.Join(tempDir, "from")
	require.NoError(t, os.Mkdir(from, 0o755))
	to := filepath.Join(tempDir, "to")
	require.NoError(t, os.Mkdir(to, 0o755))

	statusDir := filepath.Join(from, constants.StatusFileDirectory)
	require.NoError(t, os.MkdirAll(statusDir, 0o755))

	// Existing mrseq=7
	mrseqPath := filepath.Join(to, "alpha "+constants.MrSeqFileExtension)
	require.NoError(t, os.WriteFile(mrseqPath, []byte("7"), 0o600))

	// Status reports seq=5 → should NOT overwrite
	alphaStatus := filepath.Join(statusDir, "alpha.5"+constants.StatusFileExtension)
	require.NoError(t, os.WriteFile(alphaStatus, []byte(""), 0o600))

	err := rehydrateMrSeqFilesForProblematicUpgrades(ctx, from, to, events)
	require.Nil(t, err, "unexpected error: %v", err)

	got := mustReadFile(t, mrseqPath)
	require.Equal(t, "7", got, "mrseq content = %q, want %q", got, "7")
}

func Test_rehydrateMrSeqFiles_MultipleStatusFiles_TakesMax(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "TakesMax")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	ctx := log.NewContext(log.NewNopLogger())
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	from := filepath.Join(tempDir, "from")
	require.NoError(t, os.Mkdir(from, 0o755))
	to := filepath.Join(tempDir, "to")
	require.NoError(t, os.Mkdir(to, 0o755))

	statusDir := filepath.Join(from, constants.StatusFileDirectory)
	require.NoError(t, os.MkdirAll(statusDir, 0o755))

	// Both alpha.3.status and alpha.7.status — final mrseq should be "7"
	for _, seq := range []int{3, 7} {
		p := filepath.Join(statusDir, "alpha."+strconv.Itoa(seq)+constants.StatusFileExtension)
		require.NoError(t, os.WriteFile(p, []byte(""), 0o600))
	}

	err := rehydrateMrSeqFilesForProblematicUpgrades(ctx, from, to, events)
	require.Nil(t, err, "unexpected error: %v", err)

	got := mustReadFile(t, filepath.Join(to, "alpha"+constants.MrSeqFileExtension))
	require.Equal(t, "7", got, "mrseq content = %q, want %q", got, "7")
}

func Test_rehydrateMrSeqFiles_ReadExistingMrseqFails(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "ReadExistingMrseqFails")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	ctx := log.NewContext(log.NewNopLogger())
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	from := filepath.Join(tempDir, "from")
	require.NoError(t, os.Mkdir(from, 0o755))
	to := filepath.Join(tempDir, "to")
	require.NoError(t, os.Mkdir(to, 0o755))

	statusDir := filepath.Join(from, constants.StatusFileDirectory)
	require.NoError(t, os.MkdirAll(statusDir, 0o755))

	// Make a directory at the mrseq path so ReadFile fails
	mrseqPath := filepath.Join(to, "alpha"+constants.MrSeqFileExtension)
	require.NoError(t, os.MkdirAll(mrseqPath, 0o755))

	// Now create a status that would try to read/compare
	alphaStatus := filepath.Join(statusDir, "alpha.5"+constants.StatusFileExtension)
	require.NoError(t, os.WriteFile(alphaStatus, []byte(""), 0o600))

	err := rehydrateMrSeqFilesForProblematicUpgrades(ctx, from, to, events)
	require.NotNil(t, err, "expected error due to unreadable mrseq")
	require.True(t, strings.Contains(err.Error(), "Could not read file"), "Unexpected error: %v", err.Error())
}

// setEnvs is a helper to seed the env for each scenario.
func setEnvs(t *testing.T, to, curr, from, dir string) {
	t.Helper()
	t.Setenv(constants.VersionEnvName, to)
	t.Setenv(constants.ExtensionVersionEnvName, curr)
	t.Setenv(constants.ExtensionVersionUpdatingFromEnvName, from)
	t.Setenv(constants.ExtensionPathEnvName, dir)
}

func mustReadFile(t *testing.T, p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}
