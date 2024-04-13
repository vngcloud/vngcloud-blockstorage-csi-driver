package cloud

import (
	lset "github.com/cuongpiger/joat/data-structure/set"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/util"
	lsdkErr "github.com/vngcloud/vngcloud-go-sdk/error"
	lsdkEH "github.com/vngcloud/vngcloud-go-sdk/vngcloud/errors"
	"time"
)

const (
	VksClusterIdTagKey    = "vks-cluster-id"
	VksPvcNamespaceTagKey = "vks-namespace"
	VksPvNameTagKey       = "vks-pv-name"
	VksPvcNameTagKey      = "vks-pvc-name"
)

const (
	DefaultDiskSymbolIdLength = 20

	// DefaultVolumeSize represents the default volume size.
	DefaultVolumeSize int64 = 20 * util.GiB

	defaultPage     = 1
	defaultPageSize = 100
)

const (
	waitVolumeActiveTimeout = 5 * time.Minute
	waitVolumeActiveDelay   = 10
	waitVolumeActiveSteps   = 5

	waitVolumeDetachTimeout = 7 * time.Minute
	waitVolumeDetachDelay   = 10
	waitVolumeDetachSteps   = 5

	waitVolumeAttachTimeout = 7 * time.Minute
	waitVolumeAttachDelay   = 10
	waitVolumeAttachSteps   = 5

	waitSnapshotActiveTimeout = 5 * time.Minute
	waitSnapshotActiveDelay   = 10
	waitSnapshotActiveSteps   = 5
)

const (
	VolumeAvailableStatus = "AVAILABLE"
	VolumeInUseStatus     = "IN-USE"

	SnapshotActiveStatus = "ACTIVE"
)

var (
	errSetDetachIngore = lset.NewSet[lsdkErr.ErrorCode](lsdkEH.ErrCodeVolumeAvailable, lsdkEH.ErrCodeVolumeNotFound)
)
