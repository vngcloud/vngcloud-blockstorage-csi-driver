package driver

import (
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/cloud"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/driver/internal"
)

// nodeService represents the node service of CSI driver
type nodeService struct {
	metadata         cloud.MetadataService
	mounter          Mounter
	deviceIdentifier DeviceIdentifier
	inFlight         *internal.InFlight
	driverOptions    *DriverOptions
}
