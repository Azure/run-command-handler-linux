package types

// ScriptType refers to the type of script being executed in a run command.
// This type defintion matches the ScriptType definition in CRP in the Run Command Handler,
// and should always be kept in sync with the ScriptType definition in RCv2 Windows.
// None is defined in the case where no script is passed down, which is a valid scenario.
type ScriptType string

const (
	InlineScript     ScriptType = "inline"
	DownloadedScript ScriptType = "downloaded"
	GalleryScript    ScriptType = "gallery"
	DiagnosticScript ScriptType = "diagnostic"
	CommandIdScript  ScriptType = "commandId"
	NoneScript       ScriptType = "none"
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
type AllowedScriptType int

const (
	AllowedCommandId int = 1 << iota
	Gallery
	Diagnostic
	Inline
	AllowedDownloaded
	AllowAll = AllowedCommandId | Gallery | Diagnostic | Inline | AllowedDownloaded
)

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
	DownloadedScriptsAllowlist []string          `json:"downloadedScriptsAllowlist,omitempty"`
	CommandIdAllowlist         []string          `json:"commandIdAllowlist,omitempty"`
	RunAsUser                  string            `json:"runAsUser,omitempty"`
	LimitScripts               AllowedScriptType `json:"limitScripts,omitempty"`
	DisableOutputBlobs         bool              `json:"disableOutputBlobs,omitempty"`
}

// This function is called from within the LoadExtensionPolicySettings function in extensionpolicysettings.go
// to validate the format of our policy.
func (rceps RCv2ExtensionPolicySettings) ValidateFormat() error {
	return nil
}
