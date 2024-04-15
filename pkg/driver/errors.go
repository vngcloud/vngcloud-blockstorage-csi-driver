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
	ErrDevicePathNotProvided           = lstt.Error(lcodes.InvalidArgument, "Device path not provided")
	ErrStagingTargetNotProvided        = lstt.Error(lcodes.InvalidArgument, "Staging target not provided")
	ErrTargetPathNotProvided           = lstt.Error(lcodes.InvalidArgument, "Target path not provided")
	ErrVolumeCapabilityNotProvided     = lstt.Error(lcodes.InvalidArgument, "Volume capability not provided")
	ErrVolumeCapabilityNotSupported    = lstt.Error(lcodes.InvalidArgument, "Volume capability not supported")

	ErrInvalidFstype = func(pfstype string) error {
		return lstt.Errorf(lcodes.InvalidArgument, "Invalid fstype (%s)", pfstype)
	}

	ErrCanNotParseRequestArguments = func(pargument, pvalue string) error {
		return lstt.Errorf(lcodes.InvalidArgument, "Could not parse %s (%s)", pargument, pvalue)
	}

	ErrInvalidFormatParameter = func(pkey string, perr error) error {
		return lstt.Errorf(lcodes.InvalidArgument, "Invalid %s (aborting!): %v", pkey, perr)
	}

	ErrCanNotUseSpecifiedFstype = func(pkey, pfsType string) error {
		return lstt.Errorf(lcodes.InvalidArgument, "Cannot use %s with fstype %s", pkey, pfsType)
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

	ErrFailedToFindTargetPath = func(pdevicePath string, perr error) error {
		return lstt.Errorf(lcodes.Internal, "Failed to find device path %s. %v", pdevicePath, perr)
	}

	ErrFailedToCheckTargetPathExists = func(ptarget string, perr error) error {
		return lstt.Errorf(lcodes.Internal, "Failed to check if target %q exists: %v", ptarget, perr)
	}

	ErrCanNotCreateTargetDir = func(ptarget string, perr error) error {
		return lstt.Errorf(lcodes.Internal, "CAN NOT create target dir %q: %v", ptarget, perr)
	}

	ErrFailedToCheckVolumeMounted = func(perr error) error {
		return lstt.Errorf(lcodes.Internal, "Failed to check if volume is already mounted: %v", perr)
	}

	ErrCanNotFormatAndMountVolume = func(psource, ptarget string, perr error) error {
		return lstt.Errorf(lcodes.Internal, "CAN NOT format %q and mount it at %q: %v", psource, ptarget, perr)
	}

	ErrDetermineVolumeResize = func(pvolumeID, psource string, perr error) error {
		return lstt.Errorf(lcodes.Internal, "Could not determine if volume %q (%q) need to be resized:  %v", pvolumeID, psource, perr)
	}

	ErrAttemptCreateResizeFs = func(perr error) error {
		return lstt.Errorf(lcodes.Internal, "Error attempting to create new ResizeFs:  %v", perr)
	}

	ErrCanNotResizeVolumeOnNode = func(pvolumeID, psource string, perr error) error {
		return lstt.Errorf(lcodes.Internal, "Could not resize volume %q (%q):  %v", pvolumeID, psource, perr)
	}

	ErrFailedCheckTargetPathIsMountPoint = func(ptarget string, perr error) error {
		return lstt.Errorf(lcodes.Internal, "Failed to check if target %q is a mount point: %v", ptarget, perr)
	}

	ErrCanNotUnmountTarget = func(ptarget string, perr error) error {
		return lstt.Errorf(lcodes.Internal, "CAN NOT unmount target %q: %v", ptarget, perr)
	}

	ErrFailedToCheckPathExists = func(ppath string, perr error) error {
		return lstt.Errorf(lcodes.Internal, "Could not check if path exists %q: %v", ppath, perr)
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
