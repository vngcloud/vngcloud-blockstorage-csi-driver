package cloud

import (
	"k8s.io/klog/v2"
	"sync"
)

var (
	metadataInsOnce sync.Once
	metadataIns     MetadataService
)

func NewMetadataService(vserverMetadataClient VServerMetadataClient) (MetadataService, error) {
	klog.InfoS("retrieving instance data from vserver metadata")
	svc, err := vserverMetadataClient()
	if !svc.IsAvailable() {
		return nil, err
	}
}
