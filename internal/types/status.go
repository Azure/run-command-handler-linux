package types

import "time"

// StatusReport contains one or more status items and is the parent object
type StatusReport []StatusItem

func NewStatusReport(statusType StatusType, operation string, message string) StatusReport {
	return []StatusItem{
		{
			Version:      1, // this is the protocol version do not change unless you are sure
			TimestampUTC: time.Now().UTC().Format(time.RFC3339),
			Status: Status{
				Operation: operation,
				Status:    statusType,
				FormattedMessage: FormattedMessage{
					Lang:    "en",
					Message: message},
			},
		},
	}
}

// StatusItem is used to serialize an individual part of the status read by the server
type StatusItem struct {
	Version      int    `json:"version"`
	TimestampUTC string `json:"timestampUTC"`
	Status       Status `json:"status"`
}

// StatusType reports the execution status
type StatusType string

const (
	// StatusTransitioning indicates the operation has begun but not yet completed
	StatusTransitioning StatusType = "transitioning"

	// StatusError indicates the operation failed
	StatusError StatusType = "error"

	// StatusSuccess indicates the operation succeeded
	StatusSuccess StatusType = "success"

	// StatusWarning indicates the operation was executed, but with one of the below conditions:
	// 1) Status files have been lost. So, exact execution status (error or success), output and error are not known.
	StatusWarning StatusType = "warning"
)

// Status is used for serializing status in a manner the server understands
type Status struct {
	Operation        string           `json:"operation"`
	Status           StatusType       `json:"status"`
	FormattedMessage FormattedMessage `json:"formattedMessage"`
}

// FormattedMessage is a struct used for serializing status
type FormattedMessage struct {
	Lang    string `json:"lang"`
	Message string `json:"message"`
}
