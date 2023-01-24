package main

import (
	"net/url"
	"os"
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
