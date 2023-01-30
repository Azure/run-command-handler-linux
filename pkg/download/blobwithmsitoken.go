package download

import (
	"fmt"
	"net/http"
	url2 "net/url"
	"strings"

	"github.com/Azure/azure-extension-foundation/httputil"
	"github.com/Azure/azure-extension-foundation/msi"
	"github.com/pkg/errors"
)

const (
	xMsVersionHeaderName = "x-ms-version"
	xMsVersionValue      = "2018-03-28"
	storageResourceName  = "https://storage.azure.com/"
)

var azureBlobDomains = map[string]interface{}{ // golang doesn't have builtin hash sets, so this is a workaround for that
	".blob.core.":       nil,
	".blob.azurestack.": nil,
}

type blobWithMsiToken struct {
	url         string
	msiProvider MsiProvider
}

type MsiProvider func() (msi.Msi, error)

type MsiDownloader interface {
	GetMsiProvider(blobUri string) MsiProvider
	GetMsiProviderByClientId(blobUri, clientId string) MsiProvider
	GetMsiProviderByObjectId(blobUri, objectId string) MsiProvider
}

type ProdMsiDownloader struct{}

type MockMsiDownloader struct{} // Used only for test

var MockReturnErrorForMockMsiDownloader = false // Used only for test

func (self *blobWithMsiToken) GetRequest() (*http.Request, error) {
	msi, err := self.msiProvider()
	if err != nil {
		return nil, err
	}
	if msi.AccessToken == "" {
		return nil, errors.New("MSI token is empty")
	}

	request, err := http.NewRequest(http.MethodGet, self.url, nil)
	if err != nil {
		return nil, err
	}

	if IsAzureStorageBlobUri(self.url) {
		request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", msi.AccessToken))
		request.Header.Set(xMsVersionHeaderName, xMsVersionValue)
	}
	return request, nil
}

func NewBlobWithMsiDownload(url string, msiProvider MsiProvider) Downloader {
	return &blobWithMsiToken{url, msiProvider}
}

// Uses system identity to get Msi token
func (prodMsiDownloader ProdMsiDownloader) GetMsiProvider(blobUri string) MsiProvider {
	msiProvider := msi.NewMsiProvider(httputil.NewSecureHttpClient(httputil.DefaultRetryBehavior))
	return func() (msi.Msi, error) {
		msi, err := msiProvider.GetMsiForResource(GetResourceNameFromBlobUri(blobUri))
		if err != nil {
			return msi, errors.Wrapf(err, "Unable to get managed identity. "+
				"Please make sure that system assigned managed identity is enabled on the VM "+
				"or user assigned identity is added to the system.")
		}
		return msi, nil
	}
}

// Mock implementation of GetMsiProvider
func (mockMsiDownloader MockMsiDownloader) GetMsiProvider(blobUri string) MsiProvider {
	return func() (msi.Msi, error) {
		mockMsi := msi.Msi{
			AccessToken: "uwsihdiuhiuasdfui*(*(&90790asofhdioas",
			Resource:    "Msi by System Identity for blob " + blobUri,
		}
		if MockReturnErrorForMockMsiDownloader {
			return mockMsi, errors.New("Error getting msi")
		} else {
			return mockMsi, nil
		}

	}
}

// Get Msi token by clientId
func (prodMsiDownloader ProdMsiDownloader) GetMsiProviderByClientId(blobUri, clientId string) MsiProvider {
	msiProvider := msi.NewMsiProvider(httputil.NewSecureHttpClient(httputil.DefaultRetryBehavior))
	return func() (msi.Msi, error) {
		msi, err := msiProvider.GetMsiUsingClientId(clientId, GetResourceNameFromBlobUri(blobUri))
		if err != nil {
			return msi, errors.Wrapf(err, "Unable to get managed identity with client id %s. "+
				"Please make sure that the user assigned managed identity is added to the VM ", clientId)
		}
		return msi, nil
	}
}

// Mock implementation of GetMsiProviderByClientId
func (mockMsiDownloader MockMsiDownloader) GetMsiProviderByClientId(blobUri string, clientId string) MsiProvider {
	return func() (msi.Msi, error) {
		mockMsi := msi.Msi{
			AccessToken: "uwsihdiuhiuasdfui*(*(&90790asofhdioas",
			Resource:    "Msi by clientId for blob " + blobUri,
		}
		if MockReturnErrorForMockMsiDownloader {
			return mockMsi, errors.New("Error getting msi")
		} else {
			return mockMsi, nil
		}
	}
}

// Get Msi token by objectId
func (prodMsiDownloader ProdMsiDownloader) GetMsiProviderByObjectId(blobUri, objectId string) MsiProvider {
	msiProvider := msi.NewMsiProvider(httputil.NewSecureHttpClient(httputil.DefaultRetryBehavior))
	return func() (msi.Msi, error) {
		msi, err := msiProvider.GetMsiUsingObjectId(objectId, GetResourceNameFromBlobUri(blobUri))
		if err != nil {
			return msi, errors.Wrapf(err, "Unable to get managed identity with object id %s. "+
				"Please make sure that the user assigned managed identity is added to the VM ", objectId)
		}
		return msi, nil
	}
}

// Mock implementation of GetMsiProviderByObjectId
func (mockMsiDownloader MockMsiDownloader) GetMsiProviderByObjectId(blobUri, objectId string) MsiProvider {
	return func() (msi.Msi, error) {
		mockMsi := msi.Msi{
			AccessToken: "uwsihdiuhiuasdfui*(*(&90790asofhdioas",
			Resource:    "Msi by objectId for blob " + blobUri,
		}
		if MockReturnErrorForMockMsiDownloader {
			return mockMsi, errors.New("Error getting msi")
		} else {
			return mockMsi, nil
		}
	}
}

func GetResourceNameFromBlobUri(uri string) string {
	// TODO: update this function as sovereign cloud blob resource strings become available
	// resource string for getting MSI for azure storage is still https://storage.azure.com/ for sovereign regions but it is expected to change
	return storageResourceName
}

func IsAzureStorageBlobUri(url string) bool {
	parsedUrl, err := url2.Parse(url)
	if err != nil {
		return false
	}

	host := parsedUrl.Host

	for validBlobDomain := range azureBlobDomains {
		if strings.Contains(host, validBlobDomain) {
			return true
		}
	}

	return false
}
