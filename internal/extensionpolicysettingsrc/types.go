package extensionpolicysettingsrc

import (
	"fmt"
	"strings"

	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
)

// This refers *specifically* to file types that require signature verification
// when RequireSigning is enabled for RCv2. This is not a general enum for all file types in the extension.
// Non-script file types include binaries, parameter files, etc.
type FileType string

const (
	All     FileType = "all"
	NoFiles FileType = "none" // Named NoFiles instead of None to avoid conflict with ScriptType.None below.
	Scripts FileType = "scripts"
)

// AllowedScriptType is a bitmask enum that defines which types of scripts run command is
// allowed to execute based on customer policy. This should always match the AllowedScriptType in RCv2 Windows.
type AllowedScriptTypeFlag uint32

const (
	AllowedCommandId = 1 << iota
	Gallery
	Diagnostic
	Inline
	AllowedDownloaded
	AllowAll          = AllowedCommandId | Gallery | Diagnostic | Inline | AllowedDownloaded
	AllowedScriptNone = 0
)

func StringToAllowedScriptTypeFlag(s string) (AllowedScriptTypeFlag, error) {
	// lowercase the input to make the parsing case-insensitive
	s = strings.ToLower(s)
	// trim whitespace and split by comma
	s = strings.TrimSpace(s)
	parts := strings.Split(s, ",")

	var flag AllowedScriptTypeFlag
	for _, part := range parts {
		part = strings.TrimSpace(part)
		switch part {
		case "inline":
			flag |= Inline
		case "alloweddownloaded":
			flag |= AllowedDownloaded
		case "gallery":
			flag |= Gallery
		case "diagnostic":
			flag |= Diagnostic
		case "allowedcommandid":
			flag |= AllowedCommandId
		case "allowall":
			flag |= AllowAll
		// TO-DO: consider the case where 'none' scripts are allowed to run.
		default:
			return 0, fmt.Errorf("Unknown script type in policy: %s", part)
		}
	}
	return flag, nil
}

// RCv2ExtensionPolicySettings defines the structure of the policy file for RCv2.
// RequireSigning: describes the types of files that require signature verification.
// FileRootCert: the root certificate used for signature verification. Required if RequireSigning is not "none".
// DownloadedScriptsAllowlist: if scripts are limited to a specific allowlist, this is the list of hashes of the allowed scripts.
// CommandIdAllowlist: if commandId scripts are allowed only from specific commandIds, this is the list of allowed commandIds.
// RunAsUser: the only user with permission to run scripts. If another user tries to run a script, the command will fail.
// LimitScripts: the types of scripts that are allowed to be executed.
type RCv2ExtensionPolicySettings struct {
	// RequireSigning             FileType          `json:"requireSigning"`
	// FileRootCert               string            `json:"fileRootCert,omitempty"`
	DownloadedScriptsAllowlist []string `json:"downloadedScriptsAllowlist,omitempty"`
	CommandIdAllowlist         []string `json:"commandIdAllowlist,omitempty"`
	RunAsUser                  string   `json:"runAsUser,omitempty"`
	LimitScripts               string   `json:"limitScripts,omitempty"`
	DisableOutputBlobs         bool     `json:"disableOutputBlobs,omitempty"`
}

// This function is called from within the LoadExtensionPolicySettings function in extensionpolicysettings.go
// to validate the format of our policy.
func (rceps RCv2ExtensionPolicySettings) ValidateFormat() error {
	flag, err := StringToAllowedScriptTypeFlag(string(rceps.LimitScripts))
	// Requirements:
	// 1. If RequireSigning is not "none", FileRootCert must be present and non-empty.
	// 2. LimitScripts must be a valid AllowedScriptType value. so map/check the value to the AllowedScriptTypeFlag bitmask.
	if rceps.LimitScripts != "" {
		if err != nil {
			return fmt.Errorf("at least one of the values in LimitScripts is not a valid script type: %v", rceps.LimitScripts)
		}
	}
	// 3. If DownloadedScriptsAllowlist is not empty, limit scripts must allow "downloaded" scripts.
	if len(rceps.DownloadedScriptsAllowlist) > 0 {
		if (flag & AllowedDownloaded) == 0 {
			return fmt.Errorf("DownloadedScriptsAllowlist not empty, but LimitScripts does not allow 'downloaded' scripts")
		}
	}
	// 4. If CommandIdAllowlist is not empty, limit scripts must allow "commandId" scripts.
	if len(rceps.CommandIdAllowlist) > 0 {
		if (flag & AllowedCommandId) == 0 {
			return fmt.Errorf("CommandIdAllowlist not empty, but LimitScripts does not allow 'commandId' scripts")
		}
	}
	return nil
}

// This function compares a script type (of type ScriptType, defined in this file) to the allowed script types
// (of type AllowedScriptTypeFlag, also defined in this file) listed in the policy. These values and mappings
// are specific to Run Command, hence why they are defined here and not in the shared library.
func CompareScriptTypeToAllowedScriptType(scriptType handlersettings.ScriptType, allowedScriptTypes AllowedScriptTypeFlag) error {
	switch scriptType {
	case handlersettings.InlineScript:
		if (allowedScriptTypes & Inline) == 0 {
			return fmt.Errorf("inline scripts are not allowed by policy")
		}
	case handlersettings.DownloadedScript:
		if (allowedScriptTypes & AllowedDownloaded) == 0 {
			return fmt.Errorf("downloaded scripts are not allowed by policy")
		}
	case handlersettings.GalleryScript:
		if (allowedScriptTypes & Gallery) == 0 {
			return fmt.Errorf("gallery scripts are not allowed by policy")
		}
	case handlersettings.DiagnosticScript:
		if (allowedScriptTypes & Diagnostic) == 0 {
			return fmt.Errorf("diagnostic scripts are not allowed by policy")
		}
	case handlersettings.CommandIdScript:
		if (allowedScriptTypes & AllowedCommandId) == 0 {
			return fmt.Errorf("commandId scripts are not allowed by policy")
		}
	default:
		return fmt.Errorf("unknown script type: %v", scriptType)
	}
	return nil
}
