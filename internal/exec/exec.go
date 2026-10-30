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

	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

type scriptFile interface {
	io.ReadWriteCloser
	Chown(int, int) error
	Chmod(os.FileMode) error
}

var (
	openScriptFile = func(path string, flag int, mode os.FileMode) (scriptFile, error) {
		return os.OpenFile(path, flag, mode)
	}
	makeScriptDirectory = os.MkdirAll
	removeScriptFile    = os.Remove
	lookupRunAsUser     = user.Lookup
	setEnvironment      = os.Setenv
	runCommand          = func(command *exec.Cmd) error { return command.Run() }
	copyScript          = io.Copy
	commandExitStatus   = func(err error) (int, bool, bool) {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			return 0, false, false
		}
		status, ok := exitErr.Sys().(syscall.WaitStatus)
		return status.ExitStatus(), status.Signaled(), ok
	}
)

// Exec runs the given cmd in /bin/sh, saves its stdout/stderr streams to
// the specified files. It waits until the execution terminates.
//
// On error, an exit code may be returned if it is an exit code error.
// Given stdout and stderr will be closed upon returning.
func Exec(ctx *log.Context, cmd, workdir string, stdout, stderr io.WriteCloser, cfg *handlersettings.HandlerSettings) (exitCode int, resultErr error) {
	defer func() {
		if err := stdout.Close(); err != nil && resultErr == nil {
			resultErr = errorutil.WithCode(errorutil.Os_FailedToCloseStdOut, errors.Wrap(err, "failed to close stdout"))
			exitCode = constants.ExitCode_OpenStdOutFileFailed
		}
	}()
	defer func() {
		if err := stderr.Close(); err != nil && resultErr == nil {
			resultErr = errorutil.WithCode(errorutil.Os_FailedToCloseStdErr, errors.Wrap(err, "failed to close stderr"))
			exitCode = constants.ExitCode_OpenStdErrFileFailed
		}
	}()

	scriptPath := cmd

	commandArgs, err := SetEnvironmentVariables(cfg)
	if err != nil {
		return constants.ExitCode_CommandExecutionFailed, err
	}
	// Add command args if any. Unnamed arguments go in 'commandArgs'. Named arguments are set as environment variables so the'd be available within the script.
	cmd = cmd + commandArgs

	exitCode = constants.ExitCode_Okay

	if cfg.PublicSettings.RunAsUser != "" {
		ctx.Log("message", "RunAsUser is "+cfg.PublicSettings.RunAsUser)

		// Check prefix ("/var/lib/waagent/run-command-handler") exists in script path for ex. /var/lib/waagent/run-command-handler/download/<runcommandName>/0/script.sh
		if !strings.HasPrefix(scriptPath, constants.DataDir) {
			errMessage := "Failed to determine RunAs script path. Contact ICM team AzureRT\\Extensions for this service error."
			ctx.Log("message", errMessage)
			return constants.ExitCode_RunAsIncorrectScriptPath, errorutil.WithCode(errorutil.Internal_incorrectRunAsScriptPath, errors.New(errMessage))
		}

		// Gets suffix "download/<runcommandName>/0/script.sh"
		downloadPathSuffix := scriptPath[len(constants.DataDir):]
		// formats into something like "/home/<RunAsUserName>/waagent/run-command-handler-runas/download/<runcommandName>/0/script.sh", This filepath shouldn't exist yet.
		runAsScriptFilePath := filepath.Join(fmt.Sprintf(constants.RunAsDir, cfg.PublicSettings.RunAsUser), downloadPathSuffix)
		runAsScriptDirectoryPath := filepath.Dir(runAsScriptFilePath) // Get directory of runAsScript that doesn't exist yet

		// Create runAsScriptDirectoryPath and its intermediate directories if they do not exist
		if err := makeScriptDirectory(runAsScriptDirectoryPath, 0777); err != nil {
			return constants.ExitCode_RunAsCreateRunAsScriptFileFailed, errorutil.WithCode(errorutil.Os_FailedToCreateRunAsDirectory, errors.Wrap(err, "failed to create RunAs script directory"))
		}

		/// Copy source script at scriptPath to runAsScriptDirectoryPath
		// Get reference to source script by opening it
		sourceScriptFile, sourceScriptFileOpenError := openScriptFile(scriptPath, os.O_RDONLY, 0400)
		if sourceScriptFileOpenError != nil {
			errMessage := "Failed to open source script. Contact ICM team AzureRT\\Extensions for this service error."
			ctx.Log("message", errMessage+fmt.Sprintf(" Source script file is '%s'", scriptPath))
			return constants.ExitCode_RunAsOpenSourceScriptFileFailed, errorutil.WithCode(errorutil.Os_FailedToOpenRunAsSourceScript, errors.Wrap(sourceScriptFileOpenError, errMessage))
		}

		destScriptFile, destScriptCreateError := createRunAsScriptFile(runAsScriptFilePath)
		if destScriptCreateError != nil {
			sourceScriptFile.Close()
			errMessage := "Failed to create script for Run As in Run As directory. Contact ICM team AzureRT\\Extensions for this service error."
			ctx.Log("message", errMessage+fmt.Sprintf(" Destination runAs script file is '%s'", runAsScriptFilePath))
			return constants.ExitCode_RunAsCreateRunAsScriptFileFailed, errors.Wrap(destScriptCreateError, errMessage)
		}
		_, runAsScriptCopyError := copyScript(destScriptFile, sourceScriptFile)
		if runAsScriptCopyError != nil {
			sourceScriptFile.Close()
			destScriptFile.Close()
			errMessage := fmt.Sprintf("Failed to copy script file '%s' to Run As path '%s'. Contact ICM team AzureRT\\Extensions for this service error.", scriptPath, runAsScriptFilePath)
			ctx.Log("message", errMessage)
			return constants.ExitCode_RunAsCopySourceScriptToRunAsScriptFileFailed, errorutil.WithCode(errorutil.Os_FailedToCopyRunAsScript, errors.Wrap(runAsScriptCopyError, errMessage))
		}
		if err := sourceScriptFile.Close(); err != nil {
			destScriptFile.Close()
			return constants.ExitCode_RunAsCopySourceScriptToRunAsScriptFileFailed, errorutil.WithCode(errorutil.Os_FailedToCloseRunAsSourceScript, errors.Wrap(err, "failed to close source script"))
		}

		// Provide read and execute permissions to RunAsUser on .sh file at runAsScriptFilePath
		lookedUpUser, lookupUserError := lookupRunAsUser(cfg.PublicSettings.RunAsUser)
		if lookupUserError != nil {
			destScriptFile.Close()
			errMessage := fmt.Sprintf("Failed to lookup RunAs user '%s'. Looks like user does not exist. For RunAs to work properly, contact admin of VM and make sure RunAs user is added on the VM and user has access to resources accessed by the Run Command (Directories, Files, Network etc.). Refer: https://aka.ms/RunCommandManagedLinux", cfg.PublicSettings.RunAsUser)
			ctx.Log("message", errMessage)
			return constants.ExitCode_RunAsLookupUserFailed, errorutil.WithCode(errorutil.CommandExecution_runAsUserNotFound, errors.Wrap(lookupUserError, errMessage))
		}

		lookedUpUserUid, lookedUpUserUidErr := strconv.Atoi(lookedUpUser.Uid)
		if lookedUpUserUidErr != nil {
			destScriptFile.Close()
			errMessage := "Failed to determine RunAs user's Uid and Guid . Contact ICM team AzureRT\\Extensions for this service error."
			ctx.Log("message", errMessage)
			return constants.ExitCode_RunAsLookupUserUidFailed, errorutil.WithCode(errorutil.Os_InvalidRunAsUserId, errors.Wrap(lookedUpUserUidErr, errMessage))
		}

		runAsScriptChownError := destScriptFile.Chown(lookedUpUserUid, os.Getegid())
		if runAsScriptChownError != nil {
			destScriptFile.Close()
			errMessage := fmt.Sprintf("Failed to change owner of file '%s' to RunAs user '%s'. Contact ICM team AzureRT\\Extensions for this service error.", runAsScriptFilePath, cfg.PublicSettings.RunAsUser)
			ctx.Log("message", errMessage)
			return constants.ExitCode_RunAsScriptFileChangeOwnerFailed, errorutil.WithCode(errorutil.Os_FailedToChangeRunAsScriptOwner, errors.Wrap(runAsScriptChownError, errMessage))
		}

		runAsScriptChmodError := destScriptFile.Chmod(0550)
		if runAsScriptChmodError != nil {
			destScriptFile.Close()
			errMessage := fmt.Sprintf("Failed to change permissions to execute for file '%s' for RunAs user '%s'. Contact ICM team AzureRT\\Extensions for this service error.", runAsScriptFilePath, cfg.PublicSettings.RunAsUser)
			ctx.Log("message", errMessage)
			return constants.ExitCode_RunAsScriptFileChangePermissionsFailed, errorutil.WithCode(errorutil.Os_FailedToChangeRunAsScriptPermissions, errors.Wrap(runAsScriptChmodError, errMessage))
		}
		if err := destScriptFile.Close(); err != nil {
			return constants.ExitCode_RunAsCreateRunAsScriptFileFailed, errorutil.WithCode(errorutil.Os_FailedToCloseRunAsScript, errors.Wrap(err, "failed to close RunAs script"))
		}

		// Execute the command as RunAsUser. -n for non-interactive mode (do not prompt for password)
		// sudo -n -u <cfg.publicSettings.RunAsUser> <command>
		cmd = fmt.Sprintf("sudo -n -u %s %s", cfg.PublicSettings.RunAsUser, runAsScriptFilePath+commandArgs)
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
	err = runCommand(command)
	if err != nil {
		if code, signaled, ok := commandExitStatus(err); ok {
			exitCode = code
			if signaled {
				ctx.Log("message", "Timeout:"+err.Error())
				return exitCode, errorutil.WithCode(errorutil.CommandExecution_interruptedByVmShutdown, fmt.Errorf("command terminated with exit status=%d", exitCode))
			}
			return exitCode, errorutil.WithCode(errorutil.CommandExecution_failureExitCode, fmt.Errorf("command terminated with exit status=%d", exitCode))
		}
	}

	return exitCode, errorutil.WithCode(errorutil.CommandExecution_failedUnknownError, errors.Wrap(err, "failed to execute command"))
}

func createRunAsScriptFile(path string) (scriptFile, error) {
	// os.Remove does not follow symlinks, so it is safe to use here.
	if err := removeScriptFile(path); err != nil && !os.IsNotExist(err) {
		return nil, errorutil.WithCode(errorutil.Os_FailedToRemoveRunAsScript, err)
	}

	// do not follow symlinks to avoid symlink attacks.
	file, err := openScriptFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|runAsNoFollow, 0500)
	return file, errorutil.WithCode(errorutil.Os_FailedToCreateRunAsScript, err)
}

func SetEnvironmentVariables(cfg *handlersettings.HandlerSettings) (string, error) {
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
				if err := setEnvironment(name, value); err != nil {
					return commandArgs, errorutil.WithCode(errorutil.CustomerInput_invalidParameterEnvironmentVariable, errors.Wrap(err, "failed to set parameter environment variable"))
				}
			} else { // Unnamed parameters go to command args
				commandArgs += " " + value
			}
		}
	}

	return commandArgs, nil
}

// ExecCmdInDir executes the given command in given directory and saves output
// to ./stdout and ./stderr files (truncates files if exists, creates them if not
// with 0600/-rw------- permissions).
//
// Ideally, we execute commands only once per sequence number in run-command-handler,
// and save their output under /var/lib/waagent/<dir>/download/<seqnum>/*.
func ExecCmdInDir(ctx *log.Context, scriptFilePath, workdir string, cfg *handlersettings.HandlerSettings) (error, int) {

	stdoutFileName, stderrFileName := LogPaths(workdir)

	outF, err := openScriptFile(stdoutFileName, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return errorutil.WithCode(errorutil.Os_FailedToOpenStdOut, errors.Wrapf(err, "failed to open stdout file")), constants.ExitCode_OpenStdOutFileFailed
	}
	errF, err := openScriptFile(stderrFileName, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		outF.Close()
		return errorutil.WithCode(errorutil.Os_FailedToOpenStdErr, errors.Wrapf(err, "failed to open stderr file")), constants.ExitCode_OpenStdErrFileFailed
	}

	exitCode, err := Exec(ctx, scriptFilePath, workdir, outF, errF, cfg)
	return err, exitCode
}

// LogPaths returns stdout and stderr file paths for the specified output
// directory. It does not create the files.
func LogPaths(dir string) (stdout string, stderr string) {
	return filepath.Join(dir, "stdout"), filepath.Join(dir, "stderr")
}
