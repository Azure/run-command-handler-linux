package urlutil

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Azure/azure-extension-platform/vmextension"
	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/stretchr/testify/require"
)

func TestRemoveURLClarification(t *testing.T) {
	require.Nil(t, RemoveUrlFromErr(nil))
	cause := errors.New(`Get "https://example.invalid/script?sig=secret": failed`)
	err := RemoveUrlFromErr(cause)
	code, ok := errorutil.Code(err)
	require.True(t, ok)
	require.Equal(t, errorutil.FileDownload_redactedUrlError, code)
	require.NotContains(t, err.Error(), "secret")
	require.True(t, errors.Is(err, cause))
}

func TestRemoveURLPreservesClassifications(t *testing.T) {
	pointer := vmextension.NewErrorWithClarificationPtr(1998, errors.New("https://example.invalid/?sig=secret failed"))
	for _, classified := range []error{pointer, *pointer} {
		err := RemoveUrlFromErr(fmt.Errorf("wrapped: %w", classified))
		code, ok := errorutil.Code(err)
		require.True(t, ok)
		require.Equal(t, 1998, code)
		require.NotContains(t, err.Error(), "secret")
		require.True(t, errors.Is(err, classified))
	}
}
