package telemetry

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"time"

	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/pkg/errors"
)

const (
	telemetryEventsPath = constants.WaAgentDirectory + "/events"
)

type telemetryParameterString struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type telemetryParameterLong struct {
	Name  string `json:"name"`
	Value int64  `json:"value"`
}

type telemetryParameterBool struct {
	Name  string `json:"name"`
	Value bool   `json:"value"`
}

type telemetryEvent struct {
	EventID    int           `json:"eventId"`
	ProviderID string        `json:"providerId"`
	Parameters []interface{} `json:"parameters"`
}

type telemetryEventWriter struct {
	fh io.WriteCloser
}

var openTelemetryFile = func(name string) (io.WriteCloser, error) {
	return os.OpenFile(name, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0400)
}

var renameTelemetryFile = os.Rename

func (w *telemetryEventWriter) Write(bs []byte) (n int, err error) {
	fn := getTelemetryFileName()
	temp := fn + ".tmp"

	fh, err := openTelemetryFile(temp)
	if err != nil {
		return 0, errorutil.WithCode(errorutil.Os_FailedToOpenTelemetryFile, errors.Wrap(err, "failed to open telemetry file"))
	}

	w.fh = fh
	n, err = w.fh.Write(bs)
	if err != nil {
		return n, errorutil.WithCode(errorutil.Os_FailedToWriteTelemetryFile, errors.Wrap(err, "failed to write telemetry file"))
	}
	if n != len(bs) {
		return n, errorutil.WithCode(errorutil.Os_ShortTelemetryFileWrite, io.ErrShortWrite)
	}
	err = errorutil.WithCode(errorutil.Os_FailedToPublishTelemetryFile, errors.Wrap(renameTelemetryFile(temp, fn), "failed to publish telemetry file"))

	return
}

func (w *telemetryEventWriter) Close() (err error) {
	if w.fh != nil {
		err = errorutil.WithCode(errorutil.Os_FailedToCloseTelemetryFile, errors.Wrap(w.fh.Close(), "failed to close telemetry file"))
		w.fh = nil
	}
	return
}

type telemetryEventSender struct {
	writer io.WriteCloser
}

func NewTelemetryEventSender() *telemetryEventSender {
	return newTelemetryEventSenderWithWriteCloser(&telemetryEventWriter{})
}

func SendTelemetry(sender *telemetryEventSender, name, version string) func(operation, message string, isSuccess bool, duration time.Duration) error {
	return func(operation, message string, isSuccess bool, duration time.Duration) error {
		e := newTelemetryEvent(name, version, operation, message, isSuccess, duration)
		return sender.send(e)
	}
}

func newTelemetryEventSenderWithWriteCloser(writer io.WriteCloser) *telemetryEventSender {
	return &telemetryEventSender{writer: writer}
}

func (w *telemetryEventSender) send(e telemetryEvent) (result error) {
	defer func() {
		if closeErr := w.writer.Close(); result == nil {
			result = errorutil.WithCode(errorutil.Os_FailedToCloseTelemetryWriter, errors.Wrap(closeErr, "failed to close telemetry event writer"))
		}
	}()

	bs, err := json.Marshal(e)
	if err != nil {
		return errorutil.WithCode(errorutil.Internal_failedToMarshalTelemetryEvent, errors.Wrap(err, "failed to marshal telemetry event"))
	}

	n, err := w.writer.Write(bs)
	if err != nil {
		return errorutil.WithCode(errorutil.Os_FailedToWriteTelemetryEvent, errors.Wrap(err, "failed to write telemetry event"))
	}
	if n != len(bs) {
		return errorutil.WithCode(errorutil.Os_ShortTelemetryEventWrite, io.ErrShortWrite)
	}

	return nil
}

func getTelemetryFileName() string {
	fn := fmt.Sprintf("%d.tld", time.Now().UnixNano())
	return path.Join(telemetryEventsPath, fn)
}

func newTelemetryEvent(name, version, operation, message string, isSuccess bool, duration time.Duration) telemetryEvent {
	return telemetryEvent{
		EventID:    1,
		ProviderID: "69B669B9-4AF8-4C50-BDC4-6006FA76E975",
		Parameters: []interface{}{
			telemetryParameterString{
				Name:  "Name",
				Value: name,
			},
			telemetryParameterString{
				Name:  "Version",
				Value: version,
			},
			telemetryParameterString{
				Name:  "Operation",
				Value: operation,
			},
			telemetryParameterBool{
				Name:  "OperationSuccess",
				Value: isSuccess,
			},
			telemetryParameterString{
				Name:  "Message",
				Value: message,
			},
			telemetryParameterLong{
				Name:  "Duration",
				Value: duration.Nanoseconds() / 1e6,
			},
		},
	}
}
