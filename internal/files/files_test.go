package files

import (
	"fmt"
	"io/ioutil"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/Azure/run-command-handler-linux/pkg/download"
	"github.com/ahmetalpbalkan/go-httpbin"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

var mockManagedIdentityBoth = handlersettings.RunCommandManagedIdentity{
	ClientId: "5d784f90-d7d9-4b04-bdf1-4ae4824d55b0",
	ObjectId: "bed99fe3-1ad3-4a25-867d-7d48d68def6a",
}

var mockManagedIdentityClientId = handlersettings.RunCommandManagedIdentity{
	ClientId: "5d784f90-d7d9-4b04-bdf1-4ae4824d55b0",
}

var mockManagedIdentityObjectId = handlersettings.RunCommandManagedIdentity{
	ObjectId: "bed99fe3-1ad3-4a25-867d-7d48d68def6a",
}

var mockManagedSystemIdentity = handlersettings.RunCommandManagedIdentity{}

func Test_getDownloaders_externalUrl(t *testing.T) {
	download.MockReturnErrorForMockMsiDownloader = true
	var mockMsiDownloder = download.MockMsiDownloader{}

	// Case 0: Error getting Msi. It returns public URL downloader
	d, err := getDownloaders("http://acct.blob.core.windows.net/", &mockManagedIdentityObjectId, mockMsiDownloder)
	require.Nil(t, err)
	require.NotNil(t, d)
	require.NotEmpty(t, d)
	require.Equal(t, 1, len(d))
	require.Equal(t, "download.urlDownload", fmt.Sprintf("%T", d[0]), "got wrong type")

	// Case 1: Valid Msi returned. It returns both MSI downloader and public URL downloader. First downloader is MSI downloader
	download.MockReturnErrorForMockMsiDownloader = false
	d, err = getDownloaders("http://acct.blob.core.windows.net/", &mockManagedIdentityClientId, mockMsiDownloder)
	require.Nil(t, err)
	require.NotNil(t, d)
	require.Equal(t, 2, len(d))
	require.Equal(t, "*download.blobWithMsiToken", fmt.Sprintf("%T", d[0]), "got wrong type")

	download.MockReturnErrorForMockMsiDownloader = false
}

func Test_getDownloaders_SystemIdentityVersusByClientIdOrObjectId(t *testing.T) {
	download.MockReturnErrorForMockMsiDownloader = true
	var mockMsiDownloder = download.MockMsiDownloader{}

	// Case 0: Provide both clientId and ObjectId getting Msi.
	d, err := getDownloaders("http://acct.blob.core.windows.net/", &mockManagedIdentityBoth, mockMsiDownloder)
	require.NotNil(t, err)
	require.Equal(t, err.Error(), "Use either ClientId or ObjectId for managed identity. Not both.")

	download.MockReturnErrorForMockMsiDownloader = false

	// Case 1: Valid Msi returned by system identity. It returns both MSI downloader and public URL downloader. First downloader is MSI downloader
	d, err = getDownloaders("http://acct.blob.core.windows.net/", &mockManagedSystemIdentity, mockMsiDownloder)
	require.Nil(t, err)
	require.NotNil(t, d)
	require.Equal(t, 2, len(d))
	require.Equal(t, "*download.blobWithMsiToken", fmt.Sprintf("%T", d[0]), "got wrong type")

	// Case 2: Valid Msi returned by system identity - nil identity passed. It returns both MSI downloader and public URL downloader. First downloader is MSI downloader
	d, err = getDownloaders("http://acct.blob.core.windows.net/", nil, mockMsiDownloder)
	require.Nil(t, err)
	require.NotNil(t, d)
	require.Equal(t, 2, len(d))
	require.Equal(t, "*download.blobWithMsiToken", fmt.Sprintf("%T", d[0]), "got wrong type")

	// Case 3: Valid Msi returned by clientId.  It returns both MSI downloader and public URL downloader. First downloader is MSI downloader
	d, err = getDownloaders("http://acct.blob.core.windows.net/", &mockManagedIdentityClientId, mockMsiDownloder)
	require.Nil(t, err)
	require.NotNil(t, d)
	require.Equal(t, 2, len(d))
	require.Equal(t, "*download.blobWithMsiToken", fmt.Sprintf("%T", d[0]), "got wrong type")

	// Case 4: Valid Msi returned by clientId.  It returns both MSI downloader and public URL downloader. First downloader is MSI downloader
	d, err = getDownloaders("http://acct.blob.core.windows.net/", &mockManagedIdentityObjectId, mockMsiDownloder)
	require.Nil(t, err)
	require.NotNil(t, d)
	require.Equal(t, 2, len(d))
	require.Equal(t, "*download.blobWithMsiToken", fmt.Sprintf("%T", d[0]), "got wrong type")
}

func Test_urlToFileName_badURL(t *testing.T) {
	_, err := UrlToFileName("http://192.168.0.%31/")
	require.NotNil(t, err)
	require.Contains(t, err.Error(), `unable to parse URL: "http://192.168.0.%31/"`)
}

func Test_urlToFileName_noFileName(t *testing.T) {
	cases := []string{
		"http://example.com",
		"http://example.com",
		"http://example.com/",
		"http://example.com/#foo",
		"http://example.com?bar",
		"http://example.com/bar/",  // empty after last slash
		"http://example.com/bar//", // empty after last slash
		"http://example.com/?bar",
		"http://example.com/?bar#quux",
	}

	for _, c := range cases {
		_, err := UrlToFileName(c)
		require.NotNil(t, err, "not failed: %s", "url=%s", c)
		require.Contains(t, err.Error(), "cannot extract file name from URL", "url=%s", c)
	}
}

func Test_urlToFileName(t *testing.T) {
	cases := []struct{ in, out string }{
		{"http://example.com/1", "1"},
		{"http://example.com/1/2", "2"},
		{"http://example.com/1///2", "2"},
		{"http://example.com/1/2?3=4", "2"},
		{"http://example.com/1/2?3#", "2"},
	}
	for _, c := range cases {
		fn, err := UrlToFileName(c.in)
		require.Nil(t, err, "url=%s")
		require.Equal(t, c.out, fn, "url=%s", c)
	}
}

func Test_postProcessFile_fail(t *testing.T) {
	require.NotNil(t, PostProcessFile("/non/existing/path"))
}

func Test_postProcessFile(t *testing.T) {
	f, err := ioutil.TempFile("", "")
	require.Nil(t, err)
	defer os.RemoveAll(f.Name())
	_, err = fmt.Fprintf(f, "#!/bin/sh\r\necho 'Hello, world!'\n")
	require.Nil(t, err)
	f.Close()

	require.Nil(t, PostProcessFile(f.Name()))

	b, err := ioutil.ReadFile(f.Name())
	require.Nil(t, err)
	require.Equal(t, []byte("#!/bin/sh\necho 'Hello, world!'\n"), b)
}

func Test_downloadAndProcessURL(t *testing.T) {
	srv := httptest.NewServer(httpbin.GetMux())
	defer srv.Close()

	tmpDir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	defer os.RemoveAll(tmpDir)

	cfg := handlersettings.HandlerSettings{PublicSettings: handlersettings.PublicSettings{}, ProtectedSettings: handlersettings.ProtectedSettings{}}
	downloadedFilePath, err := DownloadAndProcessURL(log.NewContext(log.NewNopLogger()), srv.URL+"/bytes/256", tmpDir, &cfg)
	require.Nil(t, err)

	fp := filepath.Join(tmpDir, "256")
	require.Equal(t, fp, downloadedFilePath)
	fi, err := os.Stat(fp)
	require.Nil(t, err)
	require.EqualValues(t, 256, fi.Size())
	require.Equal(t, os.FileMode(0500).String(), fi.Mode().String())
}

func Test_saveScriptFile(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	defer os.RemoveAll(tmpDir)

	var content = "echo"
	var filePath = filepath.Join(tmpDir, "script.sh")
	err = SaveScriptFile(filePath, content)
	require.Nil(t, err)
	result, err := ioutil.ReadFile(filePath)
	require.Nil(t, err)
	require.Equal(t, content, string(result))
}
