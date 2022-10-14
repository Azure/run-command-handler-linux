package download

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/storage"
	"github.com/Azure/run-command-handler-linux/pkg/blobutil"
	"github.com/pkg/errors"
)

const (
	// blobSASDuration describes the duration for which the generated
	// Shared Access Signature for the blob is valid.
	blobSASDuration = time.Minute * 30
)

type blobDownload struct {
	accountName, accountKey string
	blob                    blobutil.AzureBlobRef
}

func (b blobDownload) GetRequest() (*http.Request, error) {
	url, err := b.getURL()
	if err != nil {
		return nil, err
	}
	return http.NewRequest("GET", url, nil)
}

// getURL returns publicly downloadable URL of the Azure Blob
// by generating a URL with a temporary Shared Access Signature.
func (b blobDownload) getURL() (string, error) {
	client, err := storage.NewClient(b.accountName, b.accountKey,
		b.blob.StorageBase, storage.DefaultAPIVersion, true)
	if err != nil {
		return "", errors.Wrap(err, "failed to initialize azure storage client")
	}

	// get read-only
	blobStorageClient := client.GetBlobService()
	container := blobStorageClient.GetContainerReference(b.blob.Container)
	blob := container.GetBlobReference(b.blob.Blob)
	options := storage.BlobSASOptions{
		BlobServiceSASPermissions: storage.BlobServiceSASPermissions{Read: true},
		SASOptions: storage.SASOptions{
			Expiry: time.Now().UTC().Add(blobSASDuration),
		},
	}
	sasURL, err := blob.GetSASURI(options)

	if err != nil {
		return "", errors.Wrap(err, "failed to generate SAS key for blob")
	}
	return sasURL, nil
}

// NewBlobDownload creates a new Downloader for a blob hosted in Azure Blob Storage.
func NewBlobDownload(accountName, accountKey string, blob blobutil.AzureBlobRef) Downloader {
	return blobDownload{accountName, accountKey, blob}
}

// GetSASBlob download a blob with specified uri and sas authorization and saves it to the target directory
// Returns the filePath where the blob was downloaded
func GetSASBlob(blobURI, blobSas, targetDir string) (string, error) {
	bloburl, err := url.Parse(blobURI + blobSas)
	if err != nil {
		return "", errors.Wrapf(err, "unable to parse URL: %q", blobURI)
	}

	containerRef, err := storage.GetContainerReferenceFromSASURI(*bloburl)
	if err != nil {
		return "", errors.Wrapf(err, "unable to open storage container: %q", blobURI)
	}

	// Extract the file name only
	fileName := getFileName(blobURI)
	if fileName == "" {
		return "", fmt.Errorf("cannot extract file name from URL: %q", blobURI)
	}

	blobref := containerRef.GetBlobReference(fileName)
	reader, err := blobref.Get(nil)
	if err != nil {
		return "", errors.Wrapf(err, "unable to open storage blob: %q", blobURI)
	}

	scriptFilePath := filepath.Join(targetDir, fileName)
	const mode = 0500 // scripts should have execute permissions
	file, err := os.OpenFile(scriptFilePath, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, mode)
	if err != nil {
		return "", errors.Wrap(err, "failed to open file for writing: "+scriptFilePath)
	}
	defer file.Close()

	var buff = make([]byte, 1000)
	for numBytes, _ := reader.Read(buff); numBytes > 0; numBytes, _ = reader.Read(buff) {
		writtenBytes, writeErr := file.Write(buff[:numBytes])
		if writtenBytes != numBytes || writeErr != nil {
			return "", errors.Wrap(writeErr, "failed to write to the file: "+scriptFilePath)
		}
	}
	return scriptFilePath, nil
}

// CreateOrReplaceAppendBlob creates a reference to an append blob. If blob exists - it gets deleted first.
func CreateOrReplaceAppendBlob(blobURI, blobSas string) (*storage.Blob, error) {

	bloburl, err := url.Parse(blobURI + blobSas)
	if err != nil {
		return nil, err
	}

	containerRef, err := storage.GetContainerReferenceFromSASURI(*bloburl)
	if err != nil {
		return nil, err
	}

	fileName := getFileName(blobURI)
	if fileName == "" {
		return nil, fmt.Errorf("cannot extract file name from URL: %q", blobURI)
	}

	blobref := containerRef.GetBlobReference(fileName)
	err = blobref.PutAppendBlob(nil) // Create the page blob
	if err != nil {
		return nil, err
	}

	return blobref, nil
}

// Extract the file name only from the blob uri
func getFileName(blobURI string) string {
	s := strings.Split(blobURI, "/")
	if len(s) > 0 {
		return s[len(s)-1]
	}
	return ""
}
