package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

const (
	// Contains the details of the service to be installed
	serviceDetails = `
	[Unit]
	Description=Managed RunCommand Service

	[Service] 
	User=root
	Restart=always
	RestartSec=5
	WorkingDirectory=/var/lib/waagent/Micrososft.Cplat.Core.RunCommandHandlerLinux/%[1]s
	ExecStart=/var/lib/waagent/Micrososft.Cplat.Core.RunCommandHandlerLinux/%[1]s/run-command-handler

	[Install]
	WantedBy=multi-user.target`

	// The name of the systemd configuration file
	systemdServiceName = "managed-run-command.service"
)

// Installs RunCommand as a service on the client
func InstallService(ctx *log.Context) (bool, error) {
	_, err := updateRunCommandVersion(ctx)
	if err != nil {
		return false, errors.Wrap(err, "failed to install service")
	}

	return startService(ctx)
}

// Upgrades the RunCommand service with the latest available one
func UpgradeService(ctx *log.Context) (bool, error) {
	_, err := updateRunCommandVersion(ctx)
	if err != nil {
		return false, errors.Wrap(err, "failed to upgrade service")
	}

	_, err = exec.Command("systemctl", "start daemon-reload").Output()
	if err != nil {
		return false, errors.Wrap(err, "failed to reload service configuration")
	}

	return true, nil
}

// Stops and removes the installed service from the VM.
func RemoveService(ctx *log.Context) (bool, error) {
	_, err := stopService(ctx)
	if err != nil {
		return false, errors.Wrap(err, "failed to remove service")
	}

	err = os.Remove("/lib/systemd/system/" + systemdServiceName)
	if err != nil {
		return false, errors.Wrap(err, "failed to delete systemd configuration")
	}

	return true, nil
}

// Updates the version of RunCommand to execute.
// It will update the 'WorkingDirectory' and 'ExecStart' paths of the systemd configuration.
// If this is the first time the method is getting invoked, then it will create the config file with the required details.
// Subsequent calls will update the version of RunCommand to use.
func updateRunCommandVersion(ctx *log.Context) (bool, error) {
	runCommandVersion := "2.42.0"
	systemdConfig := fmt.Sprintf(serviceDetails, runCommandVersion)
	err := os.WriteFile("/lib/systemd/system/"+systemdServiceName, []byte(systemdConfig), 0666)
	if err != nil {
		return false, errors.Wrap(err, "failed to write systemd configuration for runcommand version: "+runCommandVersion)
	}

	return true, nil
}

// Starts the RunCommand service by invoking 'systemctl start'
func startService(ctx *log.Context) (bool, error) {
	_, err := exec.Command("systemctl", "start", systemdServiceName).Output()
	if err != nil {
		return false, errors.Wrap(err, "failed to start service")
	}

	return true, nil
}

// Stops the RunCommand service by invoking 'systemctl stop'
func stopService(ctx *log.Context) (bool, error) {
	_, err := exec.Command("systemctl", "stop", systemdServiceName).Output()
	if err != nil {
		return false, errors.Wrap(err, "failed to stop service")
	}

	return true, nil
}
