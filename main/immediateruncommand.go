package main

import (
	"time"

	"github.com/Azure/run-command-handler-linux/internal/goalstate"
	"github.com/Azure/run-command-handler-linux/internal/hostgacommunicator"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

func StartImmediateRunCommand(ctx *log.Context) error {
	communicator := hostgacommunicator.HostGACommunicator{}
	ctx.Log("message", "Starting immediate run command")

	// Create an infinite loop
	for {
		goalStates, err := goalstate.GetImmediateRunCommandGoalStates(ctx, &communicator)
		if err != nil {
			return errors.Wrapf(err, "could not retrieve goal states for run command")
		}

		for _, el := range goalStates {
			goalstate.HandleGoalState(ctx, el)
		}

		// Wait for 2 minutes before the next iteration
		time.Sleep(time.Second * 120)
	}
}
