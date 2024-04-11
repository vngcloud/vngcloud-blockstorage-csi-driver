package driver

import (
	ltime "time"

	lcsi "github.com/container-storage-interface/spec/lib/go/csi"
)

const (
	DefaultCSIEndpoint                       = "unix://tmp/csi.sock"
	DefaultModifyVolumeRequestHandlerTimeout = 30 * ltime.Second
	AgentNotReadyNodeTaintKey                = "bs.csi.vngcloud.vn/agent-not-ready"
)

const (
	volumeCreatingInProgress = "Create volume request for %s is already in progress"
	// VolumeOperationAlreadyExists is message fmt returned to CO when there is another in-flight call on the given volumeID
	volumeOperationAlreadyExists = "An operation with the given volume=%q is already in progress"
)

// constants of disk partition suffix
const (
	diskPartitionSuffix     = ""
	nvmeDiskPartitionSuffix = "p"
)

// constants for fstypes
const (
	// FSTypeExt2 represents the ext2 filesystem type
	FSTypeExt2 = "ext2"
	// FSTypeExt3 represents the ext3 filesystem type
	FSTypeExt3 = "ext3"
	// FSTypeExt4 represents the ext4 filesystem type
	FSTypeExt4 = "ext4"
	// FSTypeXfs represents the xfs filesystem type
	FSTypeXfs = "xfs"
	// FSTypeNtfs represents the ntfs filesystem type
	FSTypeNtfs = "ntfs"
)

// constants of keys in volume parameters
const (
	// VolumeTypeKey represents key for volume type
	VolumeTypeKey = "type"

	// EncryptedKey represents key for whether filesystem is encrypted
	EncryptedKey = "encrypted"

	// PVCNameKey contains name of the PVC for which is a volume provisioned.
	PVCNameKey = "csi.storage.k8s.io/pvc/name"

	// PVCNamespaceKey contains namespace of the PVC for which is a volume provisioned.
	PVCNamespaceKey = "csi.storage.k8s.io/pvc/namespace"

	// PVNameKey contains name of the final PV that will be used for the dynamically provisioned volume
	PVNameKey = "csi.storage.k8s.io/pv/name"

	// BlockSizeKey configures the block size when formatting a volume
	BlockSizeKey = "blocksize"

	// InodeSizeKey configures the inode size when formatting a volume
	InodeSizeKey = "inodesize"

	// BytesPerInodeKey configures the `bytes-per-inode` when formatting a volume
	BytesPerInodeKey = "bytesperinode"

	// NumberOfInodesKey configures the `number-of-inodes` when formatting a volume
	NumberOfInodesKey = "numberofinodes"

	// Ext4BigAllocKey enables the bigalloc option when formatting an ext4 volume
	Ext4BigAllocKey = "ext4bigalloc"

	// Ext4ClusterSizeKey configures the cluster size when formatting an ext4 volume with the bigalloc option enabled
	Ext4ClusterSizeKey = "ext4clustersize"

	// IsPoc is a key to determine if the volume is a POC volume
	IsPoc = "ispoc"
)

var (
	FileSystemConfigs = map[string]fileSystemConfig{
		FSTypeExt2: {
			NotSupportedParams: map[string]struct{}{
				Ext4BigAllocKey:    {},
				Ext4ClusterSizeKey: {},
			},
		},
		FSTypeExt3: {
			NotSupportedParams: map[string]struct{}{
				Ext4BigAllocKey:    {},
				Ext4ClusterSizeKey: {},
			},
		},
		FSTypeExt4: {
			NotSupportedParams: map[string]struct{}{},
		},
		FSTypeXfs: {
			NotSupportedParams: map[string]struct{}{
				BytesPerInodeKey:   {},
				NumberOfInodesKey:  {},
				Ext4BigAllocKey:    {},
				Ext4ClusterSizeKey: {},
			},
		},
	}
)

const (
	// DevicePathKey represents key for device path in PublishContext, devicePath is the device path where the volume is attached to
	DevicePathKey = "devicePath"

	// VolumeAttributePartition represents key for partition config in VolumeContext
	// this represents the partition number on a device used to mount
	VolumeAttributePartition = "partition"
)

// Supported access modes
const (
	SingleNodeWriter     = lcsi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	MultiNodeMultiWriter = lcsi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER
)
