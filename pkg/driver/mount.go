package driver

import (
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/mounter"
	mountutils "k8s.io/mount-utils"
)

type Mounter interface{}

type DeviceIdentifier interface {
}

// NodeMounter implements Mounter.
// A superstruct of SafeFormatAndMount.
type NodeMounter struct {
	*mountutils.SafeFormatAndMount
}

type nodeDeviceIdentifier struct{}

func newNodeMounter() (Mounter, error) {
	// mounter.NewSafeMounter returns a SafeFormatAndMount
	safeMounter, err := mounter.NewSafeMounter()
	if err != nil {
		return nil, err
	}
	return &NodeMounter{safeMounter}, nil
}

func newNodeDeviceIdentifier() DeviceIdentifier {
	return &nodeDeviceIdentifier{}
}
