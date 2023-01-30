package download

import (
	"net/http"
	"net/url"
)

// urlDownload describes a URL to download.
type urlDownload struct {
	url string
}

// NewURLDownload creates a new  downloader with the provided URL
func NewURLDownload(url string) Downloader {
	return urlDownload{url}
}

// GetRequest returns a new request to download the URL
func (u urlDownload) GetRequest() (*http.Request, error) {
	return http.NewRequest("GET", u.url, nil)
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

	return u.Scheme + "://" + u.Host + u.Path
}
