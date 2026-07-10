package extensionpolicysettingsrc

import (
	"fmt"
	"strings"

	"github.com/Azure/azure-extension-platform/pkg/extensionpolicysettings"
	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/Azure/run-command-handler-linux/pkg/download"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

func InitializeExtensionPolicySettings(ctx *log.Context, policyPath string) (*extensionpolicysettings.ExtensionPolicySettingsManager[RCv2ExtensionPolicySettings], *RCv2ExtensionPolicySettings, error, int) {
	if policyPath == "" {
		err := fmt.Errorf("policy path to initialize extension policy settings is empty")
		ctx.Log("message", "policy path is empty. "+constants.ContactICMForServiceErrorsMessage, "error", err)
		return nil, nil, err, constants.ExitCode_InitializeCalledWithNoPolicyPath
	}
	extensionPolicyManager, err := extensionpolicysettings.NewExtensionPolicySettingsManager[RCv2ExtensionPolicySettings](policyPath)
	if err != nil {
		// Manager only fails to be created if policy path is empty, so this shouldn't fail.
		err = errors.Wrap(err, "failed to create extension policy settings manager. Ensure the policy path is valid")
		ctx.Log("message", "failed to create extension policy settings manager. "+constants.ContactICMForServiceErrorsMessage, "error", err, "policyPath", policyPath)
		return nil, nil, err, constants.ExitCode_FailedToCreateExtensionPolicySettingsManager
	}

	err = extensionPolicyManager.LoadExtensionPolicySettings()
	if err != nil {
		err = errors.Wrap(err, "failed to load extension policy settings from file. Ensure the policy format is valid and the file is accessible")
		ctx.Log("message", "failed to load extension policy settings. "+constants.ContactICMForServiceErrorsMessage, "error", err, "policyPath", policyPath)
		return nil, nil, err, constants.ExitCode_LoadExtensionPolicySettingsFailed
	}

	rceps, err := extensionPolicyManager.GetSettings() //rceps is the pointer to the actual policy struct
	if err != nil {
		err = errors.Wrap(err, "failed to get extension policy settings after loading")
		ctx.Log("message", "failed to get extension policy settings. "+constants.ContactICMForServiceErrorsMessage, "error", err, "policyPath", policyPath)
		return nil, nil, err, constants.ExitCode_FailedToGetExtensionPolicySettings
	}
	return extensionPolicyManager, rceps, nil, 0
}

func ValidateHandlerSettingsAgainstPolicy(ctx *log.Context, settings *handlersettings.HandlerSettings, policy *RCv2ExtensionPolicySettings) (error, int) {
	if policy == nil {
		ctx.Log("message", "no policy provided for extension policy settings")
		return fmt.Errorf("no policy provided to validate handler settings"), constants.ExitCode_ValidateCalledWithNilPolicy
	}
	if err := ValidateScriptTypeAgainstPolicy(ctx, settings.ScriptType(), policy.LimitScripts); err != nil {
		return err, constants.ExitCode_ScriptTypeNotAllowedByExtensionPolicy
	}
	if settings.ScriptType() == handlersettings.CommandIdScript {
		if err := ValidateCommandId(ctx, settings, policy); err != nil {
			return err, constants.ExitCode_CommandIdNotAllowedByExtensionPolicy
		}
	}
	if policy.RunAsUser != "" {
		if err := ValidateRunAsUser(ctx, settings, policy); err != nil {
			return err, constants.ExitCode_RunAsUserNotAllowedByExtensionPolicy
		}
	}
	if policy.DisableOutputBlobs {
		if err := ValidateDisableOutputBlobs(ctx, settings, policy); err != nil {
			return err, constants.ExitCode_OutputBlobSpecifiedButNotAllowedByExtensionPolicy
		}
	}

	// TO-DO: Validate RequireSigning once that feature is implemented for RCv2.

	return nil, 0
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

func ValidateDisableOutputBlobs(ctx *log.Context, settings *handlersettings.HandlerSettings, policy *RCv2ExtensionPolicySettings) error {
	if policy.DisableOutputBlobs {
		trimmedOutputBlobURI := strings.TrimSpace(settings.OutputBlobURI)
		trimmedErrorBlobURI := strings.TrimSpace(settings.ErrorBlobURI)

		if trimmedOutputBlobURI != "" || trimmedErrorBlobURI != "" {
			err := fmt.Errorf("output blobs are disabled in policy, but settings specify outputBlobURI '%s' or errorBlobURI '%s'", download.GetUriForLogging(settings.OutputBlobURI), download.GetUriForLogging(settings.ErrorBlobURI))
			ctx.Log("message", "output blobs are disabled in policy, but settings specify output or error blob URIs", "error", err, "outputBlobURI", download.GetUriForLogging(settings.OutputBlobURI), "errorBlobURI", download.GetUriForLogging(settings.ErrorBlobURI))
			return err
		}
	}
	return nil
}
