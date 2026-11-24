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
		{"https://%", errorutil.InvalidBlobUrl},
		{"ftp://a.blob.core.windows.net/c/b", errorutil.UnsupportedBlobUrlScheme},
		{"https://host/c/b", errorutil.MissingBlobUrlHost},
		{"https://a.file.core.windows.net/c/b", errorutil.InvalidBlobHost},
		{"https://a.blob./c/b", errorutil.InvalidStorageEndpoint},
		{"https://a.blob.core.windows.net", errorutil.MissingBlobContainerOrName},
	} {
		t.Run(tc.url, func(t *testing.T) {
			_, err := ParseBlobURL(tc.url)
			code, ok := errorutil.FindClarification(err)
			require.True(t, ok)
			require.Equal(t, tc.code, code)
		})
	}
}
