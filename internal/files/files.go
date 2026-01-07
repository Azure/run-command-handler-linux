package files

import (
	"fmt"
	"io/ioutil"
	"net/url"
	"path/filepath"
	"strings"

	"os"

	"github.com/Azure/azure-extension-platform/vmextension"
	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/Azure/run-command-handler-linux/pkg/download"
	"github.com/Azure/run-command-handler-linux/pkg/preprocess"
	"github.com/Azure/run-command-handler-linux/pkg/urlutil"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

var UseMockSASDownloadFailure bool = false

func DownloadAndProcessArtifact(ctx *log.Context, downloadDir string, artifact *handlersettings.UnifiedArtifact) (string, *vmextension.ErrorWithClarification) {
	fileName := artifact.FileName
	if fileName == "" {
		fileName = fmt.Sprintf("%s%d", "Artifact", artifact.ArtifactId)
	}
	targetFilePath, err := downloadAndProcessURL(ctx, artifact.ArtifactUri, downloadDir, fileName, artifact.ArtifactSasToken, artifact.ArtifactManagedIdentity)

	return targetFilePath, err
}

func DownloadAndProcessScript(ctx *log.Context, url, downloadDir string, cfg *handlersettings.HandlerSettings) (string, *vmextension.ErrorWithClarification) {
	fileName, err := UrlToFileName(url)
	if err != nil {
		return "", err
	}

	scriptSAS := cfg.ScriptSAS()
	sourceManagedIdentity := cfg.SourceManagedIdentity
	targetFilePath, err := downloadAndProcessURL(ctx, url, downloadDir, fileName, scriptSAS, sourceManagedIdentity)

	return targetFilePath, err
}

// downloadAndProcessURL downloads using the specified downloader and saves it to the
// specified existing directory, which must be the path to the saved file. Then
// it post-processes file based on heuristics.
func downloadAndProcessURL(ctx *log.Context, url, downloadDir string, fileName string, scriptSAS string, sourceManagedIdentity *handlersettings.RunCommandManagedIdentity) (string, *vmextension.ErrorWithClarification) {
	var err *vmextension.ErrorWithClarification
	if !urlutil.IsValidUrl(url) {
		return "", vmextension.NewErrorWithClarificationPtr(constants.FileDownload_CannotExtractFileNameFromUrl, fmt.Errorf(url+" is not a valid url"))
	}

	targetFilePath := filepath.Join(downloadDir, fileName)

	var scriptSASDownloadErr error = nil
	var downloadedFilePath string = ""
	if scriptSAS != "" {
		if UseMockSASDownloadFailure {
			scriptSASDownloadErr = errors.New("Downloading script using SAS token failed.")
		} else {
			downloadedFilePath, scriptSASDownloadErr = download.GetSASBlob(url, scriptSAS, downloadDir)
		}
		// Download was successful using SAS. So use downloadedFilePath
		if scriptSASDownloadErr == nil && downloadedFilePath != "" {
			targetFilePath = downloadedFilePath
		}
	}

	//If there was an error downloading using SAS URI or SAS was not provided, download using managedIdentity or publicly.
	if scriptSASDownloadErr != nil || scriptSAS == "" {
		downloaders, getDownloadersError := getDownloaders(url, sourceManagedIdentity, download.ProdMsiDownloader{})
		if getDownloadersError == nil {
			const mode = 0500 // we assume users download scripts to execute
			_, err = download.SaveTo(ctx, downloaders, targetFilePath, mode)
		} else {
			return "", vmextension.NewErrorWithClarificationPtr(constants.Msi_GenericRetrievalError, getDownloadersError)
		}
	}

	if err != nil {
		return "", err
	}

	err = PostProcessFile(targetFilePath)
	if err != nil {
		return "", err
	}

	return targetFilePath, nil
}

// getDownloaders returns one or two downloaders (two if it is an Azure storage blob):
// 1. Downloader for script using public URI.
// 2. Downloader for script using managed identity.
func getDownloaders(fileURL string, managedIdentity *handlersettings.RunCommandManagedIdentity, msiDownloader download.MsiDownloader) ([]download.Downloader, *vmextension.ErrorWithClarification) {

	if fileURL == "" {
		return nil, vmextension.NewErrorWithClarificationPtr(constants.FileDownload_Empty, fmt.Errorf("fileURL is empty"))
	}

	if download.IsAzureStorageBlobUri(fileURL) {
		// if managed identity was specified in the configuration, try to use it to download the files
		var msiProvider download.MsiProvider

		switch {
		case managedIdentity == nil || (managedIdentity.ClientId == "" && managedIdentity.ObjectId == ""):
			// get msi Provider for blob url implicitly (uses system managed identity)
			msiProvider = msiDownloader.GetMsiProvider(fileURL)

		case managedIdentity.ClientId != "" && managedIdentity.ObjectId == "":
			// uses user-managed identity
			msiProvider = msiDownloader.GetMsiProviderByClientId(fileURL, managedIdentity.ClientId)
		case managedIdentity.ClientId == "" && managedIdentity.ObjectId != "":
			// uses user-managed identity
			msiProvider = msiDownloader.GetMsiProviderByObjectId(fileURL, managedIdentity.ObjectId)
		default:
			return nil, vmextension.NewErrorWithClarificationPtr(constants.CustomerInput_ClientIdObjectIdBothSpecified, fmt.Errorf("use either ClientId or ObjectId for managed identity. Not both"))
		}

		_, msiError := msiProvider()
		if msiError == nil {
			return []download.Downloader{
				//Try downloading with MSI token first, if that fails attempt public download
				download.NewBlobWithMsiDownload(fileURL, msiProvider),
				download.NewURLDownload(fileURL), // Try downloading the Azure storage blob as public URI
			}, nil
		} else {
			return []download.Downloader{
				// Try downloading the Azure storage blob as public URI
				download.NewURLDownload(fileURL),
			}, nil
		}
	} else {
		// Public URI - do not use MSI downloader if the uri is not azure storage blob
		return []download.Downloader{download.NewURLDownload(fileURL)}, nil
	}
}

// UrlToFileName parses given URL and returns the section after the last slash
// character of the path segment to be used as a file name. If a value is not
// found, an error is returned.
func UrlToFileName(fileURL string) (string, *vmextension.ErrorWithClarification) {
	u, err := url.Parse(fileURL)
	if err != nil {
		return "", vmextension.NewErrorWithClarificationPtr(constants.FileDownload_UnableToParseFileName, errors.Wrapf(err, "unable to parse URL: %q", fileURL))
	}

	s := strings.Split(u.Path, "/")
	if len(s) > 0 {
		fn := s[len(s)-1]
		if fn != "" {
			return fn, nil
		}
	}
	return "", vmextension.NewErrorWithClarificationPtr(constants.FileDownload_CannotExtractFileNameFromUrl, fmt.Errorf("cannot extract file name from URL: %q", fileURL))
}

// postProcessFile determines if path is a script file based on heuristics
// and makes in-place changes to the file with some post-processing such as BOM
// and DOS-line endings fixes to make the script POSIX-friendly.
func PostProcessFile(path string) *vmextension.ErrorWithClarification {
	ok, ewc := preprocess.IsTextFile(path)
	if ewc != nil {
		return ewc
	}
	if !ok {
		return nil
	}

	b, err := ioutil.ReadFile(path) // read the file into memory for processing
	if err != nil {
		return vmextension.NewErrorWithClarificationPtr(constants.Internal_FailedToReadFile, errors.Wrapf(err, "error reading file"))
	}
	b = preprocess.RemoveBOM(b)
	b = preprocess.Dos2Unix(b)

	err = ioutil.WriteFile(path, b, 0)

	if err != nil {
		return vmextension.NewErrorWithClarificationPtr(constants.FileDownload_WriteFileError, errors.Wrap(os.Rename(path, path), "error writing file"))
	}
	return nil
}

func SaveScriptFile(filePath string, content string) *vmextension.ErrorWithClarification {
	const mode = 0500 // scripts should have execute permissions
	file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, mode)
	if err != nil {
		return vmextension.NewErrorWithClarificationPtr(constants.Internal_CouldNotOpenFileForWriting, errors.Wrap(err, "failed to open file for writing: "+filePath))
	}
	_, err = file.WriteString(content)
	file.Close()

	if err != nil {
		return vmextension.NewErrorWithClarificationPtr(constants.FileDownload_WriteFileError, errors.Wrap(err, "failed to write to the file: "+filePath))
	}

	return nil
}
