package handlersettings

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/Azure/azure-extension-platform/vmextension"
	"github.com/go-kit/kit/log"
)

func DoesFileExist(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	return false
}

// Scrub query. Used to remove the query parts like SAS token.
func GetUriForLogging(uriString string) string {
	if uriString == "" {
		return uriString
	}

	u, err := url.Parse(uriString)
	if err != nil {
		return ""
	}

	return u.Scheme + "//" + u.Host + u.Path
}

// Get handler settings from config folder. Example path: /var/lib/waagent/Microsoft.CPlat.Core.RunCommandHandlerLinux-1.3.2/config
func GetHandlerSettings(configFolder string, extensionName string, sequenceNumber int, logContext *log.Context) (HandlerSettings, *vmextension.ErrorWithClarification) {
	configPath := GetConfigFilePath(configFolder, sequenceNumber, extensionName)
	cfg, err := ParseAndValidateSettings(logContext, configPath)
	return cfg, err
}

// Gets the config file path for the current extension name and sequence number.
// Example config file path: RC0001_02.0.settings
func GetConfigFilePath(configFolder string, sequenceNumber int, extensionName string) string {
	configFile := fmt.Sprintf("%d.settings", sequenceNumber)
	if extensionName != "" {
		configFile = extensionName + "." + configFile
	}
	configPath := filepath.Join(configFolder, configFile)
	return configPath
}
