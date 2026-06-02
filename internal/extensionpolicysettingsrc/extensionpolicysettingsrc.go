package extensionpolicysettingsrc

import (
	"fmt"
	"strings"

	"github.com/Azure/azure-extension-platform/pkg/extensionpolicysettings"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/pkg/errors"
)

func InitializeExtensionPolicySettings(policyPath string) (*extensionpolicysettings.ExtensionPolicySettingsManager[RCv2ExtensionPolicySettings], *RCv2ExtensionPolicySettings, error) {
	var ExtensionPolicyManagerPtr *extensionpolicysettings.ExtensionPolicySettingsManager[RCv2ExtensionPolicySettings]
	var rceps *RCv2ExtensionPolicySettings

	ExtensionPolicyManagerPtr, err := extensionpolicysettings.NewExtensionPolicySettingsManager[RCv2ExtensionPolicySettings](policyPath)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to create extension policy settings manager")
	}

	err = ExtensionPolicyManagerPtr.LoadExtensionPolicySettings()
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to load extension policy settings")
	} else {
		rceps, err = ExtensionPolicyManagerPtr.GetSettings()

		if err != nil {
			return nil, nil, errors.Wrap(err, "failed to get extension policy settings")
		}
	}
	return ExtensionPolicyManagerPtr, rceps, nil
}

func InitialValidateHandlerSettingsAgainstPolicy(settings *handlersettings.HandlerSettings, policy *RCv2ExtensionPolicySettings) error {
	if policy == nil {
		return fmt.Errorf("no policy provided")
	}
	if err := ValidateScriptTypeAgainstPolicy(settings.ScriptType(), policy.LimitScripts); err != nil {
		return err
	}
	if settings.ScriptType() == handlersettings.CommandIdScript {
		if err := ValidateCommandId(settings, policy); err != nil {
			return err
		}
	}
	if policy.RunAsUser != "" {
		if err := ValidateRunAsUser(settings, policy); err != nil {
			return err
		}
	}

	// TO-DO: Validate Disable Outputblob and RequireSigning once those features are implemented for RCv2.

	return nil
}

func ValidateScriptTypeAgainstPolicy(scriptType handlersettings.ScriptType, allowedScriptTypesString string) error {
	allowedScriptTypes, _ := StringToAllowedScriptTypeFlag(allowedScriptTypesString)
	// Compare the script type of the command with the allowed script types in the policy.
	err := CompareScriptTypeToAllowedScriptType(scriptType, allowedScriptTypes)
	if err != nil {
		return errors.Wrapf(err, "script type %s is not allowed by policy", scriptType)
	}
	return nil
}

func ValidateCommandId(settings *handlersettings.HandlerSettings, policy *RCv2ExtensionPolicySettings) error {
	settingsCommandId := settings.CommandId()
	allowedCommandIds := policy.CommandIdAllowlist

	if len(allowedCommandIds) == 0 {
		// if list is empty, all commandIds are allowed
		return nil
	}
	err := extensionpolicysettings.ValidateValueInAllowlist(settingsCommandId, allowedCommandIds)
	if err != nil {
		return errors.Wrapf(err, "command ID %s is not allowed by policy", settingsCommandId)
	}
	return nil
}

func ValidateRunAsUser(settings *handlersettings.HandlerSettings, policy *RCv2ExtensionPolicySettings) error {
	settingsRunAsUser := strings.ToLower(strings.TrimSpace(settings.RunAsUser))
	policyRunAsUser := strings.ToLower(strings.TrimSpace(policy.RunAsUser))

	if strings.Compare(settingsRunAsUser, policyRunAsUser) != 0 {
		return fmt.Errorf("RunAsUser '%s' in settings does not match RunAsUser '%s' in policy", settingsRunAsUser, policyRunAsUser)
	}
	return nil
}
