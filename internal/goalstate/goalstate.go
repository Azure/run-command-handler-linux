package goalstate

import (
	"encoding/json"

	"github.com/Azure/run-command-handler-linux/internal/hostgacommunicator"
	"github.com/go-kit/kit/log"
)

const enableCommand = "enable"

func HandleGoalState(ctx *log.Context, goalState hostgacommunicator.ExtensionGoalStates) error {
	for _, setting := range goalState.Settings {
		r, _ := json.Marshal(setting)
		ctx.Log("content", r)
	}
	// cmd, ok := cmds[enableCommand]

	// if !ok {
	// 	return errors.New("missing enable command")
	// }
	return nil
}
