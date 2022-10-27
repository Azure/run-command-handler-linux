package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	scriptPathDirectory := filepath.Dir(cmd)

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

		// Create a 4-line script to be run as below to be able to Run As different user.
		scriptLines := [4]string{}

		// Create runAsScriptDirectoryPath and its intermediate directories if they do not exist
		scriptLines[0] = fmt.Sprintf("mkdir -p -m u=rwx %s", runAsScriptDirectoryPath)

		// Copy script at scriptPath to runAsScriptDirectoryPath
		scriptLines[1] = fmt.Sprintf("cp %s %s", scriptPath, runAsScriptDirectoryPath)

		// Provide read and execute permissions to RunAsUser on .sh file at runAsScriptFilePath
		scriptLines[2] = fmt.Sprintf("chown -R %s %s", cfg.publicSettings.RunAsUser, runAsScriptDirectoryPath)

		// echo pipes the RunAsPassword to sudo -S for RunAsUser instead of prompting the password interactively from user and blocking.
		// echo <cfg.protectedSettings.RunAsPassword> | sudo -S -u <cfg.publicSettings.RunAsUser> <command>
		scriptLines[3] = fmt.Sprintf("echo %s | sudo -S -u %s %s", cfg.protectedSettings.RunAsPassword, cfg.publicSettings.RunAsUser, runAsScriptFilePath+commandArgs)

		// Create a shell script file that is run by root at <scriptPathDirectory>/runAsScript.sh that contains script that is run as RunAsUser
		// ex. /var/lib/waagent/run-command-handler/download/<RunCommandName>/<sequenceNumber>/runAsScript.sh
		// Create a writer
		runAsScriptContainerScriptFilePath := filepath.Join(scriptPathDirectory, "runAsScript.sh")
		runAsScriptContainerScript, runAsScriptContainerScriptCreateError := os.Create(runAsScriptContainerScriptFilePath)
		if runAsScriptContainerScriptCreateError != nil {
			return failedExitCodeGeneral, errors.Wrapf(runAsScriptContainerScriptCreateError, fmt.Sprintf("Failed to create RunAs script '%s'. Contact ICM team AzureRT\\Extensions for this service error.", runAsScriptContainerScriptFilePath))
		}
		// Provide permissions to root to read + execute runAsScript.sh
		runAsScriptContainerScriptChmodError := os.Chmod(runAsScriptContainerScriptFilePath, 0550)
		if runAsScriptContainerScriptChmodError != nil {
			return failedExitCodeGeneral, errors.Wrapf(runAsScriptContainerScriptCreateError, fmt.Sprintf("Failed to provide execute permissions to root for RunAs script '%s'. Contact ICM team AzureRT\\Extensions for this service error.", runAsScriptContainerScriptFilePath))
		}

		runAsScriptContainerScriptFileWriter := bufio.NewWriter(runAsScriptContainerScript)
		for _, line := range scriptLines {
			runAsScriptContainerScriptFileWriter.WriteString(line + "\n")
		}
		runAsScriptContainerScriptFileWriter.Flush() // Flush buffer
		runAsScriptContainerScript.Close()

		// .sh script file at runAsScriptContainerScriptFilePath would contain four lines of script to execute the Run Command as RunAsUser
		cmd = runAsScriptContainerScriptFilePath
		ctx.Log("message", "RunAs script is "+cmd)
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
