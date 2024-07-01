package cloud

import (
	lctx "context"
	lfmt "fmt"

	ljmath "github.com/cuongpiger/joat/math"
	ljtime "github.com/cuongpiger/joat/timer"
	ljwait "github.com/cuongpiger/joat/utils/exponential-backoff"
	lsentity "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/cloud/entity"
	lserr "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/cloud/errors"
	lsdkClientV2 "github.com/vngcloud/vngcloud-go-sdk/v2/client"
	lsdkEntity "github.com/vngcloud/vngcloud-go-sdk/v2/vngcloud/entity"
	lsdkErrs "github.com/vngcloud/vngcloud-go-sdk/v2/vngcloud/sdk_error"
	lsdkComputeV2 "github.com/vngcloud/vngcloud-go-sdk/v2/vngcloud/services/compute/v2"
	lsdkPortalSvcV1 "github.com/vngcloud/vngcloud-go-sdk/v2/vngcloud/services/portal/v1"
	lsdkVolumeV1 "github.com/vngcloud/vngcloud-go-sdk/v2/vngcloud/services/volume/v1"
	lsdkVolumeV2 "github.com/vngcloud/vngcloud-go-sdk/v2/vngcloud/services/volume/v2"
	llog "k8s.io/klog/v2"

	lsutil "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/util"
)

func NewCloud(iamURL, vserverUrl, clientID, clientSecret string, metadataSvc MetadataService) (Cloud, error) {
	projectID := metadataSvc.GetProjectID()
	clientCfg := lsdkClientV2.NewSdkConfigure().
		WithClientId(clientID).
		WithClientSecret(clientSecret).
		WithIamEndpoint(iamURL).
		WithVServerEndpoint(vserverUrl)

	cloudClient := lsdkClientV2.NewClient(lctx.TODO()).Configure(clientCfg)

	llog.V(5).InfoS("[DEBUG] - NodeGetInfo: Get the portal info and quota",
		"underProjectId", projectID, "iamURL", iamURL, "vserverUrl", vserverUrl, "clientID", clientID)
	portal, sdkErr := cloudClient.VServerGateway().V1().PortalService().
		GetPortalInfo(lsdkPortalSvcV1.NewGetPortalInfoRequest(projectID))

	if sdkErr != nil {
		llog.ErrorS(sdkErr.GetError(), "[ERROR] - NodeGetInfo; failed to get portal info", "errMsg", sdkErr.GetErrorMessages())
		return nil, sdkErr.GetError()
	}

	llog.InfoS("[INFO] - NodeGetInfo: Received the portal info successfully", "portal", portal)
	cloudClient = cloudClient.WithProjectId(portal.ProjectID)

	return &cloud{
		metadataService: metadataSvc,
		client:          cloudClient,
	}, nil
}

type (
	cloud struct {
		metadataService MetadataService
		client          lsdkClientV2.IClient
	}

	// ModifyDiskOptions represents parameters to modify a volume
	ModifyDiskOptions struct {
		VolumeType string
	}
)

func (s *cloud) EitherCreateResizeVolume(preq lsdkVolumeV2.ICreateBlockVolumeRequest) (*lsentity.Volume, lserr.IError) {
	var (
		vol, tmpVol *lsdkEntity.Volume
		serr        lserr.IError
		sdkErr      lsdkErrs.ISdkError
	)

	// Get the volume depend on the volume name
	if preq.GetVolumeName() != "" {
		llog.InfoS("[INFO] - EitherCreateResizeVolume: Get the volume by name", "volumeName", preq.GetVolumeName())
		vol, serr = s.getVolumeByName(preq.GetVolumeName())
		if serr != nil {
			if !serr.IsError(lsdkErrs.EcVServerVolumeNotFound) {
				llog.ErrorS(serr.GetError(), "[ERROR] - EitherCreateResizeVolume: Failed to get the volume by name", serr.GetListParameters()...)
				return nil, serr
			}
		}
	}

	if vol != nil {
		newSize := ljmath.MaxNumeric(vol.Size, uint64(preq.GetSize()))
		newVolumeType := preq.GetVolumeType()
		if vol.Size != newSize || vol.VolumeTypeID != newVolumeType {
			llog.InfoS("[INFO] - EitherCreateResizeVolume: Resize the volume", "volumeID", vol.Id, "newSize", newSize, "newVolumeType", newVolumeType)
			opt := lsdkVolumeV2.NewResizeBlockVolumeByIdRequest(newVolumeType, vol.Id, int(newSize))
			tmpVol, sdkErr = s.client.VServerGateway().V2().VolumeService().ResizeBlockVolumeById(opt)
			if sdkErr != nil {
				if sdkErr.IsError(lsdkErrs.EcVServerVolumeUnchanged) {
					return &lsentity.Volume{Volume: tmpVol}, nil
				}

				llog.ErrorS(sdkErr.GetError(), "[ERROR] - EitherCreateResizeVolume: Failed to resize the volume", sdkErr.GetListParameters()...)
				return nil, lserr.NewError(sdkErr)
			}

			vol = tmpVol
		}

		return &lsentity.Volume{Volume: vol}, nil
	}

	llog.InfoS("[INFO] - EitherCreateResizeVolume: Create the volume", preq.GetListParameters()...)
	vol, sdkErr = s.client.VServerGateway().V2().VolumeService().CreateBlockVolume(preq)
	if sdkErr != nil {
		llog.ErrorS(sdkErr.GetError(), "[ERROR] - EitherCreateResizeVolume: Failed to create the volume", sdkErr.GetListParameters()...)
		return nil, lserr.NewError(sdkErr)
	}

	llog.InfoS("[INFO] - EitherCreateResizeVolume: Created the volume successfully", "volumeID", vol.Id)
	return &lsentity.Volume{
		Volume: vol,
	}, nil
}

func (s *cloud) GetVolumeByName(pvolName string) (*lsentity.Volume, lserr.IError) {
	vol, serr := s.getVolumeByName(pvolName)
	if serr != nil {
		return nil, serr
	}

	return &lsentity.Volume{
		Volume: vol,
	}, nil
}

func (s *cloud) GetVolume(volumeID string) (*lsentity.Volume, lserr.IError) {
	vol, serr := s.getVolumeById(volumeID)
	if serr != nil {
		return nil, serr
	}

	return &lsentity.Volume{
		Volume: vol,
	}, nil
}

func (s *cloud) DeleteVolume(volID string) lserr.IError {
	llog.InfoS("[INFO] - DeleteVolume: Start deleting the volume", "volumeId", volID)

	var (
		ierr lserr.IError
	)

	_ = ljwait.ExponentialBackoff(ljwait.NewBackOff(5, 10, true, ljtime.Minute(10)), func() (bool, error) {
		vol, sdkErr := s.client.VServerGateway().V2().VolumeService().
			GetBlockVolumeById(lsdkVolumeV2.NewGetBlockVolumeByIdRequest(volID))

		if sdkErr != nil {
			if sdkErr.IsError(lsdkErrs.EcVServerVolumeNotFound) {
				ierr = nil // reset
				llog.InfoS("[INFO] - DeleteVolume: The volume was deleted before", "volumeId", volID)
				return true, nil
			}

			ierr = lserr.ErrVolumeFailedToGet(volID, sdkErr)
			return false, nil
		}

		// Check can delete this volume
		if vol.CanDelete() {
			llog.InfoS("[INFO] - DeleteVolume: Deleting the volume", "volumeId", volID)
			if sdkErr = s.client.VServerGateway().V2().VolumeService().
				DeleteBlockVolumeById(lsdkVolumeV2.NewDeleteBlockVolumeByIdRequest(volID)); sdkErr != nil {
				if sdkErr.IsError(lsdkErrs.EcVServerVolumeNotFound) {
					ierr = nil // reset
					llog.InfoS("[INFO] - DeleteVolume: The volume was deleted before", "volumeId", volID)
					return true, nil
				}

				ierr = lserr.ErrVolumeFailedToDelete(volID, sdkErr)
				llog.ErrorS(ierr.GetError(), "[ERROR] - DeleteVolume: Failed to delete the volume", ierr.GetListParameters()...)
				return false, nil
			}

			ierr = nil // reset
			return true, nil
		}

		return false, nil
	})

	if ierr == nil {
		llog.InfoS("[INFO] - DeleteVolume: Deleted the volume successfully", "volumeId", volID)
		return nil
	}

	return ierr
}

func (s *cloud) AttachVolume(pinstanceId, pvolumeId string) (*lsentity.Volume, lserr.IError) {
	var (
		svol *lsentity.Volume
		ierr lserr.IError
	)

	_ = ljwait.ExponentialBackoff(ljwait.NewBackOff(5, 10, true, ljtime.Minute(10)), func() (bool, error) {
		vol, sdkErr := s.client.VServerGateway().V2().VolumeService().
			GetBlockVolumeById(lsdkVolumeV2.NewGetBlockVolumeByIdRequest(pvolumeId))
		if sdkErr != nil {
			if sdkErr.IsError(lsdkErrs.EcVServerVolumeNotFound) || vol == nil {
				ierr = lserr.ErrVolumeNotFound(pvolumeId)
				llog.ErrorS(ierr.GetError(), "[ERROR] - AttachVolume: Volume not found", ierr.GetListParameters()...)
				return false, ierr.GetError()
			}

			return false, nil
		}

		// Volume is in error state
		if vol.IsError() {
			ierr = lserr.ErrVolumeIsInErrorState(pvolumeId)
			llog.ErrorS(ierr.GetError(), "[ERROR] - AttachVolume: The volume is in error state", ierr.GetListParameters()...)
			return false, ierr.GetError()
		}

		if vol.AttachedTheInstance(pinstanceId) {
			ierr = nil // reset
			llog.InfoS("[INFO] - AttachVolume: The volume is already attached", "volumeId", pvolumeId, "instanceId", pinstanceId)
			return true, nil
		}

		if vol.MultiAttach || vol.IsAvailable() {
			sdkErr = s.client.VServerGateway().V2().ComputeService().
				AttachBlockVolume(lsdkComputeV2.NewAttachBlockVolumeRequest(pinstanceId, pvolumeId))
			if sdkErr != nil {
				switch sdkErr.GetErrorCode() {
				case lsdkErrs.EcVServerVolumeAlreadyAttachedThisServer:
					ierr = nil // reset
					llog.InfoS("[INFO] - AttachVolume: The volume is already attached", "volumeId", pvolumeId, "instanceId", pinstanceId)
					return true, nil
				case lsdkErrs.EcVServerVolumeInProcess:
					llog.InfoS("[INFO] - AttachVolume: The volume is in process", "volumeId", pvolumeId, "instanceId", pinstanceId)
					return false, nil
				default:
					ierr = lserr.ErrVolumeFailedToAttach(pinstanceId, pvolumeId, sdkErr)
					llog.ErrorS(ierr.GetError(), "[ERROR] - AttachVolume: Failed to attach the volume", ierr.GetListParameters()...)
					return false, ierr.GetError()
				}
			}

			ierr = nil // reset
			llog.InfoS("[INFO] - AttachVolume: Attached the volume successfully", "volumeId", pvolumeId, "instanceId", pinstanceId)
			return true, nil
		}

		return false, nil
	})

	if ierr != nil {
		return nil, ierr
	}

	_ = ljwait.ExponentialBackoff(ljwait.NewBackOff(5, 10, true, ljtime.Minute(10)), func() (bool, error) {
		vol, sdkErr := s.client.VServerGateway().V2().VolumeService().
			GetBlockVolumeById(lsdkVolumeV2.NewGetBlockVolumeByIdRequest(pvolumeId))
		if sdkErr != nil {
			if sdkErr.IsError(lsdkErrs.EcVServerVolumeNotFound) || vol == nil {
				ierr = lserr.ErrVolumeNotFound(pvolumeId)
				llog.ErrorS(ierr.GetError(), "[ERROR] - AttachVolume: Volume not found", ierr.GetListParameters()...)
				return false, ierr.GetError()
			}

			llog.ErrorS(ierr.GetError(), "[ERROR] - AttachVolume: Failed to get the volume when waiting it become archieve status", ierr.GetListParameters()...)
			return false, nil
		}

		if vol.IsError() {
			ierr = lserr.ErrVolumeIsInErrorState(pvolumeId)
			llog.ErrorS(ierr.GetError(), "[ERROR] - AttachVolume: The volume is in error state", ierr.GetListParameters()...)
			return false, ierr.GetError()
		}

		if vol.AttachedTheInstance(pinstanceId) {
			ierr = nil // reset
			svol = lsentity.NewVolume(vol)
			return true, nil
		}

		return false, nil
	})

	if ierr != nil {
		return nil, ierr
	}

	return svol, nil
}

func (s *cloud) DetachVolume(pinstanceId, pvolumeId string) lserr.IError {
	llog.InfoS("[INFO] - DetachVolume: Start detaching the volume", "volumeId", pvolumeId, "instanceId", pinstanceId)

	var (
		ierr lserr.IError
	)

	_ = ljwait.ExponentialBackoff(ljwait.NewBackOff(5, 10, true, ljtime.Minute(10)), func() (bool, error) {
		vol, sdkErr := s.client.VServerGateway().V2().VolumeService().
			GetBlockVolumeById(lsdkVolumeV2.NewGetBlockVolumeByIdRequest(pvolumeId))
		if sdkErr != nil {
			ierr = lserr.ErrVolumeFailedToGet(pvolumeId, sdkErr)
			llog.ErrorS(ierr.GetError(), "[ERROR] - DetachVolume: Failed to get the volume", ierr.GetListParameters()...)
			return false, ierr.GetError()
		}

		// Ignore if the volume is AVAILABLE status => This volume is not attached to any instance
		if vol.IsAvailable() {
			ierr = nil // reset the ierr variable
			llog.InfoS("[INFO] - DetachVolume: The volume is already detached", "volumeId", pvolumeId)
			return true, nil
		}

		// Ignore if the volume is not attached to this instance
		if !vol.AttachedTheInstance(pinstanceId) {
			ierr = nil
			llog.InfoS("[INFO] - DetachVolume: The volume is not attached to the instance", "volumeId", pvolumeId, "instanceId", pinstanceId)
			return true, nil
		}

		// Return if the volume is in ERROR state => stop the process if the volume is in ERROR state
		if vol.IsError() {
			ierr = lserr.ErrVolumeIsInErrorState(pvolumeId)
			llog.InfoS("[INFO] - DetachVolume: The volume is in error state", "volumeId", pvolumeId)
			return false, ierr.GetError()
		}

		// Only detach the volume if it is in IN-USE state
		if vol.IsInUse() {
			llog.InfoS("[INFO] - DetachVolume: Detaching the volume", "volumeId", pvolumeId, "instanceId", pinstanceId)
			if sdkErr = s.client.VServerGateway().V2().ComputeService().
				DetachBlockVolume(lsdkComputeV2.NewDetachBlockVolumeRequest(pinstanceId, pvolumeId)); sdkErr != nil {
				ierr = lserr.ErrVolumeFailedToDetach(pinstanceId, pvolumeId, sdkErr)
				llog.ErrorS(ierr.GetError(), "[ERROR] - DetachVolume: Failed to detach the volume", ierr.GetListParameters()...)
			}
		}

		// Return false to wait for the next iteration
		return false, nil
	})

	if ierr == nil {
		llog.InfoS("[DEBUG] - DetachVolume: Detached the volume successfully", "volumeId", pvolumeId, "instanceId", pinstanceId)
		return nil
	}

	return ierr
}

func (s *cloud) ResizeOrModifyDisk(volumeID string, newSizeBytes int64, options *ModifyDiskOptions) (newSize int64, err error) {
	newSizeGiB := uint64(lsutil.RoundUpGiB(newSizeBytes))
	volume, sdkErr := s.GetVolume(volumeID)
	if sdkErr != nil {
		return 0, sdkErr.GetError()
	}

	if newSizeGiB < volume.Size {
		newSizeGiB = volume.Size
	}

	if options.VolumeType == "" {
		options.VolumeType = volume.VolumeTypeID
	}

	// Check that we need to modify this volume`
	needsModification, volumeSize, err := s.validateModifyVolume(volumeID, newSizeGiB, options)
	if err != nil || !needsModification {
		return volumeSize, err
	}

	// The volume types are different => so please check the zone a same
	same, sdkErr2 := s.checkSameZone(options.VolumeType, volume.VolumeTypeID)
	if sdkErr2 != nil {
		return 0, sdkErr2.GetError()
	} else if !same && !volume.IsAttched() {
		// In the case of these volume types are not in the same zone, we MUST migrate the volume to the target zone before resize this volume
		err = ljwait.ExponentialBackoff(ljwait.NewBackOff(10, 10, true, ljtime.Minute(30)), func() (bool, error) {
			volume, sdkErr = s.GetVolume(volumeID)
			if sdkErr != nil {
				return true, sdkErr.GetError()
			}

			if volume.IsMigration() || volume.IsCreating() {
				return false, nil
			} else if volumeArchivedStatus.ContainsOne(volume.Status) && volume.VolumeTypeID == options.VolumeType {
				// Check the volume status is in the archived status
				return true, nil
			}

			// So make a migration volume request to the VngCloud API
			sdkErr2 = s.client.VServerGateway().V2().VolumeService().
				MigrateBlockVolumeById(
					lsdkVolumeV2.NewMigrateBlockVolumeByIdRequest(volumeID, options.VolumeType).
						WithConfirm(true))

			// In the case of getting an error from the VngCloud API
			if sdkErr2 != nil {
				switch sdkErr2.GetErrorCode() {
				case lsdkErrs.EcVServerVolumeMigrateInSameZone, lsdkErrs.EcVServerVolumeMigrateBeingProcess, lsdkErrs.EcVServerVolumeMigrateProcessingConfirm, lsdkErrs.EcVServerVolumeMigrateBeingMigrating, lsdkErrs.EcVServerVolumeMigrateBeingFinish:
					// These statuses are used to indicate that the volume is being migrated => continue to wait
					return false, nil
				default:
					// In some other cases, we need to handle the error
					return true, sdkErr2.GetError()
				}
			}

			// Otherwise, we need to wait for the volume to be migrated
			return false, nil
		})

		if err != nil {
			return 0, err
		}
	}

	opt := lsdkVolumeV2.NewResizeBlockVolumeByIdRequest(volumeID, options.VolumeType, int(newSizeGiB))
	_, sdkErr = s.client.VServerGateway().V2().VolumeService().ResizeBlockVolumeById(opt)
	if sdkErr != nil && !sdkErr.IsError(lsdkErrs.EcVServerVolumeUnchanged) {
		return 0, sdkErr.GetError()
	}

	_, err = s.waitVolumeAchieveStatus(volumeID, volumeArchivedStatus)
	if err != nil {
		return 0, err
	}

	// Perform one final check on the volume
	return s.checkDesiredState(volumeID, newSizeGiB, options)
}

func (s *cloud) ModifyVolumeType(pvolumeId, pvolumeType string, psize int) lserr.IError {
	llog.InfoS("[INFO] - ModifyVolumeType: Modify the volume type", "volumeId", pvolumeId, "volumeType", pvolumeType, "size", psize)
	opts := lsdkVolumeV2.NewResizeBlockVolumeByIdRequest(pvolumeId, pvolumeType, psize)
	_, sdkErr := s.client.VServerGateway().V2().VolumeService().ResizeBlockVolumeById(opts)

	if sdkErr != nil {
		if !sdkErr.IsError(lsdkErrs.EcVServerVolumeUnchanged) {
			llog.ErrorS(sdkErr.GetError(), "[ERROR] - ModifyVolumeType: Failed to modify the volume type", sdkErr.GetListParameters()...)
			return lserr.NewError(sdkErr)
		}
	}

	var ierr lserr.IError
	llog.InfoS("[INFO] - ModifyVolumeType: Modified the volume type successfully, waiting the volume to be desired state", "volumeId", pvolumeId, "volumeType", pvolumeType, "size", psize)
	_ = ljwait.ExponentialBackoff(ljwait.NewBackOff(5, 10, true, ljtime.Minute(20)), func() (bool, error) {
		vol, sdkErr2 := s.GetVolume(pvolumeId)
		if sdkErr2 != nil {
			llog.ErrorS(sdkErr2.GetError(), "[ERROR] - ModifyVolumeType: Failed to get the volume", sdkErr2.GetListParameters()...)
			return false, sdkErr2.GetError()
		}

		if volumeArchivedStatus.ContainsOne(vol.Status) && vol.VolumeTypeID == pvolumeType {
			return true, nil
		}

		// if the volume is in ERROR state => return right now
		if vol.IsError() {
			ierr = lserr.ErrVolumeIsInErrorState(pvolumeId)
			return false, ierr.GetError()
		}

		return false, nil
	})

	return ierr
}

func (s *cloud) ExpandVolume(volumeID, volumeTypeID string, newSize uint64) error {
	_, err := s.ResizeOrModifyDisk(volumeID, lsutil.GiBToBytes(int64(newSize)), &ModifyDiskOptions{
		VolumeType: volumeTypeID,
	})
	return err
}

func (s *cloud) GetDeviceDiskID(pvolID string) (string, error) {
	opts := lsdkVolumeV2.NewGetBlockVolumeByIdRequest(pvolID)
	vol, err := s.client.VServerGateway().V2().VolumeService().GetUnderBlockVolumeId(opts)
	if err != nil {
		llog.ErrorS(err.GetError(), "[ERROR] - GetDeviceDiskID: Failed to get the device disk ID", err.GetListParameters()...)
		return "", err.GetError()
	}

	return vol.UnderId, nil
}

func (s *cloud) GetVolumeSnapshotByName(pvolID, psnapshotName string) (*lsentity.Snapshot, error) {
	opt := lsdkVolumeV2.NewListSnapshotsByBlockVolumeIdRequest(1, 10, pvolID)
	res, err := s.client.VServerGateway().V2().VolumeService().ListSnapshotsByBlockVolumeId(opt)
	if err != nil {
		return nil, err.GetError()
	}

	for _, snap := range res.Items {
		if snap.VolumeId == pvolID && snap.Name == psnapshotName {
			return &lsentity.Snapshot{Snapshot: snap}, nil
		}
	}

	return nil, ErrSnapshotNotFound
}

func (s *cloud) CreateSnapshotFromVolume(pclusterId, pvolId, psnapshotName string) (*lsentity.Snapshot, error) {
	opt := lsdkVolumeV2.NewCreateSnapshotByBlockVolumeIdRequest(psnapshotName, pvolId).
		WithPermanently(true).
		WithDescription(lfmt.Sprintf(patternSnapshotDescription, pvolId, pclusterId))

	snapshot, sdkErr := s.client.VServerGateway().V2().VolumeService().CreateSnapshotByBlockVolumeId(opt)
	if sdkErr != nil {
		return nil, sdkErr.GetError()
	}

	err := s.waitSnapshotActive(pvolId, snapshot.Name)
	return &lsentity.Snapshot{Snapshot: snapshot}, err
}

func (s *cloud) DeleteSnapshot(psnapshotID string) error {
	opt := lsdkVolumeV2.NewDeleteSnapshotByIdRequest(psnapshotID)
	sdkErr := s.client.VServerGateway().V2().VolumeService().DeleteSnapshotById(opt)
	if sdkErr != nil {
		if !sdkErr.IsError(lsdkErrs.EcVServerSnapshotNotFound) {
			return sdkErr.GetError()
		}
	}
	return nil
}

func (s *cloud) ListSnapshots(pvolID string, ppage int, ppageSize int) (*lsentity.ListSnapshots, lserr.IError) {
	opt := lsdkVolumeV2.NewListSnapshotsByBlockVolumeIdRequest(ppage, ppageSize, pvolID)
	res, sdkErr := s.client.VServerGateway().V2().VolumeService().ListSnapshotsByBlockVolumeId(opt)
	if sdkErr != nil {
		return nil, lserr.NewError(sdkErr)
	}

	return &lsentity.ListSnapshots{ListSnapshots: res}, nil
}

func (s *cloud) GetVolumeTypeById(pvolTypeId string) (*lsentity.VolumeType, lserr.IError) {
	opt := lsdkVolumeV1.NewGetVolumeTypeByIdRequest(pvolTypeId)
	volType, err := s.client.VServerGateway().V1().VolumeService().GetVolumeTypeById(opt)
	if err != nil {
		return nil, lserr.NewError(err)
	}

	return &lsentity.VolumeType{VolumeType: volType}, nil
}

func (s *cloud) GetDefaultVolumeType() (*lsentity.VolumeType, lserr.IError) {
	volType, err := s.client.VServerGateway().V1().VolumeService().GetDefaultVolumeType()
	if err != nil {
		return nil, lserr.NewError(err)
	}

	return &lsentity.VolumeType{VolumeType: volType}, nil
}
