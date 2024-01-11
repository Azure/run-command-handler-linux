package goalstate

import (
	"github.com/Azure/run-command-handler-linux/internal/hostgacommunicator"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

const runCommandExtensionName = "Microsoft.CPlat.Core.RunCommandHandlerLinux"

func GetImmediateRunCommandGoalStates(ctx *log.Context, communicator hostgacommunicator.IHostGACommunicator) ([]hostgacommunicator.ExtensionGoalStates, error) {
	vmSettings, err := communicator.GetImmediateVMSettings(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to retrieve VMSettings")
	}

	extensionGoalStates := filterImmediateRunCommandGoalStates(vmSettings.ExtensionGoalStates)
	return extensionGoalStates, nil
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
	return goalState.Name == runCommandExtensionName
}
