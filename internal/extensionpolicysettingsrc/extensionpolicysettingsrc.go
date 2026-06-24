package extensionpolicysettingsrc

import (
	"fmt"
	"strings"

	"github.com/Azure/azure-extension-platform/pkg/extensionpolicysettings"
	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

func InitializeExtensionPolicySettings(ctx *log.Context, policyPath string) (*extensionpolicysettings.ExtensionPolicySettingsManager[RCv2ExtensionPolicySettings], *RCv2ExtensionPolicySettings, error) {
	if policyPath == "" {
		err := fmt.Errorf("policy path is empty")
		ctx.Log("message", "policy path is empty. "+constants.ContactICMForServiceErrorsMessage, "error", err)
		return nil, nil, err
	}
	extensionPolicyManager, err := extensionpolicysettings.NewExtensionPolicySettingsManager[RCv2ExtensionPolicySettings](policyPath)
	if err != nil {
		err = errors.Wrap(err, "failed to create extension policy settings manager")
		ctx.Log("message", "failed to create extension policy settings manager. "+constants.ContactICMForServiceErrorsMessage, "error", err, "policyPath", policyPath)
		return nil, nil, err
	}

	err = extensionPolicyManager.LoadExtensionPolicySettings()
	if err != nil {
		err = errors.Wrap(err, "failed to load extension policy settings")
		ctx.Log("message", "failed to load extension policy settings. "+constants.ContactICMForServiceErrorsMessage, "error", err, "policyPath", policyPath)
		return nil, nil, err
	}

	rceps, err := extensionPolicyManager.GetSettings() //rceps is the pointer to the actual policy struct
	if err != nil {
		err = errors.Wrap(err, "failed to get extension policy settings after loading")
		ctx.Log("message", "failed to get extension policy settings. "+constants.ContactICMForServiceErrorsMessage, "error", err, "policyPath", policyPath)
		return nil, nil, err
	}
	return extensionPolicyManager, rceps, nil
}

func ValidateHandlerSettingsAgainstPolicy(ctx *log.Context, settings *handlersettings.HandlerSettings, policy *RCv2ExtensionPolicySettings) error {
	if policy == nil {
		ctx.Log("message", "no policy provided for extension policy settings")
		return fmt.Errorf("no policy provided")
	}
	if err := ValidateScriptTypeAgainstPolicy(ctx, settings.ScriptType(), policy.LimitScripts); err != nil {
		return err
	}
	if settings.ScriptType() == handlersettings.CommandIdScript {
		if err := ValidateCommandId(ctx, settings, policy); err != nil {
			return err
		}
	}
	if policy.RunAsUser != "" {
		if err := ValidateRunAsUser(ctx, settings, policy); err != nil {
			return err
		}
	}

	// TO-DO: Validate Disable Outputblob and RequireSigning once those features are implemented for RCv2.

	return nil
}

func ValidateScriptTypeAgainstPolicy(ctx *log.Context, scriptType handlersettings.ScriptType, allowedScriptTypesString string) error {
	allowedScriptTypes, _ := StringToAllowedScriptTypeFlag(allowedScriptTypesString)
	// Compare the script type of the command with the allowed script types in the policy.
	err := CompareScriptTypeToAllowedScriptType(scriptType, allowedScriptTypes)
	if err != nil {
		ctx.Log("message", "script type not allowed by policy", "error", err, "scriptType", scriptType)
		return errors.Wrapf(err, "script type %s is not allowed by policy", scriptType)
	}
	return nil
}

func ValidateCommandId(ctx *log.Context, settings *handlersettings.HandlerSettings, policy *RCv2ExtensionPolicySettings) error {
	settingsCommandId := settings.CommandId()
	allowedCommandIds := policy.CommandIdAllowlist

	if len(allowedCommandIds) == 0 {
		// if list is empty, all commandIds are allowed
		ctx.Log("message", "allowedCommandID list empty, allowing all commands")
		return nil
	}
	err := extensionpolicysettings.ValidateValueInAllowlist(settingsCommandId, allowedCommandIds)
	if err != nil {
		ctx.Log("message", "command ID is not allowed by policy", "error", err, "commandId", settingsCommandId)
		return errors.Wrapf(err, "command ID %s is not allowed by policy", settingsCommandId)
	}
	return nil
}

func ValidateRunAsUser(ctx *log.Context, settings *handlersettings.HandlerSettings, policy *RCv2ExtensionPolicySettings) error {
	settingsRunAsUser := strings.ToLower(strings.TrimSpace(settings.RunAsUser))
	policyRunAsUser := strings.ToLower(strings.TrimSpace(policy.RunAsUser))

	if strings.Compare(settingsRunAsUser, policyRunAsUser) != 0 {
		err := fmt.Errorf("runAsUser '%s' in settings does not match runAsUser '%s' in policy", settingsRunAsUser, policyRunAsUser)
		ctx.Log("message", "runAsUser settings does not match runAsUser in policy", "error", err, "settingsRunAsUser", settingsRunAsUser, "policyRunAsUser", policyRunAsUser)
		return err
	}
	return nil
}
