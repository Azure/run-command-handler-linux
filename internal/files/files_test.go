package files

import (
	"fmt"
	"io/ioutil"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Azure/azure-extension-foundation/msi"
	"github.com/Azure/azure-extension-platform/vmextension"
	"github.com/Azure/run-command-handler-linux/internal/constants"
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
	fmt.Println(err.Error())
	require.NotNil(t, err)
	require.Equal(t, err.Error(), "use either ClientId or ObjectId for managed identity. Not both")

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
	VerifyErrorClarification(t, constants.FileDownload_UnableToParseFileName, err)
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
		VerifyErrorClarification(t, constants.FileDownload_CannotExtractFileNameFromUrl, err)
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
	err := PostProcessFile("/non/existing/path")
	VerifyErrorClarification(t, constants.Internal_FailedToOpenFileForReading, err)
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

func Test_downloadAndProcessScript(t *testing.T) {
	srv := httptest.NewServer(httpbin.GetMux())
	defer srv.Close()

	tmpDir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	defer os.RemoveAll(tmpDir)

	cfg := handlersettings.HandlerSettings{PublicSettings: handlersettings.PublicSettings{}, ProtectedSettings: handlersettings.ProtectedSettings{}}
	downloadedFilePath, err := DownloadAndProcessScript(log.NewContext(log.NewNopLogger()), srv.URL+"/bytes/256", tmpDir, &cfg)
	require.Nil(t, err)

	fp := filepath.Join(tmpDir, "256")
	require.Equal(t, fp, downloadedFilePath)
	fi, err := os.Stat(fp)
	require.Nil(t, err)
	require.EqualValues(t, 256, fi.Size())
	require.Equal(t, os.FileMode(0500).String(), fi.Mode().String())
}

func Test_downloadAndProcessArtifact(t *testing.T) {
	srv := httptest.NewServer(httpbin.GetMux())
	defer srv.Close()

	tmpDir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	defer os.RemoveAll(tmpDir)

	// FileName is specified
	artifact := handlersettings.UnifiedArtifact{
		ArtifactId:  1,
		ArtifactUri: srv.URL + "/bytes/256",
		FileName:    "iggy.txt",
	}
	downloadedFilePath, err := DownloadAndProcessArtifact(log.NewContext(log.NewNopLogger()), tmpDir, &artifact)
	require.Nil(t, err)

	fp := filepath.Join(tmpDir, "iggy.txt")
	require.Equal(t, fp, downloadedFilePath)
	fi, err := os.Stat(fp)
	require.Nil(t, err)
	require.EqualValues(t, 256, fi.Size())
	require.Equal(t, os.FileMode(0500).String(), fi.Mode().String())

	// FileName is not specified
	artifact = handlersettings.UnifiedArtifact{
		ArtifactId:  3,
		ArtifactUri: srv.URL + "/bytes/256",
	}
	downloadedFilePath, err = DownloadAndProcessArtifact(log.NewContext(log.NewNopLogger()), tmpDir, &artifact)
	require.Nil(t, err)

	fp = filepath.Join(tmpDir, "Artifact3")
	require.Equal(t, fp, downloadedFilePath)
	fi, err = os.Stat(fp)
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

func TestGetDownloaders_NonBlobURL_ReturnsPublicOnly(t *testing.T) {
	publicURL := "https://example.com/scripts/a.sh"

	mock := &mockMsiDownloader{providerToReturn: providerSuccess()}
	downloaders, err := getDownloaders(publicURL, nil, mock)

	require.Nil(t, err)
	require.Len(t, downloaders, 1, "non-blob URL must return only public downloader")
	require.Equal(t, 0, mock.calledGet+mock.calledByClientID+mock.calledByObjectID,
		"msi downloader must not be used for non-blob URL")
}

func TestGetDownloaders_EmptyURL_ReturnsClarification(t *testing.T) {
	dl, err := getDownloaders("", nil, &mockMsiDownloader{providerToReturn: providerSuccess()})
	require.Nil(t, dl)
	VerifyErrorClarification(t, constants.FileDownload_Empty, err)
}

func VerifyErrorClarification(t *testing.T, expectedCode int, ewc *vmextension.ErrorWithClarification) {
	require.NotNil(t, ewc, "No error returned when one was expected")
	require.Equal(t, expectedCode, ewc.ErrorCode, "Expected error %d but received %d", expectedCode, ewc.ErrorCode)
}

func TestGetDownloaders_BlobURL_BothClientAndObjectID_ReturnsClarification(t *testing.T) {
	blobURL := "https://acct.blob.core.windows.net/container/blob.txt"

	mi := &handlersettings.RunCommandManagedIdentity{
		ClientId: "11111111-1111-1111-1111-111111111111",
		ObjectId: "22222222-2222-2222-2222-222222222222",
	}

	mock := &mockMsiDownloader{providerToReturn: providerSuccess()}
	downloaders, err := getDownloaders(blobURL, mi, mock)

	require.Nil(t, downloaders)
	VerifyErrorClarification(t, constants.CustomerInput_ClientIdObjectIdBothSpecified, err)
}

// MsiProvider is invoked as: _, err := msiProvider()
type mockMsiDownloader struct {
	calledGet        int
	calledByClientID int
	calledByObjectID int
	lastURL          string
	lastClientID     string
	lastObjectID     string
	providerToReturn download.MsiProvider
}

func (m *mockMsiDownloader) GetMsiProvider(url string) download.MsiProvider {
	m.calledGet++
	m.lastURL = url
	return m.providerToReturn
}

func (m *mockMsiDownloader) GetMsiProviderByClientId(url, clientId string) download.MsiProvider {
	m.calledByClientID++
	m.lastURL = url
	m.lastClientID = clientId
	return m.providerToReturn
}

func (m *mockMsiDownloader) GetMsiProviderByObjectId(url, objectId string) download.MsiProvider {
	m.calledByObjectID++
	m.lastURL = url
	m.lastObjectID = objectId
	return m.providerToReturn
}

func providerSuccess() download.MsiProvider {
	return func() (msi.Msi, error) { return msi.Msi{}, nil }
}
