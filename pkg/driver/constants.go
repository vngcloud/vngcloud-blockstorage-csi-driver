package driver

import (
	ltime "time"

	lcsi "github.com/container-storage-interface/spec/lib/go/csi"
	lwait "k8s.io/apimachinery/pkg/util/wait"

	lscloud "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/cloud"
)

const (
	DefaultCSIEndpoint                       = "unix://tmp/csi.sock"
	DefaultModifyVolumeRequestHandlerTimeout = 30 * ltime.Second
	AgentNotReadyNodeTaintKey                = "csi.vngcloud.vn/agent-not-ready"

	DefaultTimeoutModifyChannel = 10 * ltime.Minute
	WellKnownZoneTopologyKey    = "topology.kubernetes.io/zone"
	DriverName                  = "bs.csi.vngcloud.vn"
	ZoneTopologyKey             = "topology." + DriverName + "/zone"
)

// constants of disk partition suffix
const (
	diskPartitionSuffix     = ""
	nvmeDiskPartitionSuffix = "p"

	operationFinishInitDelay = 1 * ltime.Second
	operationFinishFactor    = 1.1
	operationFinishSteps     = 15

	defaultFsType = FSTypeExt4
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

	AnnotationModificationKeyVolumeType = "volume-type"
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

	// nodeCaps represents the capability of node service.
	nodeCaps = []lcsi.NodeServiceCapability_RPC_Type{
		lcsi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
		lcsi.NodeServiceCapability_RPC_EXPAND_VOLUME,
		lcsi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
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

var (
	// NewMetadataFunc is a variable for the cloud.NewMetadata function that can
	// be overwritten in unit tests.
	NewMetadataFunc = lscloud.NewMetadataService
	NewCloudFunc    = lscloud.NewCloud
)

var (
	// controllerCaps represents the capability of controller service
	controllerCaps = []lcsi.ControllerServiceCapability_RPC_Type{
		lcsi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		lcsi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
		lcsi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT,
		lcsi.ControllerServiceCapability_RPC_LIST_SNAPSHOTS,
		lcsi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
		lcsi.ControllerServiceCapability_RPC_MODIFY_VOLUME,
	}
)

var (
	// taintRemovalInitialDelay is the initial delay for node taint removal
	taintRemovalInitialDelay = 1 * ltime.Second

	// taintRemovalBackoff is the exponential backoff configuration for node taint removal
	taintRemovalBackoff = lwait.Backoff{
		Duration: 500 * ltime.Millisecond,
		Factor:   2,
		Steps:    10, // Max delay = 0.5 * 2^9 = ~4 minutes
	}
)

var (
	ValidFSTypes = map[string]struct{}{
		FSTypeExt2: {},
		FSTypeExt3: {},
		FSTypeExt4: {},
		FSTypeXfs:  {},
		FSTypeNtfs: {},
	}
)
