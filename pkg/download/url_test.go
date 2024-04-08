package download

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_urlDownload_GetRequest_badURL(t *testing.T) {
	// http.NewRequest will not fail with most URLs, such as
	// containing spaces, relative URLs by design. So testing a
	// misencoded URL here.

	u := "http://[fe80::1%en0]/a.txt"
	d := NewURLDownload(u)
	r, err := d.GetRequest()
	require.NotNil(t, err, u)
	require.Contains(t, err.Error(), "invalid URL", u)
	require.Nil(t, r, u)
}

func Test_urlDownload_GetRequest_goodURL(t *testing.T) {
	u := "http://example.com/a.txt"
	d := NewURLDownload(u)
	r, err := d.GetRequest()
	require.Nil(t, err, u)
	require.NotNil(t, r, u)
	require.NotNil(t, r.Header.Get(xMsClientRequestIdHeaderName))
}

func Test_GetUriForLogging_ScrubsQuery(t *testing.T) {
	uri := "http://example.com/a.txt"
	scrubbedUri := GetUriForLogging(uri)
	require.Equal(t, uri, scrubbedUri)

	uri = "https://samplest.blob.core.windows.net/samplecontainer/HelloWorld.sh?sp=r&st=2023-01-21T00:19:32Z&se=2023-01-21T08:19:32Z&spr=https&sv=2021-06-08&sr=b"
	expectedScrubbedUri := "https://samplest.blob.core.windows.net/samplecontainer/HelloWorld.sh"
	scrubbedUri = GetUriForLogging(uri)
	require.Equal(t, expectedScrubbedUri, scrubbedUri)

	uri = ""
	scrubbedUri = GetUriForLogging(uri)
	require.Equal(t, uri, scrubbedUri)

	// random string for Uri
	uri = "jweu^^&*)^&*(^w0q485"
	scrubbedUri = GetUriForLogging(uri)
	require.Equal(t, "://jweu^^&*)^&*(^w0q485", scrubbedUri)
}
