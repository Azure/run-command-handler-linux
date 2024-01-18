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
	go startAsync(ctx, setting, done)
	select {
	case <-done:
		ctx.Log("message", "goal state successfully finished")
	case <-time.After(time.Minute * time.Duration(maxExecutionTimeInMinutes)):
		return errors.New("timeout when trying to execute goal state")
	}
	return nil
}

func startAsync(ctx *log.Context, setting settings.SettingsCommon, done chan bool) {
	r, _ := json.Marshal(setting)
	ctx.Log("content", r)

	rand.Seed(time.Now().UnixNano())
	randomInt := rand.Intn(10)
	ctx.Log("report", fmt.Sprintf("sleeping for %v seconds", randomInt))
	time.Sleep(time.Minute * time.Duration(randomInt))

	// cmd, ok := cmds[enableCommand]

	// if !ok {
	// 	return errors.New("missing enable command")
	// }
	// ctx.Log("message", "done sleeping")
	done <- true
}
