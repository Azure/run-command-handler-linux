package main

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

	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

// Exec runs the given cmd in /bin/sh, saves its stdout/stderr streams to
// the specified files. It waits until the execution terminates.
//
// On error, an exit code may be returned if it is an exit code error.
// Given stdout and stderr will be closed upon returning.
func Exec(ctx *log.Context, cmd, workdir string, stdout, stderr io.WriteCloser, cfg *handlerSettings) (int, error) {
	defer stdout.Close()
	defer stderr.Close()

	scriptPath := cmd

	commandArgs, err := SetEnvironmentVariables(cfg)
	// Add command args if any. Unnamed arguments go in 'commandArgs'. Named arguments are set as environment variables so the'd be available within the script.
	cmd = cmd + commandArgs

	//executionMessage := ""   // TODO: return
	exitCode := 0 // TODO: return exit code and execution state

	if cfg.publicSettings.RunAsUser != "" {
		ctx.Log("message", "RunAsUser is "+cfg.publicSettings.RunAsUser)

		// Check prefix ("/var/lib/waagent/run-command-handler") exists in script path for ex. /var/lib/waagent/run-command-handler/download/<runcommandName>/0/script.sh
		if !strings.HasPrefix(scriptPath, dataDir) {
			errMessage := "Failed to determine RunAs script path. Contact ICM team AzureRT\\Extensions for this service error."
			ctx.Log("message", errMessage)
			return failedExitCodeGeneral, errors.New(errMessage)
		}

		// Gets suffix "download/<runcommandName>/0/script.sh"
		downloadPathSuffix := scriptPath[len(dataDir):]
		// formats into something like "/home/<RunAsUserName>/waagent/run-command-handler-runas/download/<runcommandName>/0/script.sh", This filepath doesn't exist yet.
		runAsScriptFilePath := filepath.Join(fmt.Sprintf(runAsDir, cfg.publicSettings.RunAsUser), downloadPathSuffix)
		runAsScriptDirectoryPath := filepath.Dir(runAsScriptFilePath) // Get directory of runAsScript that doesn't exist yet

		// Create runAsScriptDirectoryPath and its intermediate directories if they do not exist
		os.MkdirAll(runAsScriptDirectoryPath, 0777)

		/// Copy source script at scriptPath to runAsScriptDirectoryPath
		// Get reference to source script by opening it
		sourceScriptFile, sourceScriptFileOpenError := os.OpenFile(scriptPath, os.O_RDONLY, 0400)
		if sourceScriptFileOpenError != nil {
			errMessage := "Failed to open source script. Contact ICM team AzureRT\\Extensions for this service error."
			ctx.Log("message", errMessage+fmt.Sprintf(" Source script file is '%s'", scriptPath))
			return failedExitCodeGeneral, errors.Wrapf(sourceScriptFileOpenError, errMessage)
		}

		destScriptFile, destScriptCreateError := os.Create(runAsScriptFilePath)
		if destScriptCreateError != nil {
			errMessage := "Failed to create script for Run As in Run As directory. Contact ICM team AzureRT\\Extensions for this service error."
			ctx.Log("message", errMessage+fmt.Sprintf(" Destination runAs script file is '%s'", runAsScriptFilePath))
			return failedExitCodeGeneral, errors.Wrapf(destScriptCreateError, errMessage)
		}
		_, runAsScriptCopyError := io.Copy(destScriptFile, sourceScriptFile)
		if runAsScriptCopyError != nil {
			errMessage := fmt.Sprintf("Failed to copy script file '%s' to Run As path '%s'. Contact ICM team AzureRT\\Extensions for this service error.", scriptPath, runAsScriptFilePath)
			ctx.Log("message", errMessage)
			return failedExitCodeGeneral, errors.Wrapf(runAsScriptCopyError, errMessage)
		}
		sourceScriptFile.Close()
		destScriptFile.Close()

		// Provide read and execute permissions to RunAsUser on .sh file at runAsScriptFilePath
		lookedUpUser, lookupUserError := user.Lookup(cfg.publicSettings.RunAsUser)
		if lookupUserError != nil {
			errMessage := fmt.Sprintf("Failed to lookup RunAs user '%s'. Looks like user does not exist. For RunAs to work properly, contact admin of VM and make sure RunAs user is added on the VM and user has access to resources accessed by the Run Command (Directories, Files, Network etc.). Refer: https://aka.ms/RunCommandManagedLinux", cfg.publicSettings.RunAsUser)
			ctx.Log("message", errMessage)
			return failedExitCodeGeneral, errors.Wrapf(lookupUserError, errMessage)
		}

		lookedUpUserUid, lookedUpUserUidErr := strconv.Atoi(lookedUpUser.Uid)
		if lookedUpUserUidErr != nil {
			errMessage := "Failed to determine RunAs user's Uid and Guid . Contact ICM team AzureRT\\Extensions for this service error."
			ctx.Log("message", errMessage)
			return failedExitCodeGeneral, errors.Wrapf(lookedUpUserUidErr, errMessage)
		}

		runAsScriptChownError := os.Chown(runAsScriptFilePath, lookedUpUserUid, os.Getegid())
		if runAsScriptChownError != nil {
			errMessage := fmt.Sprintf("Failed to change owner of file '%s' to RunAs user '%s'. Contact ICM team AzureRT\\Extensions for this service error.", runAsScriptFilePath, cfg.publicSettings.RunAsUser)
			ctx.Log("message", errMessage)
			return failedExitCodeGeneral, errors.Wrapf(runAsScriptChownError, errMessage)
		}

		runAsScriptChmodError := os.Chmod(runAsScriptFilePath, 0550)
		if runAsScriptChmodError != nil {
			errMessage := fmt.Sprintf("Failed to change permissions to execute for file '%s' for RunAs user '%s'. Contact ICM team AzureRT\\Extensions for this service error.", runAsScriptFilePath, cfg.publicSettings.RunAsUser)
			ctx.Log("message", errMessage)
			return failedExitCodeGeneral, errors.Wrapf(runAsScriptChmodError, errMessage)
		}

		// echo pipes the RunAsPassword to sudo -S for RunAsUser instead of prompting the password interactively from user and blocking.
		// echo <cfg.protectedSettings.RunAsPassword> | sudo -S -u <cfg.publicSettings.RunAsUser> <command>
		cmd = fmt.Sprintf("echo %s | sudo -S -u %s %s", cfg.protectedSettings.RunAsPassword, cfg.publicSettings.RunAsUser, runAsScriptFilePath+commandArgs)
		ctx.Log("message", "RunAs cmd is "+cmd)
	}

	var command *exec.Cmd
	if cfg.publicSettings.TimeoutInSeconds > 0 {
		commandContext, cancel := context.WithTimeout(context.Background(), time.Duration(1)*time.Second)
		defer cancel()
		command = exec.CommandContext(commandContext, "/bin/bash", "-c", cmd)
		ctx.Log("message", "Execute with TimeoutInSeconds="+strconv.Itoa(cfg.publicSettings.TimeoutInSeconds))
	} else {
		command = exec.Command("/bin/bash", "-c", cmd)
	}

	command.Dir = workdir
	command.Stdout = stdout
	command.Stderr = stderr
	err = command.Run()
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				exitCode = status.ExitStatus()
				if status.Signaled() { // Timed out
					ctx.Log("message", "Timeout:"+err.Error())
				}
				return exitCode, fmt.Errorf("command terminated with exit status=%d", exitCode)
			}
		}
	}

	return exitCode, errors.Wrapf(err, "failed to execute command")
}

func SetEnvironmentVariables(cfg *handlerSettings) (string, error) {
	var err error
	commandArgs := ""
	parameters := []parameterDefinition{}
	if cfg.publicSettings.Parameters != nil && len(cfg.publicSettings.Parameters) > 0 {
		parameters = cfg.publicSettings.Parameters
	}
	if cfg.protectedSettings.ProtectedParameters != nil && len(cfg.protectedSettings.ProtectedParameters) > 0 {
		parameters = append(parameters, cfg.protectedSettings.ProtectedParameters...)
	}

	for i := 0; i < len(parameters); i++ {
		name := parameters[i].Name
		value := parameters[i].Value
		if value != "" {
			if name != "" { // Named parameters are set as environmental setting
				err = os.Setenv(name, value)
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
func ExecCmdInDir(ctx *log.Context, scriptFilePath, workdir string, cfg *handlerSettings) error {

	stdoutFileName, stderrFileName := logPaths(workdir)

	outF, err := os.OpenFile(stdoutFileName, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return errors.Wrapf(err, "failed to open stdout file")
	}
	errF, err := os.OpenFile(stderrFileName, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return errors.Wrapf(err, "failed to open stderr file")
	}

	_, err = Exec(ctx, scriptFilePath, workdir, outF, errF, cfg)

	return err
}

// logPaths returns stdout and stderr file paths for the specified output
// directory. It does not create the files.
func logPaths(dir string) (stdout string, stderr string) {
	return filepath.Join(dir, "stdout"), filepath.Join(dir, "stderr")
}
