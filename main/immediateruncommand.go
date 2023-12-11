package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/pkg/errors"
)

func StartImmediateRunCommand() error {
	// Define the API endpoint
	api := "http://httpstat.us/200"

	// Create file for testing logs
	logFileName := "/var/log/azure/Microsoft.CPlat.Core.RunCommandHandlerLinux/ImmediateRunCommandLogs.txt"
	err := os.WriteFile(logFileName, []byte("Start immediate run command\r\n"), 0644)
	if err != nil {
		return errors.Wrap(err, "failed to create logs file for immediate run command")
	}

	f, _ := os.OpenFile(logFileName, os.O_APPEND|os.O_WRONLY, 0644)

	// Create an infinite loop
	for {
		fmt.Printf("Loop still running\r\n")
		resp, err := http.Get(api)
		t := time.Now()
		f.WriteString(fmt.Sprintf("%v: Ping to http://httpstat.us/200. Expected to see a 200 code\r\n", t))
		if err != nil {
			f.WriteString(fmt.Sprintf("An error occured: %v\r\n", err))
		} else {
			f.WriteString(fmt.Sprintf("Status code received: %v\r\n", resp.StatusCode))
			resp.Body.Close()
		}

		// Wait for 5 second before the next iteration
		time.Sleep(time.Second * 5)
	}
}
