package requesthelper

import (
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/go-kit/kit/log"
)

// SleepFunc pauses the execution for at least duration d.
type SleepFunc func(d time.Duration)

var (
	// ActualSleep uses actual time to pause the execution.
	ActualSleep SleepFunc = time.Sleep
)

const (
	// time to sleep between retries is an exponential backoff formula:
	//   t(n) = k * m^n
	expRetryN = 7 // how many times we retry the Download
	expRetryK = time.Second * 3
	expRetryM = 2
)

// WithRetries retrieves a response body using the specified downloader. Any
// error returned from d will be retried (and retrieved response bodies will be
// closed on failures). If the retries do not succeed, the last error is returned.
//
// It sleeps in exponentially increasing durations between retries.
func WithRetries(ctx *log.Context, rm *RequestManager, sf SleepFunc, eTag string) (*http.Response, error) {
	var lastErr error

	for n := 0; n < expRetryN; n++ {
		resp, err := rm.MakeRequest(ctx, eTag)

		// If there was no error, return the response
		if err == nil {
			return resp, nil
		}

		lastErr = err
		ctx.Log("warning", fmt.Sprintf("error on attempt %v: %v", n+1, err))

		status := -1
		if resp != nil {
			if resp.Body != nil { // we are not going to read this response body
				resp.Body.Close()
			}

			status = resp.StatusCode
		}

		// status == -1 means that there wasn't any http request
		if status == -1 {
			te, haste := lastErr.(interface {
				Temporary() bool
			})
			to, hasto := lastErr.(interface {
				Timeout() bool
			})

			if haste || hasto {
				if haste && te.Temporary() {
					ctx.Log("message", fmt.Sprintf("temporary error occurred. Retrying: %v", lastErr))
				} else if hasto && to.Timeout() {
					ctx.Log("message", fmt.Sprintf("timeout error occurred. Retrying: %v", lastErr))
				} else {
					ctx.Log("message", fmt.Sprintf("non-timeout, non-temporary error occurred, skipping retries: %v", lastErr))
					break
				}
			} else {
				ctx.Log("message", "no response returned and unexpected error, skipping retries.")
				break
			}
		} else if !isTransientHTTPStatusCode(status) {
			ctx.Log("message", fmt.Sprintf("RequestManager returned %v, skipping retries", status))
			break
		} else if responseNotModified(status) {
			return resp, nil
		}

		if n < expRetryN-1 {
			// have more retries to go, sleep before retrying
			slp := expRetryK * time.Duration(int(math.Pow(float64(expRetryM), float64(n))))
			sf(slp)
		}
	}

	return nil, lastErr
}

func isTransientHTTPStatusCode(statusCode int) bool {
	switch statusCode {
	case
		http.StatusRequestTimeout,      // 408
		http.StatusTooManyRequests,     // 429
		http.StatusInternalServerError, // 500
		http.StatusBadGateway,          // 502
		http.StatusServiceUnavailable,  // 503
		http.StatusGatewayTimeout:      // 504
		return true // timeout and too many requests
	default:
		return false
	}
}

func responseNotModified(statusCode int) bool {
	return statusCode == http.StatusNotModified
}
