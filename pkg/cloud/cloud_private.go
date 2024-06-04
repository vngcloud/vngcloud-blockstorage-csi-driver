package cloud

import (
	lctx "context"
	lfmt "fmt"
	lstr "strings"

	ljset "github.com/cuongpiger/joat/data-structure/set"
	lset "github.com/cuongpiger/joat/data-structure/set"
	ljwait "github.com/cuongpiger/joat/utils/exponential-backoff"
	lsdkEntity "github.com/vngcloud/vngcloud-go-sdk/v2/vngcloud/entity"
	lsdkErrs "github.com/vngcloud/vngcloud-go-sdk/v2/vngcloud/sdk_error"
	lsdkVolume "github.com/vngcloud/vngcloud-go-sdk/v2/vngcloud/services/volume/v2"
	lerrgroup "golang.org/x/sync/errgroup"

	lsentity "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/cloud/entity"
	lserr "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/cloud/errors"
)

func (s *cloud) getVolumeByName(pvolName string) (*lsdkEntity.Volume, lserr.IError) {
	// The volume name is empty
	if pvolName == "" {
		return nil, new(lsdkErrs.SdkError).WithErrorCode(lsdkErrs.EcVServerVolumeNotFound)
	}

	// Get the volume depends on name
	opts := lsdkVolume.NewListBlockVolumesRequest(1, 10).WithName(pvolName)
	vols, sdkErr := s.client.VServerGateway().V2().VolumeService().ListBlockVolumes(opts)
	if sdkErr != nil {
		return nil, lserr.NewError(sdkErr)
	}

	// Get volume by name with greater than 1 vol
	if vols.Len() != 1 {
		return nil, new(lsdkErrs.SdkError).WithErrorCode(lsdkErrs.EcVServerVolumeNotFound)
	}

	// If all valid, return the first item
	return vols.Items[0], nil
}

func (s *cloud) getVolumeById(pvolId string) (*lsdkEntity.Volume, lserr.IError) {
	// Get the volume depends on id
	opts := lsdkVolume.NewGetBlockVolumeByIdRequest(pvolId)
	vol, sdkErr := s.client.VServerGateway().V2().VolumeService().GetBlockVolumeById(opts)
	if sdkErr != nil {
		return nil, lserr.NewError(sdkErr)
	}

	return vol, nil
}

func (s *cloud) waitSnapshotActive(pvolID, psnapshotName string) error {
	return ljwait.ExponentialBackoff(ljwait.NewBackOff(waitSnapshotActiveSteps, waitSnapshotActiveDelay, true, waitSnapshotActiveTimeout), func() (bool, error) {
		vol, err := s.GetVolumeSnapshotByName(pvolID, psnapshotName)
		if err != nil {
			return false, err
		}

		if vol.Status == SnapshotActiveStatus {
			return true, nil
		}

		return false, nil
	})
}

func (s *cloud) waitDiskAttached(instanceID string, volumeID string) error {
	return ljwait.ExponentialBackoff(ljwait.NewBackOff(waitVolumeAttachSteps, waitVolumeAttachDelay, true, waitVolumeAttachTimeout), func() (bool, error) {
		vol, err := s.getVolumeById(volumeID)
		if err != nil {
			return false, err.GetError()
		}

		if vol.Status == VolumeErrorStatus {
			return true, lfmt.Errorf("volume %q is in error state", volumeID)
		}

		if vol.VmId != nil && *vol.VmId == instanceID && vol.Status == VolumeInUseStatus {
			return true, nil
		}

		return false, nil
	})
}

func (s *cloud) validateModifyVolume(volumeID string, newSizeGiB uint64, options *ModifyDiskOptions) (bool, int64, error) {
	volume, err := s.GetVolume(volumeID)
	if err != nil {
		return true, 0, err.GetError()
	}

	// At this point, we know we are starting a new volume modification
	// If we're asked to modify a volume to its current state, ignore the request and immediately return a success
	if !needsVolumeModification(volume, newSizeGiB, options) {
		// Wait for any existing modifications to prevent race conditions where DescribeVolume(s) returns the new
		// state before the volume is actually finished modifying
		_, err2 := s.waitVolumeAchieveStatus(volumeID, volumeArchivedStatus)
		if err != nil {
			return true, int64(volume.Size), err2
		}

		returnGiB, returnErr := s.checkDesiredState(volumeID, newSizeGiB, options)
		return false, returnGiB, returnErr
	}

	return true, 0, nil
}

func (s *cloud) checkDesiredState(volumeID string, desiredSizeGiB uint64, options *ModifyDiskOptions) (int64, error) {
	volume, err := s.GetVolume(volumeID)
	if err != nil {
		return 0, err.GetError()
	}

	// AWS resizes in chunks of GiB (not GB)
	realSizeGiB := int64(volume.Size)

	// Check if there is a mismatch between the requested modification and the current volume
	// If there is, the volume is still modifying and we should not return a success
	if uint64(realSizeGiB) < desiredSizeGiB {
		return realSizeGiB, lfmt.Errorf("volume %q is still being expanded to %d size", volumeID, desiredSizeGiB)
	} else if options.VolumeType != "" && !lstr.EqualFold(volume.VolumeTypeID, options.VolumeType) {
		return realSizeGiB, lfmt.Errorf("volume %q is still being modified to type %q", volumeID, options.VolumeType)
	}

	return realSizeGiB, nil
}

func needsVolumeModification(volume *lsentity.Volume, newSizeGiB uint64, options *ModifyDiskOptions) bool {
	oldSizeGiB := volume.Size
	needsModification := false

	if oldSizeGiB < newSizeGiB {
		needsModification = true
	}

	if options.VolumeType != "" && !lstr.EqualFold(volume.VolumeTypeID, options.VolumeType) {
		needsModification = true
	}

	return needsModification
}

func (s *cloud) waitVolumeAchieveStatus(pvolID string, pdesiredStatus lset.Set[string]) (*lsentity.Volume, error) {
	var resVolume *lsentity.Volume
	err := ljwait.ExponentialBackoff(ljwait.NewBackOff(waitVolumeActiveSteps, waitVolumeActiveDelay, true, waitVolumeActiveTimeout), func() (bool, error) {
		vol, err := s.getVolumeById(pvolID)
		if err != nil {
			return false, err.GetError()
		}

		if pdesiredStatus.ContainsOne(vol.Status) {
			resVolume = &lsentity.Volume{Volume: vol}
			return true, nil
		}

		return false, nil
	})

	if err != nil {
		return nil, err
	}

	return resVolume, nil
}

func (s *cloud) checkSameZone(pvolTypeA, pvolTypeB string) (bool, lsdkErrs.ISdkError) {
	if pvolTypeA == pvolTypeB {
		return true, nil
	}

	var (
		zoneSet = ljset.NewSet[string]()
		sdkErr  lsdkErrs.ISdkError
	)

	group, _ := lerrgroup.WithContext(lctx.TODO())
	for _, volType := range []string{pvolTypeA, pvolTypeB} {
		tmpVolType := volType
		group.Go(func() error {
			vol, err := s.GetVolumeTypeById(tmpVolType)
			if err != nil {
				sdkErr = err
				return err.GetError()
			}

			zoneSet.Add(vol.ZoneId)
			return nil
		})
	}

	if err := group.Wait(); err != nil {
		return false, sdkErr
	}

	if zoneSet.IsEmpty() || zoneSet.Cardinality() != 1 {
		return false, nil
	}

	return true, nil
}
