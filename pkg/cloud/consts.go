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
)

const (
	waitVolumeActiveTimeout = 5 * time.Minute
	waitVolumeActiveDelay   = 10
	waitVolumeActiveSteps   = 7

	waitVolumeDetachTimeout = 7 * time.Minute
	waitVolumeDetachDelay   = 10
	waitVolumeDetachSteps   = 7

	waitVolumeAttachTimeout = 7 * time.Minute
	waitVolumeAttachDelay   = 10
	waitVolumeAttachSteps   = 7
)

const (
	VolumeAvailableStatus = "AVAILABLE"
	VolumeInUseStatus     = "IN-USE"
)

var (
	errSetDetachIngore = lset.NewSet[lsdkErr.ErrorCode](lsdkEH.ErrCodeVolumeAvailable, lsdkEH.ErrCodeVolumeNotFound)
)
