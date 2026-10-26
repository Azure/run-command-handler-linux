package download

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-extension-foundation/msi"
	"github.com/Azure/azure-extension-platform/vmextension"
	"github.com/Azure/azure-sdk-for-go/storage"
	"github.com/Azure/run-command-handler-linux/pkg/blobutil"
	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

type clarificationDownloader struct{ err error }

func (d clarificationDownloader) GetRequest() (*http.Request, error) { return nil, d.err }

type clarificationTransport func(*http.Request) (*http.Response, error)

func (f clarificationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type clarificationReader struct {
	err    error
	closed bool
}

func (r *clarificationReader) Read([]byte) (int, error) { return 0, r.err }
func (r *clarificationReader) Close() error             { r.closed = true; return nil }

type clarificationWriter struct {
	buffer bytes.Buffer
	err    error
	closed bool
}

func (w *clarificationWriter) Write(b []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	return w.buffer.Write(b)
}
func (w *clarificationWriter) String() string { return w.buffer.String() }
func (w *clarificationWriter) Close() error   { w.closed = true; return nil }

func requireDownloadCode(t *testing.T, expected int, err error) {
	t.Helper()
	require.Error(t, err)
	code, ok := errorutil.Code(err)
	require.True(t, ok, "missing clarification: %v", err)
	require.Equal(t, expected, code)
}

func clarificationLog() *log.Context { return log.NewContext(log.NewNopLogger()) }

func stubDownloadResponse(t *testing.T, status int, body io.ReadCloser) {
	t.Helper()
	old := MakeHttpRequest
	t.Cleanup(func() { MakeHttpRequest = old })
	MakeHttpRequest = func(*http.Request) (*http.Response, error) {
		header := make(http.Header)
		header.Set(xMsServiceRequestIdHeaderName, "mock-request")
		return &http.Response{StatusCode: status, Status: fmt.Sprint(status), Header: header, Body: body}, nil
	}
}

func stubDownloadFile(t *testing.T, writer io.WriteCloser, err error) {
	t.Helper()
	old := openDownloadFile
	t.Cleanup(func() { openDownloadFile = old })
	openDownloadFile = func(string, int, os.FileMode) (io.WriteCloser, error) { return writer, err }
}

func TestRequestAndTransportClarifications(t *testing.T) {
	_, err := NewURLDownload("https://%").GetRequest()
	requireDownloadCode(t, errorutil.FileDownload_invalidRequestUrl, err)
	cause := errors.New("injected transport failure")
	old := httpClient
	t.Cleanup(func() { httpClient = old })
	httpClient = &http.Client{Transport: clarificationTransport(func(*http.Request) (*http.Response, error) {
		return nil, cause
	})}
	request, err := http.NewRequest(http.MethodGet, "https://example.invalid", nil)
	require.NoError(t, err)
	_, err = HttpClientDo(request)
	requireDownloadCode(t, errorutil.FileDownload_networkingError, err)
	require.True(t, errors.Is(err, cause))
	timeout := &net.DNSError{IsTimeout: true}
	httpClient.Transport = clarificationTransport(func(*http.Request) (*http.Response, error) {
		return nil, timeout
	})
	_, err = HttpClientDo(request)
	requireDownloadCode(t, errorutil.FileDownload_exceededTimeout, err)
	require.ErrorIs(t, err, timeout)
	_, _, err = Download(clarificationLog(), clarificationDownloader{cause})
	requireDownloadCode(t, errorutil.Internal_failedToCreateDownloadRequest, err)
	require.True(t, errors.Is(err, cause))
}

func TestDownloadNetworkAndStatusClarifications(t *testing.T) {
	old := MakeHttpRequest
	t.Cleanup(func() { MakeHttpRequest = old })
	cause := errors.New(`Get "https://example.invalid/script?sig=secret": failed`)
	MakeHttpRequest = func(*http.Request) (*http.Response, error) { return nil, cause }
	_, _, err := Download(clarificationLog(), NewURLDownload("https://example.invalid/script"))
	requireDownloadCode(t, errorutil.FileDownload_httpRequestFailed, err)
	require.NotContains(t, err.Error(), "secret")
	require.True(t, errors.Is(err, cause))
	for _, tc := range []struct {
		status          int
		direct, managed errorutil.ErrorClarificationCode
	}{
		{400, errorutil.FileDownload_badRequest, errorutil.Msi_doesNotHaveRightPermissions},
		{401, errorutil.FileDownload_accessDenied, errorutil.Msi_doesNotHaveRightPermissions},
		{403, errorutil.FileDownload_accessDenied, errorutil.Msi_doesNotHaveRightPermissions},
		{404, errorutil.FileDownload_doesNotExist, errorutil.Msi_notFound},
		{409, errorutil.FileDownload_genericError, errorutil.Msi_doesNotHaveRightPermissions},
		{429, errorutil.FileDownload_genericError, errorutil.Msi_GenericRetrievalError},
		{500, errorutil.Storage_internalServerError, errorutil.Imds_internalMsiError},
		{502, errorutil.FileDownload_internalServerError, errorutil.Imds_downloadInternalServerError},
		{503, errorutil.FileDownload_internalServerError, errorutil.Imds_downloadInternalServerError},
	} {
		for _, identity := range []bool{false, true} {
			body := &clarificationReader{}
			stubDownloadResponse(t, tc.status, body)
			d := NewURLDownload("https://example.invalid/script")
			if identity {
				d = NewBlobWithMsiDownload("https://example.invalid/script", func() (msi.Msi, error) {
					return msi.Msi{AccessToken: "fake"}, nil
				})
			}
			_, _, err := Download(clarificationLog(), d)
			expected := tc.direct
			if identity {
				expected = tc.managed
			}
			requireDownloadCode(t, expected, err)
			require.Contains(t, err.Error(), "Service request ID: mock-request")
			require.True(t, body.closed)
		}
	}
	stubDownloadResponse(t, http.StatusForbidden, nil)
	status, body, err := Download(clarificationLog(), NewURLDownload("https://example.invalid/script"))
	requireDownloadCode(t, errorutil.FileDownload_accessDenied, err)
	require.Equal(t, http.StatusForbidden, status)
	require.Nil(t, body)
}

func TestRetryAndSaveClarifications(t *testing.T) {
	_, err := WithRetries(clarificationLog(), nil, func(time.Duration) {})
	requireDownloadCode(t, errorutil.Internal_noDownloaders, err)
	first := vmextension.NewErrorWithClarificationPtr(1997, errors.New("first failure"))
	last := vmextension.NewErrorWithClarificationPtr(1998, errors.New("last failure"))
	_, err = WithRetries(clarificationLog(), []Downloader{
		clarificationDownloader{first}, clarificationDownloader{last},
	}, func(time.Duration) {})
	requireDownloadCode(t, 1998, err)
	require.True(t, errors.Is(err, last))
	require.True(t, errors.Is(err, first))
	require.Contains(t, err.Error(), "first failure")
	_, err = WithRetries(clarificationLog(), []Downloader{
		clarificationDownloader{first}, clarificationDownloader{*last},
	}, func(time.Duration) {})
	requireDownloadCode(t, 1998, err)
	require.True(t, errors.Is(err, *last))
	require.True(t, errors.Is(err, first))
	// errorutil.Internal_failedToAggregateDownloadAttempts is a defensive retry-wrapper fallback: Download always classifies its
	// error first, so assigning it at runtime would overwrite a lower-level code.

	cause := errors.New("injected file failure")
	stubDownloadFile(t, nil, cause)
	_, err = SaveTo(clarificationLog(), nil, "script", 0500)
	requireDownloadCode(t, errorutil.FileDownload_failedToOpenDestination, err)
	require.True(t, errors.Is(err, cause))

	writer := &clarificationWriter{}
	stubDownloadFile(t, writer, nil)
	_, err = SaveTo(clarificationLog(), []Downloader{clarificationDownloader{last}}, "script", 0500)
	requireDownloadCode(t, 1998, err)
	require.True(t, writer.closed)
	// errorutil.Internal_failedToDownloadToFile is a defensive SaveTo wrapper fallback: WithRetries always returns
	// a classified error (including the empty-downloader case), preserved here.

	reader := &clarificationReader{err: cause}
	stubDownloadResponse(t, http.StatusOK, reader)
	writer = &clarificationWriter{}
	stubDownloadFile(t, writer, nil)
	_, err = SaveTo(clarificationLog(), []Downloader{NewURLDownload("https://example.invalid")}, "script", 0500)
	requireDownloadCode(t, errorutil.FileDownload_failedToWriteDestination, err)
	require.True(t, errors.Is(err, cause))
	require.True(t, reader.closed)
	require.True(t, writer.closed)

	stubDownloadResponse(t, http.StatusOK, io.NopCloser(strings.NewReader("script")))
	writer = &clarificationWriter{err: cause}
	stubDownloadFile(t, writer, nil)
	_, err = SaveTo(clarificationLog(), []Downloader{NewURLDownload("https://example.invalid")}, "script", 0500)
	requireDownloadCode(t, errorutil.FileDownload_failedToWriteDestination, err)
	require.True(t, errors.Is(err, cause))
	require.True(t, writer.closed)
}

func TestBlobRequestClarifications(t *testing.T) {
	blob := blobutil.AzureBlobRef{StorageBase: "core.windows.net", Container: "container", Blob: "script"}
	_, err := NewBlobDownload("account", "invalid base64!", blob).GetRequest()
	requireDownloadCode(t, errorutil.Storage_failedToInitializeClient, err)
	old := blobSASURI
	t.Cleanup(func() { blobSASURI = old })
	cause := errors.New("injected signing failure")
	blobSASURI = func(*storage.Blob, storage.BlobSASOptions) (string, error) { return "", cause }
	_, err = NewBlobDownload("account", "a2V5", blob).GetRequest()
	requireDownloadCode(t, errorutil.Storage_failedToGenerateSas, err)
	require.True(t, errors.Is(err, cause))
	blobSASURI = func(*storage.Blob, storage.BlobSASOptions) (string, error) { return "https://%", nil }
	_, err = NewBlobDownload("account", "a2V5", blob).GetRequest()
	requireDownloadCode(t, errorutil.FileDownload_failedToCreateBlobRequest, err)
}

func stubSASResponse(t *testing.T, status int, body io.ReadCloser, err error) {
	t.Helper()
	old := sasHTTPGet
	t.Cleanup(func() { sasHTTPGet = old })
	sasHTTPGet = func(string) (*http.Response, error) {
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: status, Status: fmt.Sprint(status), Body: body}, nil
	}
}

func TestSASDownloadClarifications(t *testing.T) {
	cause := errors.New("injected network failure")
	stubSASResponse(t, 0, nil, cause)
	_, err := GetSASBlob("https://example.invalid/container/script", "", "unused")
	requireDownloadCode(t, errorutil.FileDownload_sasRequestFailed, err)
	require.True(t, errors.Is(err, cause))
	for _, status := range []int{404, 500} {
		body := &clarificationReader{}
		stubSASResponse(t, status, body, nil)
		_, err = GetSASBlob("https://example.invalid/container/script", "", "unused")
		expected := errorutil.FileDownload_sasDownloadErrorStatus
		if status == 500 {
			expected = errorutil.Storage_sasDownloadInternalServerError
		}
		requireDownloadCode(t, expected, err)
		require.True(t, body.closed)
	}
	for _, tc := range []struct {
		uri  string
		code int
	}{
		{"https://%", errorutil.CustomerInput_invalidSasBlobUrl},
		{"https://example.invalid/", errorutil.CustomerInput_missingSasBlobFileName},
	} {
		stubSASResponse(t, 200, io.NopCloser(strings.NewReader("script")), nil)
		_, err = GetSASBlob(tc.uri, "", "unused")
		requireDownloadCode(t, tc.code, err)
	}
	stubSASResponse(t, 200, io.NopCloser(strings.NewReader("script")), nil)
	stubDownloadFile(t, nil, cause)
	_, err = GetSASBlob("https://example.invalid/container/script", "", "unused")
	requireDownloadCode(t, errorutil.FileDownload_failedToOpenSasDestination, err)
	require.True(t, errors.Is(err, cause))
	for _, reading := range []bool{false, true} {
		writer := &clarificationWriter{}
		var reader io.ReadCloser = io.NopCloser(strings.NewReader("script"))
		if reading {
			reader = &clarificationReader{err: cause}
		} else {
			writer.err = cause
		}
		stubSASResponse(t, 200, reader, nil)
		stubDownloadFile(t, writer, nil)
		_, err = GetSASBlob("https://example.invalid/container/script", "", "unused")
		requireDownloadCode(t, errorutil.FileDownload_failedToWriteSasDestination, err)
		require.True(t, errors.Is(err, cause))
		require.True(t, writer.closed)
	}
}

func TestAppendBlobClarifications(t *testing.T) {
	_, err := CreateOrReplaceAppendBlob("https://%", "")
	requireDownloadCode(t, errorutil.CustomerInput_invalidAppendBlobSasUrl, err)
	originalContainer := containerFromSAS
	originalPut := putAppendBlob
	t.Cleanup(func() { containerFromSAS = originalContainer; putAppendBlob = originalPut })
	cause := errors.New("injected container failure")
	containerFromSAS = func(url.URL) (*storage.Container, error) { return nil, cause }
	_, err = CreateOrReplaceAppendBlob("https://example.invalid/container/script", "")
	requireDownloadCode(t, errorutil.Storage_failedToGetSasContainer, err)
	require.True(t, errors.Is(err, cause))
	client, err := storage.NewClient("account", "a2V5", "core.windows.net", storage.DefaultAPIVersion, true)
	require.NoError(t, err)
	service := client.GetBlobService()
	container := service.GetContainerReference("container")
	containerFromSAS = func(url.URL) (*storage.Container, error) { return container, nil }
	_, err = CreateOrReplaceAppendBlob("https://example.invalid/container/", "")
	requireDownloadCode(t, errorutil.CustomerInput_emptyAppendBlobPath, err)
	putAppendBlob = func(*storage.Blob) error { return cause }
	_, err = CreateOrReplaceAppendBlob("https://example.invalid/container/script", "")
	requireDownloadCode(t, errorutil.Storage_failedToCreateAppendBlob, err)
	require.True(t, errors.Is(err, cause))
	_, err = getBlobPathAfterContainerName("https://%", "container")
	requireDownloadCode(t, errorutil.CustomerInput_invalidBlobPathUrl, err)
	_, err = getBlobPathAfterContainerName("https://example.invalid/other/script", "container")
	requireDownloadCode(t, errorutil.CustomerInput_missingBlobContainerPath, err)
}

type clarificationMSI struct{ err error }

func (p clarificationMSI) GetMsiForResource(string) (msi.Msi, error) {
	return msi.Msi{}, p.err
}
func (p clarificationMSI) GetMsiUsingClientId(string, string) (msi.Msi, error) {
	return msi.Msi{}, p.err
}
func (p clarificationMSI) GetMsiUsingObjectId(string, string) (msi.Msi, error) {
	return msi.Msi{}, p.err
}

func TestMSIClarifications(t *testing.T) {
	cause := errors.New("injected identity failure")
	_, err := NewBlobWithMsiDownload("https://example.invalid", func() (msi.Msi, error) {
		return msi.Msi{}, cause
	}).GetRequest()
	requireDownloadCode(t, errorutil.Msi_failedToGetDownloadToken, err)
	require.True(t, errors.Is(err, cause))
	_, err = NewBlobWithMsiDownload("https://example.invalid", func() (msi.Msi, error) {
		return msi.Msi{}, nil
	}).GetRequest()
	requireDownloadCode(t, errorutil.Msi_emptyToken, err)
	_, err = NewBlobWithMsiDownload("https://%", func() (msi.Msi, error) {
		return msi.Msi{AccessToken: "fake"}, nil
	}).GetRequest()
	requireDownloadCode(t, errorutil.CustomerInput_invalidMsiDownloadUrl, err)

	old := newMSIProvider
	t.Cleanup(func() { newMSIProvider = old })
	newMSIProvider = func() managedIdentityProvider { return clarificationMSI{cause} }
	prod := ProdMsiDownloader{}
	for _, tc := range []struct {
		provider MsiProvider
		code     int
	}{
		{prod.GetMsiProvider("blob"), errorutil.Msi_failedToGetSystemIdentity},
		{prod.GetMsiProviderByClientId("blob", "client"), errorutil.Msi_failedToGetClientIdentity},
		{prod.GetMsiProviderByObjectId("blob", "object"), errorutil.Msi_failedToGetObjectIdentity},
	} {
		_, err = tc.provider()
		requireDownloadCode(t, tc.code, err)
		require.True(t, errors.Is(err, cause))
	}
	originalMock := MockReturnErrorForMockMsiDownloader
	t.Cleanup(func() { MockReturnErrorForMockMsiDownloader = originalMock })
	MockReturnErrorForMockMsiDownloader = true
	mock := MockMsiDownloader{}
	for _, tc := range []struct {
		provider MsiProvider
		code     int
	}{
		{mock.GetMsiProvider("blob"), errorutil.Imds_mockSystemIdentityError},
		{mock.GetMsiProviderByClientId("blob", "client"), errorutil.Imds_mockClientIdentityError},
		{mock.GetMsiProviderByObjectId("blob", "object"), errorutil.Imds_mockObjectIdentityError},
	} {
		_, err = tc.provider()
		requireDownloadCode(t, tc.code, err)
	}
}

func TestDownloadBoundariesPreserveLowerClarifications(t *testing.T) {
	pointer := vmextension.NewErrorWithClarificationPtr(1998, errors.New("injected classified failure"))
	for _, cause := range []error{pointer, *pointer} {
		t.Run(fmt.Sprintf("%T", cause), func(t *testing.T) {
			oldHTTP, oldClient := MakeHttpRequest, httpClient
			oldSAS, oldSigner, oldContainer, oldPut := sasHTTPGet, blobSASURI, containerFromSAS, putAppendBlob
			oldStorage, oldMSI := newStorageClient, newMSIProvider
			t.Cleanup(func() {
				MakeHttpRequest, httpClient = oldHTTP, oldClient
				sasHTTPGet, blobSASURI, containerFromSAS, putAppendBlob = oldSAS, oldSigner, oldContainer, oldPut
				newStorageClient, newMSIProvider = oldStorage, oldMSI
			})
			check := func(err error) {
				t.Helper()
				requireDownloadCode(t, 1998, err)
				require.True(t, errors.Is(err, cause))
			}
			_, _, err := Download(clarificationLog(), clarificationDownloader{cause})
			check(err)
			MakeHttpRequest = func(*http.Request) (*http.Response, error) { return nil, cause }
			_, _, err = Download(clarificationLog(), NewURLDownload("https://example.invalid"))
			check(err)
			httpClient = &http.Client{Transport: clarificationTransport(func(*http.Request) (*http.Response, error) {
				return nil, cause
			})}
			request, err := http.NewRequest(http.MethodGet, "https://example.invalid", nil)
			require.NoError(t, err)
			_, err = HttpClientDo(request)
			check(err)

			stubDownloadFile(t, nil, cause)
			_, err = SaveTo(clarificationLog(), nil, "script", 0500)
			check(err)
			sasHTTPGet = func(string) (*http.Response, error) { return nil, cause }
			_, err = GetSASBlob("https://example.invalid/container/script", "", "unused")
			check(err)
			stubSASResponse(t, 200, io.NopCloser(strings.NewReader("script")), nil)
			_, err = GetSASBlob("https://example.invalid/container/script", "", "unused")
			check(err)

			writer := &clarificationWriter{}
			stubDownloadFile(t, writer, nil)
			stubSASResponse(t, 200, &clarificationReader{err: cause}, nil)
			_, err = GetSASBlob("https://example.invalid/container/script", "", "unused")
			check(err)
			stubDownloadResponse(t, 200, &clarificationReader{err: cause})
			_, err = SaveTo(clarificationLog(), []Downloader{NewURLDownload("https://example.invalid")}, "script", 0500)
			check(err)

			newStorageClient = func(string, string, string, string, bool) (storage.Client, error) {
				return storage.Client{}, cause
			}
			blob := blobutil.AzureBlobRef{StorageBase: "core.windows.net", Container: "container", Blob: "script"}
			_, err = NewBlobDownload("account", "a2V5", blob).GetRequest()
			check(err)
			newStorageClient = oldStorage
			blobSASURI = func(*storage.Blob, storage.BlobSASOptions) (string, error) { return "", cause }
			_, err = NewBlobDownload("account", "a2V5", blob).GetRequest()
			check(err)

			containerFromSAS = func(url.URL) (*storage.Container, error) { return nil, cause }
			_, err = CreateOrReplaceAppendBlob("https://example.invalid/container/script", "")
			check(err)
			client, err := storage.NewClient("account", "a2V5", "core.windows.net", storage.DefaultAPIVersion, true)
			require.NoError(t, err)
			service := client.GetBlobService()
			containerFromSAS = func(url.URL) (*storage.Container, error) {
				return service.GetContainerReference("container"), nil
			}
			putAppendBlob = func(*storage.Blob) error { return cause }
			_, err = CreateOrReplaceAppendBlob("https://example.invalid/container/script", "")
			check(err)
			_, err = NewBlobWithMsiDownload("https://example.invalid", func() (msi.Msi, error) {
				return msi.Msi{}, cause
			}).GetRequest()
			check(err)
			newMSIProvider = func() managedIdentityProvider { return clarificationMSI{cause} }
			prod := ProdMsiDownloader{}
			for _, provider := range []MsiProvider{
				prod.GetMsiProvider("blob"), prod.GetMsiProviderByClientId("blob", "client"),
				prod.GetMsiProviderByObjectId("blob", "object"),
			} {
				_, err = provider()
				check(err)
			}
		})
	}
}

func TestDownloadInjectedSuccess(t *testing.T) {
	stubDownloadResponse(t, 200, io.NopCloser(strings.NewReader("script")))
	writer := &clarificationWriter{}
	stubDownloadFile(t, writer, nil)
	n, err := SaveTo(clarificationLog(), []Downloader{NewURLDownload("https://example.invalid")}, "script", 0500)
	require.NoError(t, err)
	require.EqualValues(t, 6, n)
	require.Equal(t, "script", writer.String())
	require.True(t, writer.closed)
	stubSASResponse(t, 200, io.NopCloser(strings.NewReader("sas script")), nil)
	writer = &clarificationWriter{}
	stubDownloadFile(t, writer, nil)
	path, err := GetSASBlob("https://example.invalid/container/script", "", "unused")
	require.NoError(t, err)
	require.NotEmpty(t, path)
	require.Equal(t, "sas script", writer.String())
	require.True(t, writer.closed)
}
