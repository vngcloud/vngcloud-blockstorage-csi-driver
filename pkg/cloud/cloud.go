package cloud

import (
	"fmt"
	lset "github.com/cuongpiger/joat/data-structure/set"
	"strings"

	ljmath "github.com/cuongpiger/joat/math"
	ljutils "github.com/cuongpiger/joat/utils"
	ljwait "github.com/cuongpiger/joat/utils/exponential-backoff"
	"github.com/vngcloud/vngcloud-go-sdk/client"
	lsdkErr "github.com/vngcloud/vngcloud-go-sdk/error"
	"github.com/vngcloud/vngcloud-go-sdk/vngcloud"
	lerrEH "github.com/vngcloud/vngcloud-go-sdk/vngcloud/errors"
	lsdkObj "github.com/vngcloud/vngcloud-go-sdk/vngcloud/objects"
	"github.com/vngcloud/vngcloud-go-sdk/vngcloud/pagination"
	lvolAct "github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/blockstorage/v2/extensions/volume_actions"
	lsnapshotV2 "github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/blockstorage/v2/snapshot"
	lvolV2 "github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/blockstorage/v2/volume"
	lvolAtch "github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/compute/v2/extensions/volume_attach"
	"github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/identity/v2/extensions/oauth2"
	"github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/identity/v2/tokens"
	lportal "github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/portal/v1"
	"k8s.io/klog/v2"

	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/metrics"
	lsutil "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/util"
)

func NewCloud(iamURL, vserverUrl, clientID, clientSecret string, metadataSvc MetadataService) (Cloud, error) {
	vserverV1 := ljutils.NormalizeURL(vserverUrl) + "v1"
	vserverV2 := ljutils.NormalizeURL(vserverUrl) + "v2"

	pc, err := newVngCloud(iamURL, clientID, clientSecret)
	if err != nil {
		return nil, err
	}

	vserverV2Client, _ := vngcloud.NewServiceClient(vserverV2, pc, "vserver-v2")
	vserverV1Client, _ := vngcloud.NewServiceClient(vserverV1, pc, "portal-v1")
	ei, err := setupPortalInfo(vserverV1Client, metadataSvc)
	if err != nil {
		return nil, err
	}

	return &cloud{
		vServerV2:       vserverV2Client,
		vServerV1:       vserverV1Client,
		metadataService: metadataSvc,
		extraInfo:       ei,
	}, nil
}

func newVngCloud(iamURL, clientID, clientSecret string) (*client.ProviderClient, error) {
	identityUrl := ljutils.NormalizeURL(iamURL) + "v2"
	provider, _ := vngcloud.NewClient(identityUrl)
	err := vngcloud.Authenticate(provider, &oauth2.AuthOptions{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		AuthOptionsBuilder: &tokens.AuthOptions{
			IdentityEndpoint: iamURL,
		},
	})

	return provider, err
}

func setupPortalInfo(pportalClient *client.ServiceClient, pmetadataSvc MetadataService) (*extraInfa, error) {
	projectID := pmetadataSvc.GetProjectID()
	if len(projectID) < 1 {
		return nil, fmt.Errorf("projectID is empty")
	}

	klog.InfoS("setupPortalInfo", "projectID", projectID)

	portalInfo, err := lportal.Get(pportalClient, projectID)
	if err != nil {
		return nil, err
	}

	if portalInfo == nil {
		return nil, fmt.Errorf("can not get portal information")
	}

	return &extraInfa{
		ProjectID: portalInfo.ProjectID,
		UserID:    portalInfo.UserID,
	}, nil
}

type (
	cloud struct {
		vServerV1       *client.ServiceClient
		vServerV2       *client.ServiceClient
		extraInfo       *extraInfa
		metadataService MetadataService
	}

	extraInfa struct {
		ProjectID string
		UserID    int64
	}

	// ModifyDiskOptions represents parameters to modify a volume
	ModifyDiskOptions struct {
		VolumeType string
	}
)

func (s *cloud) GetVolumesByName(name string) ([]*lsdkObj.Volume, error) {
	klog.Infof("GetVolumesByName; called with name %s", name)

	var vols []*lsdkObj.Volume

	opts := lvolV2.NewListOpts(s.getProjectID(), name, 0, 0)
	mc := metrics.NewMetricContext("volume", "list")
	err := lvolV2.List(s.vServerV2, opts).EachPage(func(page pagination.IPage) (bool, error) {
		vols = append(vols, page.GetBody().(*lvolV2.ListResponse).ToListVolumeObjects()...)
		return true, nil
	})

	if mc.ObserveRequest(err) != nil {
		return nil, err
	}

	return vols, nil
}

func (s *cloud) CreateVolume(popts *lvolV2.CreateOpts) (*lsdkObj.Volume, error) {
	opts := lvolV2.NewCreateOpts(s.getProjectID(), popts)
	vol, err := lvolV2.Create(s.vServerV2, opts)

	if err != nil {
		return nil, err
	}

	vol, err = s.waitVolumeAchieveStatus(vol.VolumeId, volumeAvailableStatus)
	if err != nil {
		return nil, err
	}

	if vol.Size != popts.Size || vol.VolumeTypeID != popts.VolumeTypeId {
		newSize := ljmath.MaxNumeric(vol.Size, popts.Size)
		if err = s.ExpandVolume(vol.VolumeId, popts.VolumeTypeId, newSize); err != nil {
			return nil, err
		}

		vol.Size = newSize
		vol.VolumeTypeID = popts.VolumeTypeId
	}

	return vol, err
}

func (s *cloud) GetVolume(volumeID string) (*lsdkObj.Volume, *lsdkErr.SdkError) {
	opts := lvolV2.NewGetOpts(s.getProjectID(), volumeID)
	result, err := lvolV2.Get(s.vServerV2, opts)
	return result, err
}

func (s *cloud) DeleteVolume(volID string) error {
	vol, err := s.GetVolume(volID)
	if err != nil && err.Code == lerrEH.ErrCodeVolumeNotFound {
		return nil
	}

	if vol.Status == VolumeAvailableStatus {
		return lvolV2.Delete(s.vServerV2, lvolV2.NewDeleteOpts(s.extraInfo.ProjectID, volID))
	}

	return nil
}

func (s *cloud) AttachVolume(instanceID, volumeID string) (string, error) {
	vol, err := s.GetVolume(volumeID)
	if err != nil {
		return "", err.Error
	}

	if vol == nil {
		return "", fmt.Errorf("volume %s not found", volumeID)
	}

	if vol.VmId != nil && *vol.VmId != "-1" {
		return "", fmt.Errorf("volume %s already attached to instance %s", volumeID, *vol.VmId)
	}

	opts := lvolAtch.NewCreateOpts(s.getProjectID(), instanceID, volumeID)
	_, err2 := lvolAtch.Attach(s.vServerV2, opts)
	if err2 != nil {
		if err2.Code == lerrEH.ErrCodeVolumeAlreadyAttached {
			return vol.VolumeId, nil
		}

		return "", err2.Error
	}

	err3 := s.waitDiskAttached(instanceID, volumeID)

	return vol.VolumeId, err3
}

func (s *cloud) DetachVolume(instanceID, volumeID string) error {
	_, err := lvolAtch.Detach(s.vServerV2, lvolAtch.NewDeleteOpts(s.getProjectID(), instanceID, volumeID))
	// Disk has no attachments or not attached to the provided compute
	if err != nil {
		if errSetDetachIngore.ContainsOne(err.Code) {
			return nil
		}

		return err.Error
	}

	return s.waitDiskDetached(volumeID)
}

func (s *cloud) ResizeOrModifyDisk(volumeID string, newSizeBytes int64, options *ModifyDiskOptions) (newSize int64, err error) {
	newSizeGiB := uint64(lsutil.RoundUpGiB(newSizeBytes))
	volume, err1 := s.GetVolume(volumeID)
	if err1 != nil {
		return 0, err1.Error
	}

	if newSizeGiB < volume.Size {
		newSizeGiB = volume.Size
	}

	if options.VolumeType == "" {
		options.VolumeType = volume.VolumeTypeID
	}

	needsModification, volumeSize, err := s.validateModifyVolume(volumeID, newSizeGiB, options)
	if err != nil || !needsModification {
		return volumeSize, err
	}

	opts := lvolAct.NewResizeOpts(s.getProjectID(), options.VolumeType, volumeID, newSizeGiB)
	_, err = lvolAct.Resize(s.vServerV2, opts)
	if err != nil {
		return 0, err
	}

	_, err = s.waitVolumeAchieveStatus(volumeID, volumeArchivedStatus)
	if err != nil {
		return 0, err
	}

	// Perform one final check on the volume
	return s.checkDesiredState(volumeID, newSizeGiB, options)
}

func (s *cloud) ExpandVolume(volumeID, volumeTypeID string, newSize uint64) error {
	opts := lvolAct.NewResizeOpts(s.extraInfo.ProjectID, volumeTypeID, volumeID, newSize)
	_, err := lvolAct.Resize(s.vServerV2, opts)

	if err != nil {
		return err
	}

	_, err = s.waitVolumeAchieveStatus(volumeID, volumeArchivedStatus)
	return err
}

func (s *cloud) GetDeviceDiskID(pvolID string) (string, error) {
	opts := lvolAct.NewMappingOpts(s.getProjectID(), pvolID)
	res, err := lvolAct.GetMappingVolume(s.vServerV2, opts)

	// Process error occurs
	if err != nil {
		return "", err
	}

	if len(res.UUID) < DefaultDiskSymbolIdLength {
		return "", ErrDeviceVolumeIdNotFound
	}

	// Truncate the UUID for the virtual disk
	return res.UUID, nil
}

func (s *cloud) GetVolumeSnapshotByName(pvolID, psnapshotName string) (*lsdkObj.Snapshot, error) {
	res, err := lsnapshotV2.ListVolumeSnapshot(
		s.vServerV2, lsnapshotV2.NewListVolumeOpts(s.getProjectID(), pvolID, defaultPage, defaultPageSize))
	if err != nil {
		return nil, err.Error
	}

	for _, snap := range res.Items {
		if snap.VolumeID == pvolID && snap.Name == psnapshotName {
			return &snap, nil
		}
	}

	return nil, ErrSnapshotNotFound
}

func (s *cloud) CreateSnapshotFromVolume(pvolID string, popts *lsnapshotV2.CreateOpts) (*lsdkObj.Snapshot, error) {
	resp, err := lsnapshotV2.Create(s.vServerV2, lsnapshotV2.NewCreateOpts(s.getProjectID(), pvolID, popts))
	if err != nil {
		return nil, err
	}

	err = s.waitSnapshotActive(popts.VolumeID, resp.Name)
	return resp, err
}

func (s *cloud) DeleteSnapshot(psnapshotID string) error {
	err := lsnapshotV2.Delete(s.vServerV2, lsnapshotV2.NewDeleteOpts(s.getProjectID(), psnapshotID))
	if err != nil && err.Code != lerrEH.ErrCodeSnapshotNotFound {
		return err.Error
	}

	return nil
}

func (s *cloud) ListSnapshots(pvolID string, ppage int, ppageSize int) (*lsdkObj.SnapshotList, error) {
	opts := lsnapshotV2.NewListVolumeOpts(s.getProjectID(), pvolID, ppage, ppageSize)
	resp, err := lsnapshotV2.ListVolumeSnapshot(s.vServerV2, opts)
	if err != nil {
		return nil, err.Error
	}

	return resp, nil
}

func (s *cloud) waitVolumeAchieveStatus(pvolID string, pdesiredStatus lset.Set[string]) (*lsdkObj.Volume, error) {
	var resVolume *lsdkObj.Volume
	err := ljwait.ExponentialBackoff(ljwait.NewBackOff(waitVolumeActiveSteps, waitVolumeActiveDelay, true, waitVolumeActiveTimeout), func() (bool, error) {
		vol, err := s.GetVolume(pvolID)
		if err != nil {
			return false, err.Error
		}

		if pdesiredStatus.ContainsOne(vol.Status) {
			resVolume = vol
			return true, nil
		}

		return false, nil
	})

	if err != nil {
		return nil, err
	}

	return resVolume, nil
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

func (s *cloud) waitDiskDetached(volumeID string) error {
	return ljwait.ExponentialBackoff(
		ljwait.NewBackOff(waitVolumeDetachSteps, waitVolumeDetachDelay, true, waitVolumeDetachTimeout),
		func() (bool, error) {
			vol, err := s.GetVolume(volumeID)
			if err != nil {
				return false, err.Error
			}

			if vol.Status == VolumeAvailableStatus || (vol.Status == VolumeInUseStatus && len(vol.AttachedMachine) > 0) {
				return true, nil
			}

			return false, nil
		},
	)
}

func (s *cloud) waitDiskAttached(instanceID string, volumeID string) error {
	return ljwait.ExponentialBackoff(ljwait.NewBackOff(waitVolumeAttachSteps, waitVolumeAttachDelay, true, waitVolumeAttachTimeout), func() (bool, error) {
		vol, err := s.GetVolume(volumeID)
		if err != nil {
			return false, err.Error
		}

		if vol.VmId != nil && *vol.VmId == instanceID && vol.Status == VolumeInUseStatus {
			return true, nil

		}

		return false, nil
	})
}

func (s *cloud) getProjectID() string {
	return s.extraInfo.ProjectID
}

func (s *cloud) validateModifyVolume(volumeID string, newSizeGiB uint64, options *ModifyDiskOptions) (bool, int64, error) {
	volume, err := s.GetVolume(volumeID)
	if err != nil {
		return true, 0, err.Error
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
		return 0, err.Error
	}

	// AWS resizes in chunks of GiB (not GB)
	realSizeGiB := int64(volume.Size)

	// Check if there is a mismatch between the requested modification and the current volume
	// If there is, the volume is still modifying and we should not return a success
	if uint64(realSizeGiB) < desiredSizeGiB {
		return realSizeGiB, fmt.Errorf("volume %q is still being expanded to %d size", volumeID, desiredSizeGiB)
	} else if options.VolumeType != "" && !strings.EqualFold(volume.VolumeTypeID, options.VolumeType) {
		return realSizeGiB, fmt.Errorf("volume %q is still being modified to type %q", volumeID, options.VolumeType)
	}

	return realSizeGiB, nil
}

func needsVolumeModification(volume *lsdkObj.Volume, newSizeGiB uint64, options *ModifyDiskOptions) bool {
	oldSizeGiB := volume.Size
	needsModification := false

	if oldSizeGiB < newSizeGiB {
		needsModification = true
	}

	if options.VolumeType != "" && !strings.EqualFold(volume.VolumeTypeID, options.VolumeType) {
		needsModification = true
	}

	return needsModification
}
