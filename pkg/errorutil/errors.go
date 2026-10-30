// Package errorutil attaches site-specific RunCommand error clarification codes.
package errorutil

import "github.com/Azure/azure-extension-platform/vmextension"

// WithCode classifies an error only if a lower layer has not already done so.
// Positive codes identify customer input or VM problems; negative codes identify
// handler or Azure infrastructure failures. Each code belongs to one call site.
func WithCode(code ErrorClarificationCode, err error) error {
	if err == nil {
		return nil
	}
	if _, ok := Code(err); ok {
		return err
	}
	return vmextension.NewErrorWithClarificationPtr(code, err)
}

// Code supports both pointer and value platform errors through wrapping.
func Code(err error) (int, bool) {
	switch err := err.(type) {
	case *vmextension.ErrorWithClarification:
		if err != nil {
			return err.ErrorCode, true
		}
	case vmextension.ErrorWithClarification:
		return err.ErrorCode, true
	case interface{ Unwrap() []error }:
		for _, cause := range err.Unwrap() {
			if code, ok := Code(cause); ok {
				return code, true
			}
		}
	case interface{ Unwrap() error }:
		return Code(err.Unwrap())
	}
	return 0, false
}
