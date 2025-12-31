package exec

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Azure/azure-extension-platform/vmextension"
	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

var (
	fnIoCopy     = io.Copy
	fnOsChMod    = os.Chmod
	fnOsChown    = os.Chown
	fnOsCreate   = os.Create
	fnOsMkDirAll = os.MkdirAll
	fnOsOpenFile = os.OpenFile
	fnOsSetEnv   = os.Setenv
	fnRunCommand = runCommand
	fnUserLookup = user.Lookup
)

// Exec runs the given cmd in /bin/sh, saves its stdout/stderr streams to
// the specified files. It waits until the execution terminates.
//
// On error, an exit code may be returned if it is an exit code error.
// Given stdout and stderr will be closed upon returning.
func Exec(ctx *log.Context, cmd, workdir string, stdout, stderr io.WriteCloser, cfg *handlersettings.HandlerSettings) (int, error) {
	defer stdout.Close()
	defer stderr.Close()

	scriptPath := cmd

	commandArgs, err := SetEnvironmentVariables(cfg)
	// Add command args if any. Unnamed arguments go in 'commandArgs'. Named arguments are set as environment variables so the'd be available within the script.
	cmd = cmd + commandArgs

	exitCode := constants.ExitCode_Okay

	if cfg.PublicSettings.RunAsUser != "" {
		ctx.Log("message", "RunAsUser is "+cfg.PublicSettings.RunAsUser)

		// Check prefix ("/var/lib/waagent/run-command-handler") exists in script path for ex. /var/lib/waagent/run-command-handler/download/<runcommandName>/0/script.sh
		if !strings.HasPrefix(scriptPath, constants.DataDir) {
			errMessage := "Failed to determine RunAs script path. Contact ICM team AzureRT\\Extensions for this service error."
			ctx.Log("message", errMessage)
			return constants.Internal_IncorrectRunAsScriptPath, vmextension.NewErrorWithClarification(constants.Internal_IncorrectRunAsScriptPath, errors.New(errMessage))
		}

		// Gets suffix "download/<runcommandName>/0/script.sh"
		downloadPathSuffix := scriptPath[len(constants.DataDir):]
		// formats into something like "/home/<RunAsUserName>/waagent/run-command-handler-runas/download/<runcommandName>/0/script.sh", This filepath doesn't exist yet.
		runAsScriptFilePath := filepath.Join(fmt.Sprintf(constants.RunAsDir, cfg.PublicSettings.RunAsUser), downloadPathSuffix)
		runAsScriptDirectoryPath := filepath.Dir(runAsScriptFilePath) // Get directory of runAsScript that doesn't exist yet

		// Create runAsScriptDirectoryPath and its intermediate directories if they do not exist
		fnOsMkDirAll(runAsScriptDirectoryPath, 0777)

		/// Copy source script at scriptPath to runAsScriptDirectoryPath
		// Get reference to source script by opening it
		sourceScriptFile, sourceScriptFileOpenError := fnOsOpenFile(scriptPath, os.O_RDONLY, 0400)
		if sourceScriptFileOpenError != nil {
			errMessage := fmt.Sprintf("Failed to open source script. Contact ICM team AzureRT\\Extensions for this service error. Source script file is '%s'", scriptPath)
			ctx.Log("message", errMessage)
			return constants.Internal_RunAsOpenSourceScriptFileFailed, vmextension.NewErrorWithClarification(constants.Internal_RunAsOpenSourceScriptFileFailed, sourceScriptFileOpenError)
		}

		destScriptFile, destScriptCreateError := fnOsCreate(runAsScriptFilePath)
		if destScriptCreateError != nil {
			errMessage := fmt.Sprintf("Failed to create script for Run As in Run As directory. Contact ICM team AzureRT\\Extensions for this service error. Destination runAs script file is '%s'", runAsScriptFilePath)
			ctx.Log("message", errMessage)
			return constants.Internal_RunAsOpenSourceScriptFileFailed, vmextension.NewErrorWithClarification(constants.Internal_RunAsOpenSourceScriptFileFailed, destScriptCreateError)
		}
		_, runAsScriptCopyError := fnIoCopy(destScriptFile, sourceScriptFile)
		if runAsScriptCopyError != nil {
			errMessage := fmt.Sprintf("Failed to copy script file '%s' to Run As path '%s'. Contact ICM team AzureRT\\Extensions for this service error.", scriptPath, runAsScriptFilePath)
			ctx.Log("message", errMessage)
			return constants.Internal_RunAsCopySourceScriptToRunAsScriptFileFailed, vmextension.NewErrorWithClarification(constants.Internal_RunAsCopySourceScriptToRunAsScriptFileFailed, runAsScriptCopyError)
		}
		sourceScriptFile.Close()
		destScriptFile.Close()

		// Provide read and execute permissions to RunAsUser on .sh file at runAsScriptFilePath
		lookedUpUser, lookupUserError := fnUserLookup(cfg.PublicSettings.RunAsUser)
		if lookupUserError != nil {
			errMessage := fmt.Sprintf("Failed to lookup RunAs user '%s'. Looks like user does not exist. For RunAs to work properly, contact admin of VM and make sure RunAs user is added on the VM and user has access to resources accessed by the Run Command (Directories, Files, Network etc.). Refer: https://aka.ms/RunCommandManagedLinux", cfg.PublicSettings.RunAsUser)
			ctx.Log("message", errMessage)
			return constants.CommandExecution_RunAsUserLogonFailed, vmextension.NewErrorWithClarification(constants.CommandExecution_RunAsUserLogonFailed, lookupUserError)
		}

		lookedUpUserUid, lookedUpUserUidErr := strconv.Atoi(lookedUpUser.Uid)
		if lookedUpUserUidErr != nil {
			errMessage := "Failed to determine RunAs user's Uid and Guid . Contact ICM team AzureRT\\Extensions for this service error."
			ctx.Log("message", errMessage)
			return constants.Internal_RunAsLookupUserUidFailed, vmextension.NewErrorWithClarification(constants.Internal_RunAsLookupUserUidFailed, lookedUpUserUidErr)
		}

		runAsScriptChownError := fnOsChown(runAsScriptFilePath, lookedUpUserUid, os.Getegid())
		if runAsScriptChownError != nil {
			errMessage := fmt.Sprintf("Failed to change owner of file '%s' to RunAs user '%s'. Contact ICM team AzureRT\\Extensions for this service error.", runAsScriptFilePath, cfg.PublicSettings.RunAsUser)
			ctx.Log("message", errMessage)
			return constants.Internal_RunAsScriptFileChangeOwnerFailed, vmextension.NewErrorWithClarification(constants.Internal_RunAsScriptFileChangeOwnerFailed, runAsScriptChownError)
		}

		runAsScriptChmodError := fnOsChMod(runAsScriptFilePath, 0550)
		if runAsScriptChmodError != nil {
			errMessage := fmt.Sprintf("Failed to change permissions to execute for file '%s' for RunAs user '%s'. Contact ICM team AzureRT\\Extensions for this service error.", runAsScriptFilePath, cfg.PublicSettings.RunAsUser)
			ctx.Log("message", errMessage)
			return constants.Internal_RunAsScriptFileChangePermissionsFailed, vmextension.NewErrorWithClarification(constants.Internal_RunAsScriptFileChangePermissionsFailed, runAsScriptChmodError)
		}

		// echo pipes the RunAsPassword to sudo -S for RunAsUser instead of prompting the password interactively from user and blocking.
		// echo <cfg.protectedSettings.RunAsPassword> | sudo -S -u <cfg.publicSettings.RunAsUser> <command>
		cmd = fmt.Sprintf("echo %s | sudo -S -u %s %s", cfg.ProtectedSettings.RunAsPassword, cfg.PublicSettings.RunAsUser, runAsScriptFilePath+commandArgs)
		ctx.Log("message", "RunAs cmd is "+cmd)
	}

	var command *exec.Cmd
	if cfg.PublicSettings.TimeoutInSeconds > 0 {
		commandContext, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.PublicSettings.TimeoutInSeconds)*time.Second)
		defer cancel()
		command = exec.CommandContext(commandContext, "/bin/bash", "-c", cmd)
		ctx.Log("message", "Execute with TimeoutInSeconds="+strconv.Itoa(cfg.PublicSettings.TimeoutInSeconds))
	} else {
		command = exec.Command("/bin/bash", "-c", cmd)
	}

	command.Dir = workdir
	command.Stdout = stdout
	command.Stderr = stderr
	err = fnRunCommand(command)
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				exitCode = status.ExitStatus()
				commandExitCode := exitCode
				if status.Signaled() { // Timed out
					ctx.Log("message", "Timeout:"+err.Error())
					exitCode = constants.CommandExecution_TimedOut
				} else if exitCode != 0 {
					exitCode = constants.CommandExecution_FailureExitCode
				}

				commandFailedErr := fmt.Errorf("command terminated with exit status=%d", commandExitCode)
				return exitCode, vmextension.NewErrorWithClarification(exitCode, commandFailedErr)
			}
		}
	}

	// The command succeeded
	return exitCode, nil
}

func runCommand(command *exec.Cmd) error {
	return command.Run()
}

func SetEnvironmentVariables(cfg *handlersettings.HandlerSettings) (string, error) {
	var err error
	commandArgs := ""
	parameters := []handlersettings.ParameterDefinition{}
	if cfg.PublicSettings.Parameters != nil && len(cfg.PublicSettings.Parameters) > 0 {
		parameters = cfg.PublicSettings.Parameters
	}
	if cfg.ProtectedSettings.ProtectedParameters != nil && len(cfg.ProtectedSettings.ProtectedParameters) > 0 {
		parameters = append(parameters, cfg.ProtectedSettings.ProtectedParameters...)
	}

	for i := 0; i < len(parameters); i++ {
		name := parameters[i].Name
		value := parameters[i].Value
		if value != "" {
			if name != "" { // Named parameters are set as environmental setting
				err = fnOsSetEnv(name, value)
			} else { // Unnamed parameters go to command args
				commandArgs += " " + value
			}
		}
	}

	return commandArgs, err // Return command args and the last error if any
}

// ExecCmdInDir executes the given command in given directory and saves output
// to ./stdout and ./stderr files (truncates files if exists, creates them if not
// with 0600/-rw------- permissions).
//
// Ideally, we execute commands only once per sequence number in run-command-handler,
// and save their output under /var/lib/waagent/<dir>/download/<seqnum>/*.
func ExecCmdInDir(ctx *log.Context, scriptFilePath, workdir string, cfg *handlersettings.HandlerSettings) (error, int) {

	stdoutFileName, stderrFileName := LogPaths(workdir)

	outF, err := fnOsOpenFile(stdoutFileName, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return vmextension.NewErrorWithClarification(constants.FileSystem_OpenStandardOutFailed, fmt.Errorf("failed to open stdout file: %v", err)), constants.FileSystem_OpenStandardOutFailed
	}
	errF, err := fnOsOpenFile(stderrFileName, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return vmextension.NewErrorWithClarification(constants.FileSystem_OpenStandardErrorFailed, fmt.Errorf("failed to open stderr file: %v", err)), constants.FileSystem_OpenStandardErrorFailed
	}

	exitCode, err := Exec(ctx, scriptFilePath, workdir, outF, errF, cfg)
	return err, exitCode
}

// LogPaths returns stdout and stderr file paths for the specified output
// directory. It does not create the files.
func LogPaths(dir string) (stdout string, stderr string) {
	return filepath.Join(dir, "stdout"), filepath.Join(dir, "stderr")
}
