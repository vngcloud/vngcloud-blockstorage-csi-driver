package driver

import (
	lcodes "google.golang.org/grpc/codes"
	lstt "google.golang.org/grpc/status"
)

var (
	ErrVolumeNameNotProvided           = lstt.Error(lcodes.InvalidArgument, "Volume name not provided")
	ErrVolumeCapabilitiesNotProvided   = lstt.Error(lcodes.InvalidArgument, "Volume capabilities not provided")
	ErrVolumeCapabilitiesNotSupported  = lstt.Error(lcodes.InvalidArgument, "Volume capabilities not supported")
	ErrVolumeSizeExceedLimit           = lstt.Error(lcodes.InvalidArgument, "After round-up, volume size exceeds the limit specified")
	ErrParsingVolumeSize               = lstt.Errorf(lcodes.InvalidArgument, "Could not parse volume size")
	ErrModifyMutableParam              = lstt.Errorf(lcodes.InvalidArgument, "Invalid mutable parameters")
	ErrVolumeIDNotProvided             = lstt.Errorf(lcodes.InvalidArgument, "Volume ID not provided")
	ErrNodeIdNotProvided               = lstt.Error(lcodes.InvalidArgument, "Node ID not provided")
	ErrStagingTargetPathNotProvided    = lstt.Error(lcodes.InvalidArgument, "Staging target not provided")
	ErrVolumeContentSourceNotSupported = lstt.Error(lcodes.InvalidArgument, "Unsupported volumeContentSource type")
	ErrSnapshotIsNil                   = lstt.Error(lcodes.InvalidArgument, "Error retrieving snapshot from the volumeContentSource")
	ErrSnapshotNameNotProvided         = lstt.Error(lcodes.InvalidArgument, "Snapshot name not provided")
	ErrSnapshotSourceVolumeNotProvided = lstt.Error(lcodes.InvalidArgument, "Snapshot volume source ID not provided")

	ErrCanNotParseRequestArguments = func(pargument, pvalue string) error {
		return lstt.Errorf(lcodes.InvalidArgument, "Could not parse %s (%s)", pargument, pvalue)
	}
)

var (
	ErrVolumeIsCreating = func(pvolumeID string) error {
		return lstt.Errorf(lcodes.Aborted, "Create volume request for %s is already in progress", pvolumeID)
	}

	ErrOperationAlreadyExists = func(pvolumeID string) error {
		return lstt.Errorf(lcodes.Aborted, "An operation with the given volume %s already exists", pvolumeID)
	}
)

var (
	ErrAttachVolume = func(pvolumeID, pnodeID string) error {
		return lstt.Errorf(lcodes.Internal, "CANNOT Attach volume %s to node %s", pvolumeID, pnodeID)
	}

	ErrDetachVolume = func(pvolumeID, pnodeID string) error {
		return lstt.Errorf(lcodes.Internal, "CANNOT Detach volume %s from node %s", pvolumeID, pnodeID)
	}

	ErrFailedToGetDevicePath = func(pvolumeID, pnodeID string) error {
		return lstt.Errorf(lcodes.Internal, "Failed to get device path for volume %s on node %s", pvolumeID, pnodeID)
	}
)
