package driver

import (
	lfmt "fmt"
	lcodes "google.golang.org/grpc/codes"
	lstt "google.golang.org/grpc/status"
)

var (
	ErrVolumeNameNotProvided           = lstt.Error(lcodes.InvalidArgument, "Volume name not provided")
	ErrVolumeCapabilitiesNotProvided   = lstt.Error(lcodes.InvalidArgument, "Volume capabilities not provided")
	ErrVolumeCapabilitiesNotSupported  = lstt.Error(lcodes.InvalidArgument, "Volume capabilities not supported")
	ErrCapacityRangeNotProvided        = lstt.Error(lcodes.InvalidArgument, "Capacity range is required")
	ErrVolumeSizeExceedLimit           = lstt.Error(lcodes.InvalidArgument, "After round-up, volume size exceeds the limit specified")
	ErrParsingVolumeSize               = lstt.Error(lcodes.InvalidArgument, "Could not parse volume size")
	ErrModifyMutableParam              = lstt.Error(lcodes.InvalidArgument, "Invalid mutable parameters")
	ErrVolumeIDNotProvided             = lstt.Error(lcodes.InvalidArgument, "Volume ID not provided")
	ErrNodeIdNotProvided               = lstt.Error(lcodes.InvalidArgument, "Node ID not provided")
	ErrStagingTargetPathNotProvided    = lstt.Error(lcodes.InvalidArgument, "Staging target not provided")
	ErrVolumeContentSourceNotSupported = lstt.Error(lcodes.InvalidArgument, "Unsupported volumeContentSource type")
	ErrSnapshotIsNil                   = lstt.Error(lcodes.InvalidArgument, "Error retrieving snapshot from the volumeContentSource")
	ErrSnapshotNameNotProvided         = lstt.Error(lcodes.InvalidArgument, "Snapshot name not provided")
	ErrSnapshotSourceVolumeNotProvided = lstt.Error(lcodes.InvalidArgument, "Snapshot volume source ID not provided")
	ErrVolumeAttributesInvalid         = lstt.Error(lcodes.InvalidArgument, "Volume attributes are invalid")
	ErrMountIsNil                      = lstt.Error(lcodes.InvalidArgument, "Mount is nil within volume capability")

	ErrInvalidFstype = func(pfstype string) error {
		return lstt.Errorf(lcodes.InvalidArgument, "Invalid fstype (%s)", pfstype)
	}

	ErrCanNotParseRequestArguments = func(pargument, pvalue string) error {
		return lstt.Errorf(lcodes.InvalidArgument, "Could not parse %s (%s)", pargument, pvalue)
	}
)

var (
	ErrRequestExceedLimit = func(pvolSizeBytes, pmaxVolSize int64) error {
		return lstt.Error(lcodes.OutOfRange, lfmt.Sprintf("Requested size %d exceeds limit %d", pvolSizeBytes, pmaxVolSize))
	}
)

var (
	ErrVolumeIsCreating = func(pvolumeID string) error {
		return lstt.Errorf(lcodes.Aborted, "Create volume request for %s is already in progress", pvolumeID)
	}

	ErrOperationAlreadyExists = func(pvolumeID string) error {
		return lstt.Errorf(lcodes.Aborted, "An operation with the given volume %s already exists", pvolumeID)
	}

	ErrSnapshotIsDeleting = func(psnapshotID string) error {
		return lstt.Errorf(lcodes.Aborted, "Delete snapshot request for %s is already in progress", psnapshotID)
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

	ErrFailedToDeleteSnapshot = func(psnapshotID string) error {
		return lstt.Errorf(lcodes.Internal, "CANNOT delete snapshot %s", psnapshotID)
	}

	ErrFailedToListSnapshot = func(pvolumeID string) error {
		return lstt.Errorf(lcodes.Internal, "CANNOT list snapshot for volume %s", pvolumeID)
	}

	ErrFailedToGetVolume = func(pvolumeID string) error {
		return lstt.Errorf(lcodes.Internal, "CANNOT get volume %s", pvolumeID)
	}

	ErrFailedToExpandVolume = func(pvolumeID string, psize int64) error {
		return lstt.Errorf(lcodes.Internal, "CANNOT expand volume %s to new size %d GiB", pvolumeID, psize)
	}

	ErrFailedToModifyVolume = func(pvolumeID string) error {
		return lstt.Errorf(lcodes.Internal, "CANNOT modify volume %s", pvolumeID)
	}

	ErrFailedToDeleteVolume = func(pvolumeID string) error {
		return lstt.Errorf(lcodes.Internal, "CANNOT delete volume %s", pvolumeID)
	}

	ErrFailedToListVolumeByName = func(pvolName string) error {
		return lstt.Errorf(lcodes.Internal, "CANNOT list volume by name %s", pvolName)
	}
)

var (
	ErrVolumeNotFound = func(pvolumeID string) error {
		return lstt.Errorf(lcodes.NotFound, "Volume %s not found", pvolumeID)
	}
)

var (
	ErrNotImplemented = func(pfeature string) error {
		return lstt.Errorf(lcodes.Unimplemented, "%s is NOT implemented yet", pfeature)
	}
)
