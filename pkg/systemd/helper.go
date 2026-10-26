package systemd

import (
	"fmt"
	"os"

	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

var statSystemdPath = os.Stat

func IsSystemDPresent() bool {
	_, err := os.Stat("/run/systemd/system")
	return err == nil
}

func GetSystemDConfigurationBasePath(ctx *log.Context) (string, error) {
	ctx.Log("message", "Getting systemd configuration path available in the system")
	info, err := statSystemdPath(unitConfigurationBasePath_preferred)
	if err != nil || info == nil || !info.IsDir() {
		ctx.Log("message", fmt.Sprintf("INFO: %s path was not found on the system", unitConfigurationBasePath_preferred))

		info, err = statSystemdPath(unitConfigurationBasePath_alternative)
		if err != nil || info == nil || !info.IsDir() {
			errorstring := fmt.Sprintf("ERROR: neither %s nor %s path was not found on the system", unitConfigurationBasePath_preferred, unitConfigurationBasePath_alternative)
			ctx.Log("message", errorstring)
			if err != nil {
				return "", errorutil.WithCode(errorutil.Systemd_failedToInspectUnitDirectory, errors.Wrap(err, errorstring))
			}
			return "", errorutil.WithCode(errorutil.Systemd_noUnitConfigurationDirectory, errors.New(errorstring))
		}

		ctx.Log("message", fmt.Sprintf("Alternative path was found on the system: %s", unitConfigurationBasePath_alternative))
		return unitConfigurationBasePath_alternative, nil
	}

	ctx.Log("message", fmt.Sprintf("Preferred path was found on the system: %s", unitConfigurationBasePath_preferred))
	return unitConfigurationBasePath_preferred, nil
}
