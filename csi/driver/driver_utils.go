package driver

import (
	"fmt"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/csi/utils/metadata"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/csi/utils/mount"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/csi/vcontainer/vcontainer"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/csi/version"
	obj "github.com/vngcloud/vngcloud-go-sdk/vngcloud/objects"
	"k8s.io/klog/v2"
)

// *************************************************** PUBLIC METHODS **************************************************

func NewDriver(endpoint string) *Driver {
	driver := new(Driver)
	driver.name = driverName
	driver.endpoint = endpoint
	driver.fqVersion = fmt.Sprintf("%s@%s", Version, version.Version)

	klog.Info("Driver: ", driver.name)
	klog.Info("Driver version: ", driver.fqVersion)
	klog.Info("CSI Spec version: ", specVersion)

	driver.AddControllerServiceCapabilities(
		[]csi.ControllerServiceCapability_RPC_Type{
			csi.ControllerServiceCapability_RPC_LIST_VOLUMES,
			csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
			csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
			csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
			csi.ControllerServiceCapability_RPC_CLONE_VOLUME,
			csi.ControllerServiceCapability_RPC_LIST_VOLUMES_PUBLISHED_NODES,
			csi.ControllerServiceCapability_RPC_GET_VOLUME,
		})
	driver.AddVolumeCapabilityAccessModes(
		[]csi.VolumeCapability_AccessMode_Mode{
			csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		})

	driver.AddNodeServiceCapabilities(
		[]csi.NodeServiceCapability_RPC_Type{
			csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
			csi.NodeServiceCapability_RPC_EXPAND_VOLUME,
			csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
		})

	return driver
}

func NewControllerServiceCapability(cap csi.ControllerServiceCapability_RPC_Type) *csi.ControllerServiceCapability {
	return &csi.ControllerServiceCapability{
		Type: &csi.ControllerServiceCapability_Rpc{
			Rpc: &csi.ControllerServiceCapability_RPC{
				Type: cap,
			},
		},
	}
}

func NewVolumeCapabilityAccessMode(mode csi.VolumeCapability_AccessMode_Mode) *csi.VolumeCapability_AccessMode {
	return &csi.VolumeCapability_AccessMode{Mode: mode}
}

func NewNodeServiceCapability(cap csi.NodeServiceCapability_RPC_Type) *csi.NodeServiceCapability {
	return &csi.NodeServiceCapability{
		Type: &csi.NodeServiceCapability_Rpc{
			Rpc: &csi.NodeServiceCapability_RPC{
				Type: cap,
			},
		},
	}
}

func NewIdentityServer(d *Driver) csi.IdentityServer {
	return &identityServer{
		Driver: d,
	}
}

func NewControllerServer(d *Driver, metadata metadata.IMetadata, cloud vcontainer.IVContainer) csi.ControllerServer {
	return &controllerServer{
		Driver:   d,
		Metadata: metadata,
		Cloud:    cloud,
	}
}

func NewNodeServer(d *Driver, mounter mount.IMount, metadator metadata.IMetadata, cloud vcontainer.IVContainer) csi.NodeServer {
	return &nodeServer{
		Driver:   d,
		Mount:    mounter,
		Metadata: metadator,
		Cloud:    cloud,
	}
}

func getCreateVolumeResponse(vol *obj.Volume) *csi.CreateVolumeResponse {
	var volsrc *csi.VolumeContentSource
	resp := &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      vol.VolumeId,
			CapacityBytes: int64(vol.Size * 1024 * 1024 * 1024),
			ContentSource: volsrc,
		},
	}

	return resp

}

func isAttachment(vmId *string) bool {
	if vmId == nil {
		return false
	}
	return true
}

func getDevicePath(volumeID string, m mount.IMount) (string, error) {
	var devicePath string
	devicePath, err := m.GetDevicePath(volumeID)
	if err != nil {
		klog.Warningf("Couldn't get device path from mount: %v", err)
	}

	if devicePath == "" {
		// try to get from metadata service
		klog.Info("Trying to get device path from metadata service")
		devicePath, err = m.GetDevicePath(volumeID)
		if err != nil {
			klog.Errorf("Couldn't get device path from metadata service: %v", err)
			return "", fmt.Errorf("couldn't get device path from metadata service: %v", err)
		}
	}

	return devicePath, nil
}

func collectMountOptions(fsType string, mntFlags []string) []string {
	var options []string
	options = append(options, mntFlags...)

	// By default, xfs does not allow mounting of two volumes with the same filesystem uuid.
	// Force ignore this uuid to be able to mount volume + its clone / restored snapshot on the same node.
	if fsType == "xfs" {
		options = append(options, "nouuid")
	}
	return options
}
