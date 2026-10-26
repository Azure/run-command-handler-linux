package telemetry

import (
	"errors"
	"io"
	"testing"

	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/stretchr/testify/require"
)

type failingTelemetryWriter struct {
	writeErr error
	closeErr error
	short    bool
}

func (w failingTelemetryWriter) Write(b []byte) (int, error) {
	if w.short {
		return 0, nil
	}
	return len(b), w.writeErr
}
func (w failingTelemetryWriter) Close() error { return w.closeErr }

func TestTelemetryClarifications(t *testing.T) {
	originalOpen, originalRename := openTelemetryFile, renameTelemetryFile
	t.Cleanup(func() { openTelemetryFile, renameTelemetryFile = originalOpen, originalRename })
	cause := errors.New("machine failure")
	for _, tc := range []struct {
		code   int
		writer failingTelemetryWriter
		open   error
		rename error
	}{
		{code: errorutil.Os_FailedToOpenTelemetryFile, open: cause},
		{code: errorutil.Os_FailedToWriteTelemetryFile, writer: failingTelemetryWriter{writeErr: cause}},
		{code: errorutil.Os_ShortTelemetryFileWrite, writer: failingTelemetryWriter{short: true}},
		{code: errorutil.Os_FailedToPublishTelemetryFile, rename: cause},
		{code: errorutil.Os_FailedToCloseTelemetryFile, writer: failingTelemetryWriter{closeErr: cause}},
	} {
		t.Run(string(rune(tc.code)), func(t *testing.T) {
			openTelemetryFile = func(string) (io.WriteCloser, error) { return tc.writer, tc.open }
			renameTelemetryFile = func(string, string) error { return tc.rename }
			w := telemetryEventWriter{}
			_, err := w.Write([]byte("event"))
			if tc.code == errorutil.Os_FailedToCloseTelemetryFile {
				require.NoError(t, err)
				err = w.Close()
			}
			code, ok := errorutil.Code(err)
			require.True(t, ok)
			require.Equal(t, tc.code, code)
			if tc.code == errorutil.Os_ShortTelemetryFileWrite {
				require.ErrorIs(t, err, io.ErrShortWrite)
			} else {
				require.ErrorIs(t, err, cause)
			}
		})
	}
	for _, tc := range []struct {
		code   int
		event  telemetryEvent
		writer failingTelemetryWriter
	}{
		{code: errorutil.Os_FailedToCloseTelemetryWriter, writer: failingTelemetryWriter{closeErr: cause}},
		{code: errorutil.Internal_failedToMarshalTelemetryEvent, event: telemetryEvent{Parameters: []interface{}{make(chan int)}}},
		{code: errorutil.Os_FailedToWriteTelemetryEvent, writer: failingTelemetryWriter{writeErr: cause}},
		{code: errorutil.Os_ShortTelemetryEventWrite, writer: failingTelemetryWriter{short: true}},
	} {
		s := newTelemetryEventSenderWithWriteCloser(tc.writer)
		err := s.send(tc.event)
		code, ok := errorutil.Code(err)
		require.True(t, ok)
		require.Equal(t, tc.code, code)
	}
	// The writer's primary error must not be overwritten by a close failure.
	s := newTelemetryEventSenderWithWriteCloser(failingTelemetryWriter{writeErr: cause, closeErr: io.EOF})
	err := s.send(telemetryEvent{})
	require.ErrorIs(t, err, cause)
	code, ok := errorutil.Code(err)
	require.True(t, ok)
	require.Equal(t, errorutil.Os_FailedToWriteTelemetryEvent, code)
}
