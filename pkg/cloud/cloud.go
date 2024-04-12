package cloud

import (
	"errors"
	"fmt"
	"time"

	ljutils "github.com/cuongpiger/joat/utils"
	ljwait "github.com/cuongpiger/joat/utils/exponential-backoff"
	"github.com/vngcloud/vngcloud-go-sdk/client"
	lsdkErr "github.com/vngcloud/vngcloud-go-sdk/error"
	"github.com/vngcloud/vngcloud-go-sdk/vngcloud"
	lerrEH "github.com/vngcloud/vngcloud-go-sdk/vngcloud/errors"
	"github.com/vngcloud/vngcloud-go-sdk/vngcloud/objects"
	"github.com/vngcloud/vngcloud-go-sdk/vngcloud/pagination"
	lvolAct "github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/blockstorage/v2/extensions/volume_actions"
	lvolV2 "github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/blockstorage/v2/volume"
	lVolAtch "github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/compute/v2/extensions/volume_attach"
	"github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/identity/v2/extensions/oauth2"
	"github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/identity/v2/tokens"
	lPortal "github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/portal/v1"
	"k8s.io/klog/v2"

	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/metrics"
)

// Defaults
const (
	operationFinishInitDelay = 1 * time.Second
	operationFinishFactor    = 1.1
	operationFinishSteps     = 15
)

var (
	ErrInvalidArgument = errors.New("invalid argument")
)

// NewCloud returns a new instance of AWS cloud
// It panics if session is invalid
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

type (
	cloud struct {
		compute         *client.ServiceClient
		volume          *client.ServiceClient
		portal          *client.ServiceClient
		snapshot        *client.ServiceClient
		extraInfo       *extraInfa
		metadataService MetadataService
	}

	extraInfa struct {
		ProjectID string
		UserID    int64
	}
)

func (s *cloud) GetVolumesByName(name string) ([]*objects.Volume, error) {
	klog.Infof("GetVolumesByName; called with name %s", name)

	var vols []*objects.Volume

	opts := lvolV2.NewListOpts(s.extraInfo.ProjectID, name, 0, 0)
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

func (s *cloud) CreateVolume(popts *lvolV2.CreateOpts) (*objects.Volume, error) {
	opts := lvolV2.NewCreateOpts(s.extraInfo.ProjectID, popts)
	vol, err := lvolV2.Create(s.volume, opts)

	if err != nil {
		return nil, err
	}

	err = s.waitVolumeActive(vol.VolumeId)

	return vol, err
}

func (s *cloud) GetVolume(volumeID string) (*objects.Volume, *lsdkErr.SdkError) {
	opts := lvolV2.NewGetOpts(s.extraInfo.ProjectID, volumeID)
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

	opts := lVolAtch.NewCreateOpts(s.extraInfo.ProjectID, instanceID, volumeID)
	_, err2 := lVolAtch.Attach(s.compute, opts)
	if err2 != nil {
		return "", err2
	}

	err2 = s.waitDiskAttached(instanceID, volumeID)

	return vol.VolumeId, err2
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

func (s *cloud) DetachVolume(instanceID, volumeID string) error {
	_, err := lVolAtch.Detach(s.compute, lVolAtch.NewDeleteOpts(s.extraInfo.ProjectID, instanceID, volumeID))
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
	opts := lvolAct.NewResizeOpts(s.extraInfo.ProjectID, volumeTypeID, volumeID, newSize)
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

func setupPortalInfo(pportalClient *client.ServiceClient, pmetadataSvc MetadataService) (*extraInfa, error) {
	projectID := pmetadataSvc.GetProjectID()
	if len(projectID) < 1 {
		return nil, fmt.Errorf("projectID is empty")
	}

	klog.InfoS("setupPortalInfo", "projectID", projectID)

	portalInfo, err := lPortal.Get(pportalClient, projectID)
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

// ModifyDiskOptions represents parameters to modify an EBS volume
type ModifyDiskOptions struct {
	VolumeType string
}

func (s *cloud) GetDeviceDiskID(pvolID string) (string, error) {
	opts := lvolAct.NewMappingOpts(s.extraInfo.ProjectID, pvolID)
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
