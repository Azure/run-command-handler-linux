package types

import "encoding/json"

// ExecutionState represents script current execution state
type ExecutionState string

const (
	// Unknown state (default value)
	Unknown ExecutionState = "Unknown"

	// Pending script execution
	Pending ExecutionState = "Pending"

	// Running script state
	Running ExecutionState = "Running"

	// Failed to execute script
	Failed = "Failed"

	// Succeeded state when successfully completed the script execution
	Succeeded = "Succeeded"

	// TimedOut state when time timit is reached and scrip has not completed yet
	TimedOut = "TimedOut"

	// Canceled state when customer canceled the script execution
	Canceled = "Canceled"
)

// RunCommandInstanceView reports script execution status
type RunCommandInstanceView struct {
	ExecutionState   ExecutionState `json:"executionState"`
	ExecutionMessage string         `json:"executionMessage"`
	Output           string         `json:"output"`
	Error            string         `json:"error"`
	ExitCode         int            `json:"exitCode"`
	StartTime        string         `json:"startTime"`
	EndTime          string         `json:"endTime"`
}

func (instanceView RunCommandInstanceView) Marshal() ([]byte, error) {
	return json.Marshal(instanceView)
}

func IsImmediateGoalStateInTerminalState(s Status) bool {
	return s.Status == Failed ||
		s.Status == Succeeded ||
		s.Status == TimedOut ||
		s.Status == Canceled
}
