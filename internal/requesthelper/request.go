package requesthelper

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/go-kit/kit/log"
)

// RequestFactory describes a method to create HTTP requests.
type RequestFactory interface {
	// GetRequest returns a new GET request for the resource.
	GetRequest(ctx *log.Context, eTag string) (*http.Request, error)
}

// RequestManager provides an abstraction for http requests
type RequestManager struct {
	httpClient     *http.Client
	requestFactory RequestFactory
}

// GetRequestManager returns a request manager for json requests
func GetRequestManager(rf RequestFactory, timeout time.Duration) *RequestManager {
	return &RequestManager{
		httpClient:     getHTTPClient(timeout),
		requestFactory: rf,
	}
}

// MakeRequest retrieves a response body and checks the response status code to see
// if it is 200 OK and then returns the response body. It issues a new request
// every time called. It is caller's responsibility to close the response body.
func (rm *RequestManager) MakeRequest(ctx *log.Context, eTag string) (*http.Response, error) {
	req, err := rm.requestFactory.GetRequest(ctx, eTag)
	if err != nil {
		return nil, err
	}

	resp, err := rm.httpClient.Do(req)
	if err != nil {
		return resp, err
	}

	// These are the only status codes that are expected
	if resp.StatusCode == http.StatusOK ||
		resp.StatusCode == http.StatusPartialContent ||
		resp.StatusCode == http.StatusNotModified ||
		resp.StatusCode == http.StatusNotFound {
		return resp, nil
	}

	err = fmt.Errorf("unexpected status code: actual=%d expected=%d", resp.StatusCode, http.StatusOK)
	return resp, err
}

// httpClient is the default client to be used. http.Get() uses a client without timeouts (http.DefaultClient)
// However, an infinite timeout will cause the deployment to fail so here we are setting a specific timeout.
func getHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Dial: (&net.Dialer{
				Timeout:   timeout,
				KeepAlive: 30 * time.Second,
			}).Dial,
			Proxy:                 http.ProxyFromEnvironment,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 20 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
		Timeout: timeout}
}
