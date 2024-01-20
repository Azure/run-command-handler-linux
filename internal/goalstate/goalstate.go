package goalstate

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"github.com/Azure/run-command-handler-linux/internal/settings"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

const (
	enableCommand             string = "enable"
	maxExecutionTimeInMinutes int32  = 90
)

func HandleGoalState(ctx *log.Context, setting settings.SettingsCommon) error {
	done := make(chan bool)
	err := make(chan error)
	go startAsync(ctx, setting, done, err)
	select {
	case <-err:
		return errors.Wrapf(<-err, "error when trying to execute goal state")
	case <-done:
		ctx.Log("message", "goal state successfully finished")
	case <-time.After(time.Minute * time.Duration(maxExecutionTimeInMinutes)):
		return errors.New("timeout when trying to execute goal state")
	}
	return nil
}

func startAsync(ctx *log.Context, setting settings.SettingsCommon, done chan bool, err chan error) {
	r, _ := json.Marshal(setting)
	ctx.Log("content", r)

	rand.Seed(time.Now().UnixNano())
	randomInt := rand.Intn(10)
	ctx.Log("report", fmt.Sprintf("sleeping for %v seconds", randomInt))
	time.Sleep(time.Minute * time.Duration(randomInt))
	ctx.Log("message", "done sleeping")

	// cmd, ok := commands.Cmds[enableCommand]
	// if !ok {
	// 	err <- errors.New("missing enable command")
	// 	return
	// }

	done <- true
}
