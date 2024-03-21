package vcontainer

import (
	"sync"
	"time"
)

var (
	vcontainerIns     IVContainer
	vcontainerInsOnce sync.Once
)

var (
	configFiles = []string{"/etc/config/vcontainer-csi-config.conf"}
)

const (
	defaultPageSize          = 10
	defaultFirstPage         = 1
	defaultVolumeDescription = "vcontainer_csi_blockstorage"

	diskAttachInitDelay = 30 * time.Second
	diskAttachSteps     = 15
	diskAttachFactor    = 1.2

	diskDetachInitDelay = 3 * time.Second
	diskDetachFactor    = 1.2
	diskDetachSteps     = 13

	operationFinishInitDelay = 30 * time.Second
	operationFinishFactor    = 1.2
	operationFinishSteps     = 15

	snapshotReadyStatus = "active"
	snapReadyDuration   = 1 * time.Second
	snapReadyFactor     = 1.2
	snapReadySteps      = 10

	VolumeAvailableStatus = "AVAILABLE"
	VolumeInUseStatus     = "IN-USE"

	defaultMaxVolAttachLimit = 256
)
