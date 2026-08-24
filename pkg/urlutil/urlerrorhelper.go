package urlutil

import (
	"errors"
	"net/url"
	"strings"
)

func RemoveUrlFromErr(err error) error {
	strSegments := strings.Split(err.Error(), " ")
	for i, v := range strSegments {
		if IsValidUrl(v) {
			// we found a url
			strSegments[i] = "[REDACTED]"
		}
	}
	return errors.New(strings.Join(strSegments, " "))
}

func IsValidUrl(urlstring string) bool {
	u, parseError := url.Parse(urlstring)
	if parseError == nil && u.Scheme != "" && u.Host != "" {
		return true
	}
	return false
}
