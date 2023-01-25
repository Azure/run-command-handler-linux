package download

import (
	"github.com/google/uuid"
	"net/http"
	"net/url"
)

const (
	xMsClientRequestIdHeaderName  = "x-ms-client-request-id"
	xMsServiceRequestIdHeaderName = "x-ms-request-id"
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
	req, err := http.NewRequest("GET", u.url, nil)
	if req != nil {
		req.Header.Add(xMsClientRequestIdHeaderName, uuid.New().String())
	}
	return req, err
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
