package cloud

import (
	"errors"
	"fmt"
	"github.com/cuongpiger/joat/utils"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/csi/metrics"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/util"
	"github.com/vngcloud/vngcloud-go-sdk/client"
	"github.com/vngcloud/vngcloud-go-sdk/vngcloud"
	"github.com/vngcloud/vngcloud-go-sdk/vngcloud/objects"
	"github.com/vngcloud/vngcloud-go-sdk/vngcloud/pagination"
	lVolAct "github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/blockstorage/v2/extensions/volume_actions"
	lVol "github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/blockstorage/v2/volume"
	lVolAtch "github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/compute/v2/extensions/volume_attach"
	"github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/identity/v2/extensions/oauth2"
	"github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/identity/v2/tokens"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"strings"
	"time"
)

// Defaults
const (
	// DefaultVolumeSize represents the default volume size.
	DefaultVolumeSize   int64 = 20 * util.GiB
	diskAttachInitDelay       = 30 * time.Second
	diskAttachSteps           = 15
	diskAttachFactor          = 1.2

	VolumeAvailableStatus = "AVAILABLE"
	VolumeInUseStatus     = "IN-USE"

	diskDetachInitDelay = 3 * time.Second
	diskDetachFactor    = 1.2
	diskDetachSteps     = 13

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
	vserverV1 := utils.NormalizeURL(vserverUrl) + "v1"
	vserverV2 := utils.NormalizeURL(vserverUrl) + "v2"
	pc, err := newVngCloud(iamURL, clientID, clientSecret)
	if err != nil {
		return nil, err
	}

	compute, _ := vngcloud.NewServiceClient(vserverV2, pc, "compute")
	volume, _ := vngcloud.NewServiceClient(vserverV2, pc, "volume")
	portal, _ := vngcloud.NewServiceClient(vserverV1, pc, "portal")

	return &cloud{
		compute:         compute,
		volume:          volume,
		portal:          portal,
		metadataService: metadataSvc,
	}, nil
}

func newVngCloud(iamURL, clientID, clientSecret string) (*client.ProviderClient, error) {
	identityUrl := utils.NormalizeURL(iamURL) + "v2"
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

	opts := lVol.NewListOpts(s.extraInfo.ProjectID, name, 0, 0)
	mc := metrics.NewMetricContext("volume", "list")
	err := lVol.List(s.volume, opts).EachPage(func(page pagination.IPage) (bool, error) {
		vols = append(vols, page.GetBody().(*lVol.ListResponse).ToListVolumeObjects()...)
		return true, nil
	})

	if mc.ObserveRequest(err) != nil {
		return nil, err
	}

	return vols, nil
}

func (s *cloud) CreateVolume(name string, size uint64, vtype, availability string, snapshotID string, sourcevolID string, tags *map[string]string) (*objects.Volume, error) {
	klog.Infof("CreateVolume; called with name %s, size %d, vtype %s, availability %s, snapshotID %s, sourcevolID %s, tags %+v", name, size, vtype, availability, snapshotID, sourcevolID, tags)

	opts := lVol.NewCreateOpts(
		s.extraInfo.ProjectID,
		&lVol.CreateOpts{
			Name:             name,
			Size:             size,
			VolumeTypeId:     vtype,
			CreatedFrom:      "NEW",
			PersistentVolume: true,
			MultiAttach:      false,
		})

	mc := metrics.NewMetricContext("volume", "create")
	vol, err := lVol.Create(s.volume, opts)

	if mc.ObserveRequest(err) != nil {
		return nil, err
	}

	return vol, nil
}

func (s *cloud) GetVolume(volumeID string) (*objects.Volume, error) {
	opts := lVol.NewGetOpts(s.extraInfo.ProjectID, volumeID)
	mc := metrics.NewMetricContext("volume", "get")
	result, err := lVol.Get(s.volume, opts)
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}

	return result, nil
}

func (s *cloud) DeleteVolume(volID string) error {
	used, err := s.diskIsUsed(volID)
	if err != nil {
		return err
	}

	if used {
		return fmt.Errorf("cannot delete the volume %q, it's still attached to a node", volID)
	}

	mc := metrics.NewMetricContext("volume", "delete")
	err = lVol.Delete(s.volume, lVol.NewDeleteOpts(s.extraInfo.ProjectID, volID))
	return mc.ObserveRequest(err)
}

func (s *cloud) AttachVolume(instanceID, volumeID string) (string, error) {
	vol, err := s.GetVolume(volumeID)
	if err != nil {
		return "", err
	}

	if vol == nil {
		return "", fmt.Errorf("volume %s not found", volumeID)
	}

	if vol.VmId != nil && *vol.VmId != "-1" {
		return "", fmt.Errorf("volume %s already attached to instance %s", volumeID, *vol.VmId)
	}

	mc := metrics.NewMetricContext("volume", "attach")
	opts := lVolAtch.NewCreateOpts(s.extraInfo.ProjectID, instanceID, volumeID)
	_, err = lVolAtch.Create(s.compute, opts)

	if mc.ObserveRequest(err) != nil {
		return "", err
	}

	return vol.VolumeId, nil
}

func (s *cloud) WaitDiskAttached(instanceID string, volumeID string) error {
	backoff := wait.Backoff{
		Duration: diskAttachInitDelay,
		Factor:   diskAttachFactor,
		Steps:    diskAttachSteps,
	}

	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		attached, err := s.diskIsAttached(instanceID, volumeID)
		if err != nil {
			return false, err
		}

		return attached, nil
	})

	if wait.Interrupted(err) {
		err = fmt.Errorf("interrupted while waiting for volume %s to be attached to instance within the alloted time %s", volumeID, instanceID)
	}

	return err
}

func (s *cloud) GetAttachmentDiskPath(instanceID, volumeID string) (string, error) {
	volume, err := s.GetVolume(volumeID)
	if err != nil {
		return "", err
	}

	if volume.Status != VolumeInUseStatus {
		return "", fmt.Errorf("volume %s not is in use", volumeID)
	}

	//if volume.VmId == nil || *volume.VmId != instanceID {
	//	return "", fmt.Errorf("volume %s is not attached to instance %s", volumeID, instanceID)
	//}

	return "", nil
}

func (s *cloud) DetachVolume(instanceID, volumeID string) error {
	volume, err := s.GetVolume(volumeID)
	if err != nil {
		return err
	}

	if volume == nil {
		return fmt.Errorf("volume %s not found", volumeID)
	}

	if volume.PersistentVolume != true {
		return fmt.Errorf("volume %s is not persistent volume", volumeID)
	}

	mc := metrics.NewMetricContext("volume", "detach")
	_, err = lVolAtch.Delete(s.compute, lVolAtch.NewDeleteOpts(s.extraInfo.ProjectID, instanceID, volumeID))

	if mc.ObserveRequest(err) != nil {
		return err
	}

	// Disk has no attachments or not attached to the provided compute
	return nil
}

func (s *cloud) WaitDiskDetached(instanceID string, volumeID string) error {
	backoff := wait.Backoff{
		Duration: diskDetachInitDelay,
		Factor:   diskDetachFactor,
		Steps:    diskDetachSteps,
	}

	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		attached, err := s.diskIsAttached(instanceID, volumeID)
		if err != nil {
			return false, err
		}

		return !attached, nil
	})

	if wait.Interrupted(err) {
		err = fmt.Errorf("interrupted while waiting for volume %s to be detached from instance within the alloted time %s", volumeID, instanceID)
	}

	return err
}

func (s *cloud) ExpandVolume(volumeTypeID, volumeID string, newSize uint64) error {
	opts := lVolAct.NewResizeOpts(s.extraInfo.ProjectID, volumeTypeID, volumeID, newSize)
	mc := metrics.NewMetricContext("volume", "extend")
	_, err := lVolAct.Resize(s.volume, opts)
	if mc.ObserveRequest(err) != nil {
		return err
	}

	return nil
}

func (s *cloud) WaitVolumeTargetStatus(volumeID string, tStatus []string) error {
	backoff := wait.Backoff{
		Duration: operationFinishInitDelay,
		Factor:   operationFinishFactor,
		Steps:    operationFinishSteps,
	}

	waitErr := wait.ExponentialBackoff(backoff, func() (bool, error) {
		vol, err := s.GetVolume(volumeID)
		if err != nil {
			return false, err
		}

		if vol == nil {
			return false, fmt.Errorf("volume %s not found", volumeID)
		}

		for _, t := range tStatus {
			if vol.Status == t {
				return true, nil
			}
		}

		return false, fmt.Errorf("WaitVolumeTargetStatus; volume %s status is %s", volumeID, vol.Status)
	})

	if wait.Interrupted(waitErr) {
		waitErr = fmt.Errorf("timeout on waiting for volume %s status to be in %v", volumeID, tStatus)
	}

	return waitErr
}

func (s *cloud) ResizeOrModifyDisk(volumeID string, newSizeBytes int64, options *ModifyDiskOptions) (newSize int64, err error) {
	return 0, nil
}

func (s *cloud) diskIsUsed(volumeID string) (bool, error) {
	vol, err := s.GetVolume(volumeID)
	if err != nil || vol == nil {
		return false, err
	}

	if vol.VmId != nil {
		return true, nil
	}

	return false, nil
}

func (s *cloud) diskIsAttached(instanceID string, volumeID string) (bool, error) {
	vol, err := s.GetVolume(volumeID)
	if err != nil || vol == nil {
		return false, err
	}

	if strings.ToUpper(vol.Status) != VolumeInUseStatus {
		return false, nil
	}

	return true, nil
}

// ModifyDiskOptions represents parameters to modify an EBS volume
type ModifyDiskOptions struct {
	VolumeType string
	IOPS       int
	Throughput int
}