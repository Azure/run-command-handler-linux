package types

import "time"

// StatusReport contains one or more status items and is the parent object
type StatusReport []StatusItem

func NewStatusReport(statusType StatusType, operation string, message string, extName string, optionalErrorCalrification ...string) StatusReport {
	errorClarificationName := "default"
	errorClarificationValue := "default"

	if len(optionalErrorCalrification) > 0 {
		errorClarificationName = optionalErrorCalrification[0]
		errorClarificationValue = optionalErrorCalrification[1]
	}

	return []StatusItem{
		{
			Version:      1, // this is the protocol version do not change unless you are sure
			TimestampUTC: time.Now().UTC().Format(time.RFC3339),
			Status: Status{
				Name:      extName,
				Operation: operation,
				Status:    statusType,
				FormattedMessage: FormattedMessage{
					Lang:    "en",
					Message: message},
			},
			SubStatus: &substastus{
				Name: errorClarificationName,
				Code: errorClarificationValue,
			},
		},
	}
}

// StatusItem is used to serialize an individual part of the status read by the server
type StatusItem struct {
	Version      int         `json:"version"`
	TimestampUTC string      `json:"timestampUTC"`
	Status       Status      `json:"status"`
	SubStatus    *substastus `json:"subStatus,omitempty"` // optional substatus, can be nil
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

	// StatusSkipped indicates the operation was skipped due to a precondition
	StatusSkipped StatusType = "skipped"
)

// Status is used for serializing status in a manner the server understands
type Status struct {
	Name             string           `json:"name"`
	Operation        string           `json:"operation"`
	Status           StatusType       `json:"status"`
	FormattedMessage FormattedMessage `json:"formattedMessage"`
}

// FormattedMessage is a struct used for serializing status
type FormattedMessage struct {
	Lang    string `json:"lang"`
	Message string `json:"message"`
}

// substatus used for serialization
type substastus struct {
	// Name is the name of the substatus
	Name string `json:"name"`
	// Code is the code of the substatus
	Code string `json:"code"`
}
