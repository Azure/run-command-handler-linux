package extensionpolicysettingsrc

import (
	"fmt"
	"strings"

	"github.com/Azure/azure-extension-platform/pkg/extensionpolicysettings"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/pkg/errors"
)

func InitializeExtensionPolicySettings(ExtensionPolicyManagerPtr *extensionpolicysettings.ExtensionPolicySettingsManager[types.RCv2ExtensionPolicySettings],
	policyPath string,
	rceps *types.RCv2ExtensionPolicySettings) error {
	ExtensionPolicyManagerPtr, err := extensionpolicysettings.NewExtensionPolicySettingsManager[types.RCv2ExtensionPolicySettings](policyPath)
	if err != nil {
		return errors.Wrap(err, "failed to create extension policy settings manager")
	}

	err = ExtensionPolicyManagerPtr.LoadExtensionPolicySettings()
	if err != nil {
		return errors.Wrap(err, "failed to load extension policy settings")
	} else {
		rceps, err = ExtensionPolicyManagerPtr.GetSettings()

		if err != nil {
			return errors.Wrap(err, "failed to get extension policy settings")
		}
	}
	return nil
}

func InitialValidateHandlerSettingsAgainstPolicy(settings *handlersettings.HandlerSettings, policy *types.RCv2ExtensionPolicySettings) error {
	if policy == nil {
		return fmt.Errorf("no policy provided")
	}
	if err := ValidateScriptTypeAgainstPolicy(settings.ScriptType(), policy.LimitScripts); err != nil {
		return err
	}
	if settings.ScriptType() == types.CommandIdScript {
		if err := ValidateCommandId(settings, policy); err != nil {
			return err
		}
	}
	if policy.RunAsUser != "" {
		if err := ValidateRunAsUser(settings, policy); err != nil {
			return err
		}
	}
	if policy.DisableOutputBlobs {
		ValidateOutputBlob(settings, policy)
	}
	return nil
}

func ValidateScriptTypeAgainstPolicy(scriptType types.ScriptType, allowedScriptTypesString string) error {
	allowedScriptTypes, _ := types.StringToAllowedScriptTypeFlag(allowedScriptTypesString)
	// Compare the script type of the command with the allowed script types in the policy.
	err := types.CompareScriptTypeToAllowedScriptType(scriptType, allowedScriptTypes)
	if err != nil {
		return errors.Wrapf(err, "script type %s is not allowed by policy", scriptType)
	}
	return nil
}

func ValidateCommandId(settings *handlersettings.HandlerSettings, policy *types.RCv2ExtensionPolicySettings) error {
	settingsCommandId := settings.CommandId()
	allowedCommandIds := policy.CommandIdAllowlist

	if len(allowedCommandIds) == 0 {
		// if list is empty, all commandIds are allowed
		return nil
	}
	return extensionpolicysettings.ValidateValueInAllowlist(settingsCommandId, allowedCommandIds)
}

func ValidateRunAsUser(settings *handlersettings.HandlerSettings, policy *types.RCv2ExtensionPolicySettings) error {
	settingsRunAsUser := strings.ToLower(strings.TrimSpace(settings.RunAsUser))
	policyRunAsUser := strings.ToLower(strings.TrimSpace(policy.RunAsUser))

	if strings.Compare(settingsRunAsUser, policyRunAsUser) != 0 {
		return fmt.Errorf("RunAsUser '%s' in settings does not match RunAsUser '%s' in policy", settingsRunAsUser, policyRunAsUser)
	}
	return nil
}

func ValidateOutputBlob(settings *handlersettings.HandlerSettings, policy *types.RCv2ExtensionPolicySettings) {
	if policy.DisableOutputBlobs {
		// Log a warning that output blobs are disabled by policy. The command will still execute, but no output blobs will be created.
		if settings.OutputBlobURI != "" {
			fmt.Println("Warning: Output blobs are disabled by policy. The provided output blob URI will be ignored and no output blobs will be created for this command.")
		} else {
			fmt.Println("Warning: Output blobs are disabled by policy. No output blobs will be created for this command.")
		}
	}
}
