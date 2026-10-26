package blobutil

import (
	"testing"

	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/stretchr/testify/require"
)

func TestParseBlobURLClarifications(t *testing.T) {
	for _, tc := range []struct {
		url  string
		code int
	}{
		{"https://%", errorutil.CustomerInput_invalidBlobUrl},
		{"ftp://a.blob.core.windows.net/c/b", errorutil.CustomerInput_unsupportedBlobUrlScheme},
		{"https://host/c/b", errorutil.CustomerInput_missingBlobUrlHost},
		{"https://a.file.core.windows.net/c/b", errorutil.CustomerInput_invalidBlobHost},
		{"https://a.blob./c/b", errorutil.CustomerInput_invalidStorageEndpoint},
		{"https://a.blob.core.windows.net", errorutil.CustomerInput_missingBlobContainerOrName},
	} {
		t.Run(tc.url, func(t *testing.T) {
			_, err := ParseBlobURL(tc.url)
			code, ok := errorutil.Code(err)
			require.True(t, ok)
			require.Equal(t, tc.code, code)
		})
	}
}
