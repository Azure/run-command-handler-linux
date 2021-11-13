package main

import (
	"fmt"
	"io/ioutil"
	"net/url"
	"path/filepath"
	"strings"

	"os"

	"github.com/Azure/run-command-handler-linux/pkg/download"
	"github.com/Azure/run-command-handler-linux/pkg/preprocess"
	"github.com/Azure/run-command-handler-linux/pkg/urlutil"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

// downloadAndProcessURL downloads using the specified downloader and saves it to the
// specified existing directory, which must be the path to the saved file. Then
// it post-processes file based on heuristics.
func downloadAndProcessURL(ctx *log.Context, url, downloadDir string, cfg *handlerSettings) (string, error) {
	fileName, err := urlToFileName(url)
	if err != nil {
		return "", err
	}

	if !urlutil.IsValidUrl(url) {
		return "", fmt.Errorf(url + " is not a valid url") // url does not contain SAS to se can log it
	}

	downloadedFilePath := filepath.Join(downloadDir, fileName)
	scriptSAS := cfg.scriptSAS()
	if scriptSAS != "" {
		downloadedFilePath, err = download.GetSASBlob(url, scriptSAS, downloadDir)
	} else {
		downloaders, err := getDownloaders(url)
		if err == nil {
			const mode = 0500 // we assume users download scripts to execute
			_, err = download.SaveTo(ctx, downloaders, downloadedFilePath, mode)
		}
	}
	if err != nil {
		return "", err
	}

	err = postProcessFile(downloadedFilePath)
	if err != nil {
		return "", errors.Wrapf(err, "failed to post-process '%s'", fileName)
	}

	return downloadedFilePath, nil
}

// getDownloader returns a downloader for the given URL based on whether the
// storage credentials are empty or not.
func getDownloaders(fileURL string) (
	[]download.Downloader, error) {
	return []download.Downloader{download.NewURLDownload(fileURL)}, nil
}

// urlToFileName parses given URL and returns the section after the last slash
// character of the path segment to be used as a file name. If a value is not
// found, an error is returned.
func urlToFileName(fileURL string) (string, error) {
	u, err := url.Parse(fileURL)
	if err != nil {
		return "", errors.Wrapf(err, "unable to parse URL: %q", fileURL)
	}

	s := strings.Split(u.Path, "/")
	if len(s) > 0 {
		fn := s[len(s)-1]
		if fn != "" {
			return fn, nil
		}
	}
	return "", fmt.Errorf("cannot extract file name from URL: %q", fileURL)
}

// postProcessFile determines if path is a script file based on heuristics
// and makes in-place changes to the file with some post-processing such as BOM
// and DOS-line endings fixes to make the script POSIX-friendly.
func postProcessFile(path string) error {
	ok, err := preprocess.IsTextFile(path)
	if err != nil {
		return errors.Wrapf(err, "error determining if script is a text file")
	}
	if !ok {
		return nil
	}

	b, err := ioutil.ReadFile(path) // read the file into memory for processing
	if err != nil {
		return errors.Wrapf(err, "error reading file")
	}
	b = preprocess.RemoveBOM(b)
	b = preprocess.Dos2Unix(b)

	err = ioutil.WriteFile(path, b, 0)
	return errors.Wrap(os.Rename(path, path), "error writing file")
}

func saveScriptFile(filePath string, content string) error {
	const mode = 0500 // scripts should have execute permissions
	file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, mode)
	if err != nil {
		return errors.Wrap(err, "failed to open file for writing: "+filePath)
	}
	_, err = file.WriteString(content)
	file.Close()
	return errors.Wrap(err, "failed to write to the file: "+filePath)
}
