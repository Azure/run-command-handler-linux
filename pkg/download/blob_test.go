package download

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/storage"
	"github.com/Azure/run-command-handler-linux/pkg/blobutil"
	"github.com/go-kit/kit/log"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

var (
	testctx = log.NewContext(log.NewNopLogger())
)

func Test_blobDownload_validateInputs(t *testing.T) {
	type sas interface {
		getURL() (string, error)
	}

	_, err := NewBlobDownload("", "", blobutil.AzureBlobRef{}).GetRequest()
	require.NotNil(t, err)
	errorMessage := err.Error()
	require.Contains(t, errorMessage, "failed to initialize azure storage client")
	require.Contains(t, errorMessage, "azure: account name is not valid")

	_, err = NewBlobDownload("account", "", blobutil.AzureBlobRef{}).GetRequest()
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "failed to initialize azure storage client")
	require.Contains(t, err.Error(), "account key required")

	_, err = NewBlobDownload("account", "Zm9vCg==", blobutil.AzureBlobRef{}).GetRequest()
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "failed to initialize azure storage client")

	_, err = NewBlobDownload("account", "Zm9vCg==", blobutil.AzureBlobRef{
		StorageBase: storage.DefaultBaseURL,
	}).GetRequest()
	require.Nil(t, err)
}

func Test_blobDownload_getURL(t *testing.T) {
	type sas interface {
		getURL() (string, error)
	}

	d := NewBlobDownload("account", "Zm9vCg==", blobutil.AzureBlobRef{
		StorageBase: "test.core.windows.net",
		Container:   "",
		Blob:        "blob.txt",
	})

	v, ok := d.(blobDownload)
	require.True(t, ok)

	url, err := v.getURL()
	require.Nil(t, err)
	require.Contains(t, url, "https://", "missing https scheme")
	require.Contains(t, url, "/account.blob.test.core.windows.net/", "missing/wrong host")
	require.Contains(t, url, "/$root/", "missing container in url")
	require.Contains(t, url, "/blob.txt", "missing blob name in url")
	for _, v := range []string{"sig", "se", "sr", "sp", "sv"} { // SAS query parameters
		require.Contains(t, url, v+"=", "missing SAS query '%s' in url", v)
	}
}

func Test_blobDownload_fails_badCreds(t *testing.T) {
	d := NewBlobDownload("example", "Zm9vCg==", blobutil.AzureBlobRef{
		StorageBase: storage.DefaultBaseURL,
		Blob:        "fooBlob.txt",
		Container:   "foocontainer",
	})

	mockResponse := &http.Response{
		StatusCode: http.StatusForbidden,
		Status:     "The chipmunks do not like you and thus returned a 403",
	}

	original := MakeHttpRequest
	defer func() { MakeHttpRequest = original }()

	MakeHttpRequest = func(*http.Request) (*http.Response, error) {
		return mockResponse, nil
	}

	status, _, err := Download(testctx, d)
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "Please verify the machine has network connectivity")
	require.Contains(t, err.Error(), "403")
	require.Equal(t, status, http.StatusForbidden)
}

// Tests that a common error message will be uniquely formatted
func Test_blobDownload_fails_badRequest(t *testing.T) {
	d := badRequestBlobDownload{
		blobDownload{"example", "Zm9vCg==", blobutil.AzureBlobRef{
			StorageBase: storage.DefaultBaseURL,
			Blob:        "fooBlob.txt",
			Container:   "foocontainer",
		}}}

	mockResponse := &http.Response{
		StatusCode: http.StatusBadRequest,
		Status:     "The chipmunks don't understand you. 400.",
	}

	original := MakeHttpRequest
	defer func() { MakeHttpRequest = original }()

	MakeHttpRequest = func(*http.Request) (*http.Response, error) {
		return mockResponse, nil
	}

	status, _, err := Download(testctx, d)
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "parts of the request were incorrectly formatted, missing, and/or invalid")
	require.Contains(t, err.Error(), "400")
	require.Equal(t, status, http.StatusBadRequest)
}

func Test_blobDownload_fails_urlNotFound(t *testing.T) {
	d := NewBlobDownload("accountname", "Zm9vCg==", blobutil.AzureBlobRef{
		StorageBase: ".example.com",
		Blob:        "fooBlob.txt",
		Container:   "foocontainer",
	})

	_, _, err := Download(testctx, d)
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "http request failed:")
}

func Test_GetSASBlob_actualBlob(t *testing.T) {
	sasURL := os.Getenv("AZURE_STORAGE_BLOB")
	sasToken := os.Getenv("SASTOKEN")
	if sasURL == "" || sasToken == "" {
		t.Skipf("Skipping: AZURE_STORAGE_BLOB or SASTOKEN not specified to run this test")
	}

	tmpDir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	defer os.RemoveAll(tmpDir)

	scriptFilePath, err := GetSASBlob(sasURL, sasToken, tmpDir)
	require.Nil(t, err)
	result, err := ioutil.ReadFile(scriptFilePath)
	require.Nil(t, err)
	require.True(t, len(result) > 0)
}

func Test_blobDownload_actualBlob(t *testing.T) {
	acct := os.Getenv("AZURE_STORAGE_ACCOUNT")
	key := os.Getenv("AZURE_STORAGE_ACCESS_KEY")
	if acct == "" || key == "" {
		t.Skipf("Skipping: AZURE_STORAGE_ACCOUNT or AZURE_STORAGE_ACCESS_KEY not specified to run this test")
	}
	base := storage.DefaultBaseURL

	// Create a blob first
	client, err := storage.NewClient(acct, key, base, storage.DefaultAPIVersion, true)
	require.Nil(t, err)
	blobStorageClient := client.GetBlobService()

	var (
		n         = 1024 * 64
		name      = "blob.txt"
		container = fmt.Sprintf("run-command-test-%d", rand.New(rand.NewSource(time.Now().UnixNano())).Int63())
		chunk     = make([]byte, n)
	)

	containerReference := blobStorageClient.GetContainerReference(container)
	_, err = containerReference.DeleteIfExists(nil)
	require.Nil(t, err)
	_, err = containerReference.CreateIfNotExists(&storage.CreateContainerOptions{Access: storage.ContainerAccessTypePrivate})
	require.Nil(t, err)
	defer containerReference.Delete(nil)
	blobReference := containerReference.GetBlobReference(name)
	require.Nil(t, blobReference.PutAppendBlob(nil))
	rand.Read(chunk)
	require.Nil(t, blobReference.AppendBlock(chunk, nil))

	// Get the blob via downloader
	d := NewBlobDownload(acct, key, blobutil.AzureBlobRef{
		Container:   container,
		Blob:        name,
		StorageBase: base,
	})
	_, body, err := Download(testctx, d)
	require.Nil(t, err)
	defer body.Close()
	b, err := ioutil.ReadAll(body)
	require.Nil(t, err)
	require.EqualValues(t, chunk, b, "retrieved body is different body=%d chunk=%d", len(b), len(chunk))
}

func Test_GetSASBlob_nestedDirectories_actualBlob(t *testing.T) {
    acct := os.Getenv("AZURE_STORAGE_ACCOUNT")
    key := os.Getenv("AZURE_STORAGE_ACCESS_KEY")
    if acct == "" || key == "" {
        t.Skipf("Skipping: AZURE_STORAGE_ACCOUNT or AZURE_STORAGE_ACCESS_KEY not specified to run this test")
    }
    base := storage.DefaultBaseURL

    // Create a blob in a nested directory structure
    client, err := storage.NewClient(acct, key, base, storage.DefaultAPIVersion, true)
    require.Nil(t, err)
    blobStorageClient := client.GetBlobService()

    var (
        content   = []byte("test script content\n")
        name      = "dir1/dir2/script.sh" // nested path
        container = fmt.Sprintf("run-command-test-%d", rand.New(rand.NewSource(time.Now().UnixNano())).Int63())
    )

    containerReference := blobStorageClient.GetContainerReference(container)
    _, err = containerReference.DeleteIfExists(nil)
    require.Nil(t, err)
    _, err = containerReference.CreateIfNotExists(&storage.CreateContainerOptions{Access: storage.ContainerAccessTypePrivate})
    require.Nil(t, err)
    defer containerReference.Delete(nil)
    
    blobReference := containerReference.GetBlobReference(name)
    require.Nil(t, blobReference.PutAppendBlob(nil))
    require.Nil(t, blobReference.AppendBlock(content, nil))

    // Generate SAS URL for the blob
    d := NewBlobDownload(acct, key, blobutil.AzureBlobRef{
        Container:   container,
        Blob:        name,
        StorageBase: base,
    })
    v, ok := d.(blobDownload)
    require.True(t, ok)
    sasURL, err := v.getURL()
    require.Nil(t, err)

    // Parse the SAS URL to separate URI from SAS token
    blobURI := fmt.Sprintf("https://%s.blob.%s/%s/%s", acct, base, container, name)
    sasToken := sasURL[len(blobURI):] // Everything after the base URI is the SAS token

    // Download the blob using GetSASBlob
    tmpDir, err := ioutil.TempDir("", "nested-test-")
    require.Nil(t, err)
    defer os.RemoveAll(tmpDir)

    scriptFilePath, err := GetSASBlob(blobURI, sasToken, tmpDir)
    require.Nil(t, err)

    // Verify that the file was downloaded with correct content
    result, err := ioutil.ReadFile(scriptFilePath)
    require.Nil(t, err)
    require.EqualValues(t, content, result, "downloaded content should match")

    // Verify that the file was created
    require.Contains(t, scriptFilePath, tmpDir, "file path should be in temp directory")
    
    // Verify the file exists and is not a directory
    fileInfo, err := os.Stat(scriptFilePath)
    require.Nil(t, err)
    require.False(t, fileInfo.IsDir(), "should be a file, not a directory")
}

func Test_blobAppend_actualBlob(t *testing.T) {
	// Before running the test locally prepare storage account and set the following env variables:
	// export AZURE_STORAGE_BLOB="https://atanas.blob.core.windows.net/con1/output5.txt"
	// export SASTOKEN="sp=racwdl&st=2021-03-16T23:27:57Z&se=2022-03-18T07:27:57Z&spr=https&sv=2020-02-10&sr=c&sig=2QiTjxoux2JO5kzYQGVzaqweiPHElKQKRZ3eV1n02v4%3D"

	blobURI := os.Getenv("AZURE_STORAGE_BLOB")
	sasToken := os.Getenv("SASTOKEN")
	if blobURI == "" || sasToken == "" {
		t.Skipf("Skipping: AZURE_STORAGE_BLOB or SASTOKEN not specified to run this test")
	}

	blobref, err := CreateOrReplaceAppendBlob(blobURI, sasToken)
	require.Nil(t, err)

	err = blobref.AppendBlock([]byte("First line\n"), nil)
	err = blobref.AppendBlock([]byte("Second line\n"), nil)
	err = blobref.AppendBlock([]byte("Third line\n"), nil)
	require.Nil(t, err)
}

type badRequestBlobDownload struct {
	blobDownloader blobDownload
}

func (b badRequestBlobDownload) GetRequest() (*http.Request, error) {
	url, err := b.blobDownloader.getURL()
	if err != nil {
		return nil, err
	}
	req, error := http.NewRequest("GET", url, nil)
	if req != nil {
		req.Header.Set(xMsClientRequestIdHeaderName, uuid.New().String())
		req.Header.Set("x-ms-version", "bad value")
	}
	return req, error
}
