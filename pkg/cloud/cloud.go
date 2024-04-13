package cloud

import (
	"fmt"
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
)

func NewCloud(iamURL, vserverUrl, clientID, clientSecret string, metadataSvc MetadataService) (Cloud, error) {
	vserverV1 := ljutils.NormalizeURL(vserverUrl) + "v1"
	vserverV2 := ljutils.NormalizeURL(vserverUrl) + "v2"

	pc, err := newVngCloud(iamURL, clientID, clientSecret)
	if err != nil {
		return nil, err
	}

	compute, _ := vngcloud.NewServiceClient(vserverV2, pc, "compute")
	volume, _ := vngcloud.NewServiceClient(vserverV2, pc, "volume")
	portal, _ := vngcloud.NewServiceClient(vserverV1, pc, "portal")
	ei, err := setupPortalInfo(portal, metadataSvc)
	if err != nil {
		return nil, err
	}

	return &cloud{
		compute:         compute,
		volume:          volume,
		portal:          portal,
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
		compute         *client.ServiceClient
		volume          *client.ServiceClient
		portal          *client.ServiceClient
		extraInfo       *extraInfa
		metadataService MetadataService
	}

	extraInfa struct {
		ProjectID string
		UserID    int64
	}

	// modifyDiskOptions represents parameters to modify an EBS volume
	ModifyDiskOptions struct {
		VolumeType string
	}
)

func (s *cloud) GetVolumesByName(name string) ([]*lsdkObj.Volume, error) {
	klog.Infof("GetVolumesByName; called with name %s", name)

	var vols []*lsdkObj.Volume

	opts := lvolV2.NewListOpts(s.getProjectID(), name, 0, 0)
	mc := metrics.NewMetricContext("volume", "list")
	err := lvolV2.List(s.volume, opts).EachPage(func(page pagination.IPage) (bool, error) {
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
	vol, err := lvolV2.Create(s.volume, opts)

	if err != nil {
		return nil, err
	}

	err = s.waitVolumeActive(vol.VolumeId)

	return vol, err
}

func (s *cloud) GetVolume(volumeID string) (*lsdkObj.Volume, *lsdkErr.SdkError) {
	opts := lvolV2.NewGetOpts(s.getProjectID(), volumeID)
	result, err := lvolV2.Get(s.volume, opts)
	return result, err
}

func (s *cloud) DeleteVolume(volID string) error {
	vol, err := s.GetVolume(volID)
	if err != nil && err.Code == lerrEH.ErrCodeVolumeNotFound {
		return nil
	}

	if vol.Status == VolumeAvailableStatus {
		return lvolV2.Delete(s.volume, lvolV2.NewDeleteOpts(s.extraInfo.ProjectID, volID))
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
	_, err2 := lvolAtch.Attach(s.compute, opts)
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
	_, err := lvolAtch.Detach(s.compute, lvolAtch.NewDeleteOpts(s.getProjectID(), instanceID, volumeID))
	// Disk has no attachments or not attached to the provided compute
	if err != nil {
		if errSetDetachIngore.ContainsOne(err.Code) {
			return nil
		}

		return err.Error
	}

	return s.waitDiskDetached(volumeID)
}

func (s *cloud) ExpandVolume(volumeTypeID, volumeID string, newSize uint64) error {
	opts := lvolAct.NewResizeOpts(s.getProjectID(), volumeTypeID, volumeID, newSize)
	mc := metrics.NewMetricContext("volume", "extend")
	_, err := lvolAct.Resize(s.volume, opts)
	if mc.ObserveRequest(err) != nil {
		return err
	}

	return nil
}

func (s *cloud) ResizeOrModifyDisk(volumeID string, newSizeBytes int64, options *ModifyDiskOptions) (newSize int64, err error) {
	return 0, nil
}

func (s *cloud) GetDeviceDiskID(pvolID string) (string, error) {
	opts := lvolAct.NewMappingOpts(s.getProjectID(), pvolID)
	res, err := lvolAct.GetMappingVolume(s.volume, opts)

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
		s.volume, lsnapshotV2.NewListVolumeOpts(s.getProjectID(), pvolID, defaultPage, defaultPageSize))
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
	resp, err := lsnapshotV2.Create(s.volume, lsnapshotV2.NewCreateOpts(s.getProjectID(), pvolID, popts))
	if err != nil {
		return nil, err
	}

	err = s.waitSnapshotActive(popts.VolumeID, resp.Name)
	return resp, err
}

func (s *cloud) DeleteSnapshot(psnapshotID string) error {
	err := lsnapshotV2.Delete(s.volume, lsnapshotV2.NewDeleteOpts(s.getProjectID(), psnapshotID))
	if err != nil && err.Code != lerrEH.ErrCodeSnapshotNotFound {
		return err.Error
	}

	return nil
}

func (s *cloud) waitVolumeActive(pvolID string) error {
	return ljwait.ExponentialBackoff(ljwait.NewBackOff(waitVolumeActiveSteps, waitVolumeActiveDelay, true, waitVolumeActiveTimeout), func() (bool, error) {
		vol, err := s.GetVolume(pvolID)
		if err != nil {
			return false, err.Error
		}

		if vol.Status == VolumeAvailableStatus {
			return true, nil
		}

		return false, nil
	})
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
