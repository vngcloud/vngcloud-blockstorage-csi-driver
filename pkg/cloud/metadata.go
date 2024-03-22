package cloud

import (
	"k8s.io/klog/v2"
	"sync"
)

var (
	metadataInsOnce sync.Once
	metadataIns     MetadataService
)

type (
	Metadata struct {
		UUID      string           `json:"uuid"`
		Name      string           `json:"name"`
		ProjectID string           `json:"project_id"`
		Devices   []DeviceMetadata `json:"devices,omitempty"`
	}

	VServerMetadata struct {
		UUID             string           `json:"uuid"`
		Name             string           `json:"name"`
		AvailabilityZone string           `json:"availability_zone"`
		ProjectID        string           `json:"project_id"`
		Devices          []DeviceMetadata `json:"devices,omitempty"`
		Meta             Meta             `json:"meta,omitempty"`
		// ... and other fields we don't care about.  Expand as necessary.
	}

	DeviceMetadata struct {
		Type    string `json:"type"`
		Bus     string `json:"bus,omitempty"`
		Serial  string `json:"serial,omitempty"`
		Address string `json:"address,omitempty"`
		// ... and other fields.
	}

	Meta struct {
		Product    string `json:"product,omitempty"`
		PortalUUID string `json:"portal_uuid,omitempty"`
		ProjectID  string `json:"project_id,omitempty"`
	}
)

func (s *Metadata) GetInstanceID() string {
	return s.UUID
}

func (s *Metadata) GetProjectID() string {
	return s.ProjectID
}

func (s *VServerMetadata) GetMetadata() *VServerMetadata {
	return s
}

func NewMetadataService(vserverMetadataClient VServerMetadataClient) (MetadataService, error) {
	klog.InfoS("retrieving instance data from vserver metadata")
	svc, err := vserverMetadataClient()
	if err != nil {
		klog.InfoS("error creating vServer metadata client", "err", err)
		return nil, err
	}

	return VServerMetadataInstanceInfo(svc)
}
