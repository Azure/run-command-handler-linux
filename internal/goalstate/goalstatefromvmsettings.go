package goalstate

import (
	"strings"

	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/hostgacommunicator"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

func GetImmediateRunCommandGoalStates(ctx *log.Context, communicator hostgacommunicator.IHostGACommunicator) ([]hostgacommunicator.ExtensionGoalStates, error) {
	vmSettings, err := communicator.GetImmediateVMSettings(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to retrieve VMSettings")
	}

	if vmSettings != nil {
		return filterImmediateRunCommandGoalStates(vmSettings.ExtensionGoalStates), nil
	}

	return []hostgacommunicator.ExtensionGoalStates{}, nil
}

func filterImmediateRunCommandGoalStates(extensionGoalStates []hostgacommunicator.ExtensionGoalStates) []hostgacommunicator.ExtensionGoalStates {
	var result []hostgacommunicator.ExtensionGoalStates
	for _, element := range extensionGoalStates {
		if isRunCommandGoalState(element) {
			result = append(result, element)
		}
	}
	return result
}

func isRunCommandGoalState(goalState hostgacommunicator.ExtensionGoalStates) bool {
	return strings.EqualFold(goalState.Name, constants.RunCommandExtensionName)
}
