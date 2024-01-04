package systemd

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

const (
	systemctl              = "systemctl"
	systemctl_daemonreload = "daemon-reload"
	systemctl_disable      = "disable"
	systemctl_enable       = "enable"
	systemctl_isactive     = "is-active"
	systemctl_isenabled    = "is-enabled"
	systemctl_start        = "start"
	systemctl_status       = "status"
	systemctl_stop         = "stop"

	unitConfigurationBasePath_preferred   = "/lib/systemd/system"
	unitConfigurationBasePath_alternative = "/usr/lib/systemd/system"

	unitConfigurationFilePermission = 0644
)

type Manager struct {
}

func NewUnitManager() *Manager {
	return &Manager{}
}

func (mgr *Manager) StartUnit(unitName string, ctx *log.Context) error {
	ctx.Log("message", "running command to start unit")
	err := exec.Command(systemctl, systemctl_start, unitName).Run()
	return err
}

func (mgr *Manager) StopUnit(unitName string, ctx *log.Context) error {
	ctx.Log("message", "running command to stop unit")
	err := exec.Command(systemctl, systemctl_stop, unitName).Run()
	return err
}

func (mgr *Manager) EnableUnit(unitName string, ctx *log.Context) error {
	ctx.Log("message", "running command to enable unit")
	err := exec.Command(systemctl, systemctl_enable, unitName).Run()
	return err
}

func (mgr *Manager) DisableUnit(unitName string, ctx *log.Context) error {
	ctx.Log("message", "running command to disable unit")
	err := exec.Command(systemctl, systemctl_disable, unitName).Run()
	return err
}

func (mgr *Manager) DaemonReload(unitName string, ctx *log.Context) error {
	ctx.Log("message", "running command to reload daemon")
	err := exec.Command(systemctl, systemctl_daemonreload).Run()
	return err
}

func (mgr *Manager) IsUnitActive(unitName string, ctx *log.Context) error {
	ctx.Log("message", "running command to check if unit is active")
	err := exec.Command(systemctl, systemctl_isactive, unitName).Run()
	return err
}

func (mgr *Manager) IsUnitEnabled(unitName string, ctx *log.Context) (bool, error) {
	ctx.Log("message", "running command to check if unit is already enabled")
	output, err := exec.Command(systemctl, systemctl_isenabled, unitName).Output()
	sanitizedOutput := strings.Replace(string(output), "\n", "", -1)
	ctx.Log("message", fmt.Sprintf("%v %v output: %v", systemctl, systemctl_isenabled, sanitizedOutput))
	if sanitizedOutput == "enabled" {
		return true, nil
	} else if sanitizedOutput == "disabled" {
		return false, nil
	}

	return false, err
}

func (mgr *Manager) IsUnitInstalled(unitName string, ctx *log.Context) (bool, error) {
	filePath, err := GetUnitConfigurationFilePath(unitName, ctx)
	if err != nil {
		return false, err
	}

	ctx.Log("message", fmt.Sprintf("Checking if unit file %v exists to verify presence of installed service", filePath))
	_, err = os.Stat(filePath)

	if os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, errors.Wrap(err, "Error occurred while checking file existence")
	}

	return true, nil
}

func (*Manager) CreateUnitConfigurationFile(unitName string, content []byte, ctx *log.Context) error {
	unitConfigPath, err := GetUnitConfigurationFilePath(unitName, ctx)
	if err != nil {
		return err
	}

	ctx.Log("message", "creating unit configuration file in "+unitConfigPath)
	return os.WriteFile(unitConfigPath, content, unitConfigurationFilePermission)
}

func (*Manager) RemoveUnitConfigurationFile(unitName string, ctx *log.Context) error {
	unitConfigPath, err := GetUnitConfigurationFilePath(unitName, ctx)
	if err != nil {
		return err
	}

	ctx.Log("message", "removing unit configuration file from "+unitConfigPath)
	return os.Remove(unitConfigPath)
}

var GetUnitConfigurationFilePath = func(unitName string, ctx *log.Context) (string, error) {
	base_path, err := GetSystemDConfigurationBasePath(ctx)
	if err != nil {
		return "", err
	}
	return path.Join(base_path, unitName), nil
}
