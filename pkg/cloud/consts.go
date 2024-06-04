package cloud

import (
	ltime "time"

	lset "github.com/cuongpiger/joat/data-structure/set"
	lsdkErrs "github.com/vngcloud/vngcloud-go-sdk/v2/vngcloud/sdk_error"

	lsutil "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/util"
)

const (
	VksClusterIdTagKey    = "vks-cluster-id"
	VksPvcNamespaceTagKey = "vks-namespace"
	VksPvNameTagKey       = "vks-pv-name"
	VksPvcNameTagKey      = "vks-pvc-name"
	VksSnapshotIdTagKey   = "vks-snapshot-id"
)

const (
	// DefaultVolumeSize represents the default volume size.
	DefaultVolumeSize int64 = 5 * lsutil.GiB
)

const (
	waitVolumeActiveTimeout = 5 * ltime.Minute
	waitVolumeActiveDelay   = 10
	waitVolumeActiveSteps   = 5

	waitVolumeAttachTimeout = 7 * ltime.Minute
	waitVolumeAttachDelay   = 10
	waitVolumeAttachSteps   = 5

	waitSnapshotActiveTimeout = 5 * ltime.Minute
	waitSnapshotActiveDelay   = 10
	waitSnapshotActiveSteps   = 5
)

const (
	VolumeAvailableStatus = "AVAILABLE"
	VolumeInUseStatus     = "IN-USE"
	VolumeCreatingStatus  = "CREATING"
	VolumeErrorStatus     = "ERROR"

	SnapshotActiveStatus = "ACTIVE"
)

var (
	errSetDetachIngore = lset.NewSet[lsdkErrs.ErrorCode](lsdkErrs.EcVServerVolumeNotFound, lsdkErrs.EcVServerVolumeAvailable)
)

var (
	volumeArchivedStatus  = lset.NewSet[string](VolumeAvailableStatus, VolumeInUseStatus)
	volumeAvailableStatus = lset.NewSet[string](VolumeAvailableStatus)
)

const (
	patternSnapshotDescription = "Snapshot of PersistentVolume %s for vKS cluster %s"
)
