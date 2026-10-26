package preprocess

import (
	"errors"
	"io"
	"testing"

	"github.com/Azure/azure-extension-platform/vmextension"
	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/stretchr/testify/require"
)

type failingTextReader struct{ err error }

func (r failingTextReader) Read([]byte) (int, error) { return 0, r.err }
func (r failingTextReader) Close() error             { return nil }

func TestTextFileClarifications(t *testing.T) {
	original := openTextFile
	t.Cleanup(func() { openTextFile = original })
	for _, code := range []int{errorutil.Os_FailedToOpenTextFile, errorutil.Os_FailedToReadTextFile} {
		pointer := vmextension.NewErrorWithClarificationPtr(1998, errors.New("injected classified file failure"))
		for _, cause := range []error{errors.New("injected file failure"), pointer, *pointer} {
			expected := code
			if _, ok := errorutil.Code(cause); ok {
				expected = 1998
			}
			openTextFile = func(string) (io.ReadCloser, error) {
				if code == errorutil.Os_FailedToOpenTextFile {
					return nil, cause
				}
				return failingTextReader{cause}, nil
			}
			_, err := IsTextFile("script")
			actual, ok := errorutil.Code(err)
			require.True(t, ok)
			require.Equal(t, expected, actual)
			require.True(t, errors.Is(err, cause))
		}
	}
}
