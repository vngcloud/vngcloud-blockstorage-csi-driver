package cloud

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

const (
	metadataURLTemplate    = "http://169.254.169.254/openstack/%s/meta_data.json"
	defaultMetadataVersion = "latest"
)

var (
	vServerMetadataCache *VServerMetadata
	metadataCache        *Metadata
	ErrBadMetadata       = errors.New("invalid vServer metadata because of got empty uuid")
	ErrMetadataEmpty     = errors.New("metadata is empty")
)

type VServerMetadataClient func() (IVServerMetadata, error)

var DefaultVServerMetadataClient = func() (IVServerMetadata, error) {
	// Try to get JSON from metadata server.
	metadataURL := getMetadataURL(defaultMetadataVersion)
	resp, err := noProxyHTTPClient().Get(metadataURL)
	if err != nil {
		return nil, fmt.Errorf("getFromMetadataService; error fetching metadata from URL %s; ERR: %v", metadataURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("getFromMetadataService; unexpected status code when reading metadata from %s with response status code %s", metadataURL, resp.Status)
		return nil, err
	}

	return parseMetadata(resp.Body)
}

func getMetadataURL(metadataVersion string) string {
	return fmt.Sprintf(metadataURLTemplate, metadataVersion)
}

func noProxyHTTPClient() *http.Client {
	noProxyTransport := http.DefaultTransport.(*http.Transport).Clone()
	noProxyTransport.Proxy = nil
	return &http.Client{Transport: noProxyTransport}
}

func parseMetadata(r io.Reader) (*VServerMetadata, error) {
	if vServerMetadataCache == nil {
		var metadata VServerMetadata
		jsonDecoder := json.NewDecoder(r)
		if err := jsonDecoder.Decode(&metadata); err != nil {
			return nil, err
		}

		if metadata.Meta.PortalUUID == "" {
			return nil, ErrBadMetadata
		}

		vServerMetadataCache = &metadata
	}

	return vServerMetadataCache, nil
}

func VServerMetadataInstanceInfo(psvc IVServerMetadata) (MetadataService, error) {
	if metadataCache != nil {
		return metadataCache, nil
	}

	instanceInfo := psvc.GetMetadata()
	if instanceInfo == nil {
		return nil, ErrMetadataEmpty
	}

	metadataCache = &Metadata{}
	metadataCache.UUID = instanceInfo.Meta.PortalUUID
	metadataCache.Name = instanceInfo.Name
	metadataCache.ProjectID = instanceInfo.ProjectID
	metadataCache.AvailabilityZone = instanceInfo.AvailabilityZone

	return metadataCache, nil
}
