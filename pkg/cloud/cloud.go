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

func (s *cloud) DeleteVolume(volID string) error {
	vol, ierr := s.GetVolume(volID)
	if ierr != nil && ierr.IsError(lsdkErrs.EcVServerVolumeNotFound) {
		return nil
	}

	if vol.CanDelete() {
		_, err := s.waitVolumeAchieveStatus(volID, availableDeleteStatus)
		if err != nil {
			return err
		}

		opt := lsdkVolumeV2.NewDeleteBlockVolumeByIdRequest(volID)
		sdkErr := s.client.VServerGateway().V2().VolumeService().DeleteBlockVolumeById(opt)
		if sdkErr != nil {
			if sdkErr.IsError(lsdkErrs.EcVServerVolumeNotFound) {
				return nil
			}

			return sdkErr.GetError()
		}
	}

	return nil
}

func (s *cloud) AttachVolume(pinstanceId, pvolumeId string) (*lsentity.Volume, error) {
	var (
		svol *lsentity.Volume
		err  error
	)

	svol, err = s.waitVolumeAchieveStatus(pvolumeId, volumeArchivedStatus)
	if err != nil {
		return nil, err
	}

	if svol.AttachedTheInstance(pinstanceId) {
		return svol, nil
	}

	opt := lsdkComputeV2.NewAttachBlockVolumeRequest(pinstanceId, pvolumeId)
	sdkErr := s.client.VServerGateway().V2().ComputeService().AttachBlockVolume(opt)
	if sdkErr != nil {
		if !sdkErr.IsError(lsdkErrs.EcVServerVolumeAlreadyAttachedThisServer) {
			return nil, sdkErr.GetError()
		}
	}

	err = s.waitDiskAttached(pinstanceId, pvolumeId)
	return svol, err

}

func (s *cloud) DetachVolume(pinstanceId, pvolumeId string) error {
	if err := ljwait.ExponentialBackoff(ljwait.NewBackOff(5, 10, true, ljtime.Minute(10)), func() (bool, error) {
		_, sdkErr := s.getVolumeById(pvolumeId)
		if sdkErr != nil {
			if sdkErr.IsError(lsdkErrs.EcVServerVolumeNotFound) {
				return true, nil
			}
			return false, sdkErr.GetError()
		}

		opt := lsdkComputeV2.NewDetachBlockVolumeRequest(pinstanceId, pvolumeId)
		sdkErr = s.client.VServerGateway().V2().ComputeService().DetachBlockVolume(opt)
		if sdkErr != nil {
			if errSetDetachIngore.ContainsOne(sdkErr.GetErrorCode()) {
				return true, nil
			}

			if sdkErr.IsError(lsdkErrs.EcVServerVolumeInProcess) {
				return false, nil
			}

			llog.ErrorS(sdkErr.GetError(), "[ERROR] - DetachVolume: Failed to detach the volume", sdkErr.GetListParameters()...)
			return false, sdkErr.GetError()
		}

		return false, nil
	}); err != nil {
		return err
	}

	llog.V(5).InfoS("[DEBUG] - DetachVolume: Detached the volume successfully", "volumeId", pvolumeId, "instanceId", pinstanceId)
	return nil
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

func (s *cloud) ExpandVolume(volumeID, volumeTypeID string, newSize uint64) error {
	//last := false
	//err := ljwait.ExponentialBackoff(ljwait.NewBackOff(10, 10, true, ljtime.Minute(10)), func() (bool, error) {
	//	vol, err := s.getVolumeById(volumeID)
	//	if err != nil {
	//		lsalert.HandleIError(err)
	//		return true, err.GetError()
	//	}
	//
	//	if volumeArchivedStatus.ContainsOne(vol.Status) {
	//		if last {
	//			return true, nil
	//		}
	//
	//		opt := lsdkVolumeV2.NewResizeBlockVolumeByIdRequest(volumeID, volumeTypeID, int(newSize))
	//		if _, sdkErr := s.client.VServerGateway().V2().VolumeService().ResizeBlockVolumeById(opt); sdkErr != nil {
	//			if sdkErr.IsError(lsdkErrs.EcVServerVolumeUnchanged) {
	//				return true, nil
	//			}
	//
	//			lsalert.HandleSdkError(sdkErr)
	//			return true, sdkErr.GetError()
	//		}
	//
	//		last = true
	//	}
	//
	//	return false, nil
	//})
	//
	//return err
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
