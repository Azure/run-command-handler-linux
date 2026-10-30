package files

import (
	"fmt"
	"io/ioutil"
	"net/url"
	"path/filepath"
	"strings"

	"os"

	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/Azure/run-command-handler-linux/pkg/download"
	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/Azure/run-command-handler-linux/pkg/preprocess"
	"github.com/Azure/run-command-handler-linux/pkg/urlutil"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

var UseMockSASDownloadFailure bool = false

var (
	downloadSASBlob = download.GetSASBlob
	saveDownload    = download.SaveTo
	isScriptText    = preprocess.IsTextFile
	readScript      = ioutil.ReadFile
	writeScript     = ioutil.WriteFile
	setScriptMode   = os.Chmod
)

type scriptWriter interface {
	WriteString(string) (int, error)
	Close() error
}

var openScriptFile = func(name string, flag int, mode os.FileMode) (scriptWriter, error) {
	return os.OpenFile(name, flag, mode)
}

func DownloadAndProcessArtifact(ctx *log.Context, downloadDir string, artifact *handlersettings.UnifiedArtifact) (string, error) {
	fileName := artifact.FileName
	if fileName == "" {
		fileName = fmt.Sprintf("%s%d", "Artifact", artifact.ArtifactId)
	}
	targetFilePath, err := downloadAndProcessURL(ctx, artifact.ArtifactUri, downloadDir, fileName, artifact.ArtifactSasToken, artifact.ArtifactManagedIdentity)

	return targetFilePath, err
}

func DownloadAndProcessScript(ctx *log.Context, url, downloadDir string, cfg *handlersettings.HandlerSettings) (string, error) {
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
func downloadAndProcessURL(ctx *log.Context, url, downloadDir string, fileName string, scriptSAS string, sourceManagedIdentity *handlersettings.RunCommandManagedIdentity) (string, error) {
	var err error
	if !urlutil.IsValidUrl(url) {
		return "", errorutil.WithCode(errorutil.CustomerInput_invalidScriptUrl, fmt.Errorf("%s is not a valid url", url)) // url does not contain SAS to se can log it
	}

	targetFilePath := filepath.Join(downloadDir, fileName)

	var scriptSASDownloadErr error = nil
	var downloadedFilePath string = ""
	if scriptSAS != "" {
		if UseMockSASDownloadFailure {
			scriptSASDownloadErr = errors.New("Downloading script using SAS token failed.")
		} else {
			downloadedFilePath, scriptSASDownloadErr = downloadSASBlob(url, scriptSAS, downloadDir)
		}
		scriptSASDownloadErr = errorutil.WithCode(errorutil.FileDownload_scriptSasDownloadFailed, scriptSASDownloadErr)
		// Download was successful using SAS. So use downloadedFilePath
		if scriptSASDownloadErr == nil && downloadedFilePath != "" {
			targetFilePath = downloadedFilePath
		}
	}

	//If there was an error downloading using SAS URI or SAS was not provided, download using managedIdentity or publicly.
	if scriptSASDownloadErr != nil || scriptSAS == "" {
		if scriptSASDownloadErr != nil {
			ctx.Log("info", "Downloading script using SAS token failed. Attempting download using managed identity or public access.", "error", scriptSASDownloadErr)
		}
		downloaders, getDownloadersError := getDownloaders(url, sourceManagedIdentity, download.ProdMsiDownloader{})
		if getDownloadersError == nil {
			const mode = 0500 // we assume users download scripts to execute
			_, err = saveDownload(ctx, downloaders, targetFilePath, mode)
		} else {
			return "", getDownloadersError
		}
	}

	if err != nil {
		return "", errorutil.WithCode(errorutil.Os_FailedToMakeDownloadedFileExecutable, err)
	}

	// Downloads are executable but read-only; allow the owner to normalize the
	// script, then restore the original execution permissions even on failure.
	if err := setScriptMode(targetFilePath, 0700); err != nil {
		return "", errorutil.WithCode(errorutil.Os_FailedToMakeDownloadedFileWritable, errors.Wrap(err, "failed to make downloaded file writable for post-processing"))
	}
	err = PostProcessFile(targetFilePath)
	restoreErr := errorutil.WithCode(errorutil.Os_FailedToRestoreDownloadedFilePermissions, errors.Wrap(setScriptMode(targetFilePath, 0500), "failed to restore downloaded file permissions"))
	if err != nil {
		if restoreErr != nil {
			return "", fmt.Errorf("failed to post-process %q: %w; failed to restore permissions: %w", fileName, err, restoreErr)
		}
		return "", errors.Wrapf(err, "failed to post-process '%s'", fileName)
	}
	if restoreErr != nil {
		return "", restoreErr
	}

	return targetFilePath, nil
}

// getDownloaders returns one or two downloaders (two if it is an Azure storage blob):
// 1. Downloader for script using public URI.
// 2. Downloader for script using managed identity.
func getDownloaders(fileURL string, managedIdentity *handlersettings.RunCommandManagedIdentity, msiDownloader download.MsiDownloader) ([]download.Downloader, error) {

	if fileURL == "" {
		return nil, errorutil.WithCode(errorutil.CustomerInput_emptyDownloadUrl, fmt.Errorf("fileURL is empty"))
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
			return nil, errorutil.WithCode(errorutil.CustomerInput_clientIdObjectIdBothSpecified, fmt.Errorf("use either ClientId or ObjectId for managed identity. Not both"))
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
func UrlToFileName(fileURL string) (string, error) {
	u, err := url.Parse(fileURL)
	if err != nil {
		return "", errorutil.WithCode(errorutil.CustomerInput_invalidDownloadFileUrl, errors.Wrapf(err, "unable to parse URL: %q", fileURL))
	}

	s := strings.Split(u.Path, "/")
	if len(s) > 0 {
		fn := s[len(s)-1]
		if fn != "" {
			return fn, nil
		}
	}
	return "", errorutil.WithCode(errorutil.CustomerInput_missingDownloadFileName, fmt.Errorf("cannot extract file name from URL: %q", fileURL))
}

// postProcessFile determines if path is a script file based on heuristics
// and makes in-place changes to the file with some post-processing such as BOM
// and DOS-line endings fixes to make the script POSIX-friendly.
func PostProcessFile(path string) error {
	ok, err := isScriptText(path)
	if err != nil {
		return errorutil.WithCode(errorutil.Os_FailedToDetermineScriptFileType, errors.Wrapf(err, "error determining if script is a text file"))
	}
	if !ok {
		return nil
	}

	b, err := readScript(path) // read the file into memory for processing
	if err != nil {
		return errorutil.WithCode(errorutil.Os_FailedToReadScriptForPostprocessing, errors.Wrapf(err, "error reading file"))
	}
	b = preprocess.RemoveBOM(b)
	b = preprocess.Dos2Unix(b)

	err = writeScript(path, b, 0)
	if err != nil {
		return errorutil.WithCode(errorutil.Os_FailedToWritePostprocessedScript, errors.Wrap(err, "error writing file"))
	}
	return nil
}

func SaveScriptFile(filePath string, content string) error {
	const mode = 0500 // scripts should have execute permissions
	file, err := openScriptFile(filePath, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, mode)
	if err != nil {
		return errorutil.WithCode(errorutil.Os_FailedToOpenEmbeddedScript, errors.Wrap(err, "failed to open file for writing: "+filePath))
	}
	_, err = file.WriteString(content)
	closeErr := file.Close()
	if err != nil {
		return errorutil.WithCode(errorutil.Os_FailedToWriteEmbeddedScript, errors.Wrap(err, "failed to write to the file: "+filePath))
	}
	return errorutil.WithCode(errorutil.Os_FailedToCloseEmbeddedScript, errors.Wrap(closeErr, "failed to close the file: "+filePath))
}
