// TODO: Consider modifying https://github.com/Azure/azure-extension-platform/blob/main/pkg/utils/utils_linux.go to
// add the logic to allow other extensions to delete all files and settings as desired.
package linuxutils

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

func TryClearExtensionScriptsDirectoriesAndSettingsFiles(ctx *log.Context, scriptsDirectory string, runtimeSettingsDirectory string, extensionName string, runtimeSettingsRegexFormatWithAnyExtName string) error {
	// It is important to call both methods first and then handle the errors or unexpected files could remain in the VM.
	// E.g., If nothing was dowloaded to the VM, the directory won't exist and thus the `TryDeleteDirectories` will return an error.
	dirErr := TryDeleteDirectories(ctx, filepath.Join(scriptsDirectory, extensionName))
	filesErr := tryClearRegexMatchingFiles(ctx, runtimeSettingsDirectory, runtimeSettingsRegexFormatWithAnyExtName, true)

	if dirErr != nil && filesErr != nil {
		return fmt.Errorf("failed to delete dirs: %w; failed to delete settings: %w", dirErr, filesErr)
	} else if dirErr != nil {
		return errors.Wrap(dirErr, "could not delete extension scripts directories")
	} else if filesErr != nil {
		return errors.Wrap(filesErr, "could not delete runtime settings files")
	}

	return nil
}

func TryDeleteDirectories(ctx *log.Context, parentDirectory string) error {
	// Check if the directory exists
	directoryFDRef, err := os.Open(parentDirectory)
	if err != nil {
		return errors.Wrap(err, "could not open parent directory")
	}

	dirEntries, err := directoryFDRef.ReadDir(0)
	if err != nil {
		return errors.Wrap(err, "could not read contents from directory")
	}

	if dirEntries != nil {
		for _, dirEntry := range dirEntries {
			if dirEntry.IsDir() {
				fullDirectoryPath := filepath.Join(parentDirectory, dirEntry.Name())
				ctx.Log("message", "trying to remove directory: "+fullDirectoryPath)
				err = os.RemoveAll(fullDirectoryPath)
				if err != nil {
					ctx.Log("warning", "could not delete directory", "error", err)
				}
			}
		}

		return nil
	}

	return err
}

func tryClearRegexMatchingFiles(ctx *log.Context, directory string, regexFileNamePattern string, deleteFiles bool) error {
	if regexFileNamePattern == "" {
		return errors.New("empty regexFileNamePattern argument")
	}

	// Check if the directory exists
	directoryFDRef, err := os.Open(directory)
	if err != nil {
		return errors.Wrap(err, "could not open directory")
	}

	regex, err := regexp.Compile(regexFileNamePattern)
	if err != nil {
		return errors.Wrap(err, "could not parse given regular expression")
	}

	dirEntries, err := directoryFDRef.ReadDir(0)
	if err != nil {
		return errors.Wrap(err, "could not read contents from directory")
	}

	for _, dirEntry := range dirEntries {
		fileName := dirEntry.Name()

		if regex.MatchString(fileName) {
			fullFilePath := filepath.Join(directory, fileName)
			if deleteFiles {
				ctx.Log("message", "deleting file "+fullFilePath)
				err = os.Remove(fullFilePath)
				if err != nil {
					ctx.Log("warning", "could not delete file", "error", err)
				}
			} else {
				ctx.Log("message", "cleaning file "+fullFilePath)
				err = os.Truncate(fullFilePath, 0) // Calling create on existing file truncates file
				if err != nil {
					ctx.Log("warning", "could not truncate file", "error", err)
				}
			}
		}
	}

	return nil
}
