package urlutil

import (
	"net/url"
	"strings"

	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
)

type redactedError struct {
	message string
	cause   error
}

func (e redactedError) Error() string { return e.message }
func (e redactedError) Unwrap() error { return e.cause }

func RemoveUrlFromErr(err error) error {
	if err == nil {
		return nil
	}
	strSegments := strings.Split(err.Error(), " ")
	for i, v := range strSegments {
		if IsValidUrl(v) {
			// we found a url
			strSegments[i] = "[REDACTED]"
		}
	}
	return errorutil.WithCode(errorutil.FileDownload_redactedUrlError, redactedError{strings.Join(strSegments, " "), err})
}

func IsValidUrl(urlstring string) bool {
	u, parseError := url.Parse(strings.Trim(urlstring, `"'`))
	if parseError == nil && u.Scheme != "" && u.Host != "" {
		return true
	}
	return false
}
