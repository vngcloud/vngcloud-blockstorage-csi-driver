package vcontainer

import (
	"fmt"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/csi/metrics"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/csi/utils/metadata"
	"github.com/vngcloud/vngcloud-go-sdk/client"
	"github.com/vngcloud/vngcloud-go-sdk/vngcloud/objects"
	"github.com/vngcloud/vngcloud-go-sdk/vngcloud/pagination"
	lSnap "github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/blockstorage/v1/snapshot"
	lVolAct "github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/blockstorage/v2/extensions/volume_actions"
	lSnapV2 "github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/blockstorage/v2/snapshot"
	lVol "github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/blockstorage/v2/volume"
	lVolAtch "github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/compute/v2/extensions/volume_attach"
	lServer "github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/compute/v2/server"
	lPortal "github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/portal/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"strings"
)

type (
	vContainer struct {
		compute      *client.ServiceClient
		blockstorage *client.ServiceClient
		portal       *client.ServiceClient
		vBackUp      *client.ServiceClient
		bsOpts       BlockStorageOpts
		metadataOpts metadata.Opts
		extraInfo    *ExtraInfo
	}

	ExtraInfo struct {
		ProjectID string
		UserID    int64
	}
)

func (s *vContainer) GetMetadataOpts() metadata.Opts {
	klog.Infof("GetMetadataOpts; metadataOps is %+v", s.metadataOpts)
	return s.metadataOpts
}

func (s *vContainer) SetupPortalInfo(metadata metadata.IMetadata) error {
	projectID, err := metadata.GetProjectID()
	if err != nil {
		return err
	}
	klog.Infof("SetupPortalInfo; projectID is %s", projectID)

	portalInfo, err := lPortal.Get(s.portal, projectID)
	if err != nil {
		return err
	}

	if portalInfo == nil {
		return fmt.Errorf("can not get portal information")
	}

	s.extraInfo = &ExtraInfo{
		ProjectID: portalInfo.ProjectID,
		UserID:    portalInfo.UserID,
	}

	return nil
}

func (s *vContainer) ListVolumes(limit int, startingToken string) ([]*objects.Volume, string, error) {
	klog.Infof("ListVolumes; called with limit %d and startingToken %s", limit, startingToken)

	var nextPage string
	var vols []*objects.Volume

	//page, size := standardPaging(limit, startingToken)
	//opts := lVol.NewListOpts(s.extraInfo.ProjectID, "", page, size)
	//mc := metrics.NewMetricContext("volume", "list")
	//err := lVol.List(s.blockstorage, opts).EachPage(func(page pagination.IPage) (bool, error) {
	//	tfPage := page.GetBody().(*lVol.ListResponse)
	//	nextPage = tfPage.NextPage()
	//	vols = append(vols, tfPage.ToListVolumeObjects()...)
	//	return true, nil
	//})

	mc := metrics.NewMetricContext("volume", "list-all")
	vols, err := lVol.ListAll(s.blockstorage, lVol.NewListAllOpts(s.extraInfo.ProjectID))
	if mc.ObserveRequest(err) != nil {
		return nil, nextPage, err
	}

	return vols, nextPage, nil
}

func (s *vContainer) GetVolumesByName(name string) ([]*objects.Volume, error) {
	klog.Infof("GetVolumesByName; called with name %s", name)

	var vols []*objects.Volume

	opts := lVol.NewListOpts(s.extraInfo.ProjectID, name, 0, 0)
	mc := metrics.NewMetricContext("volume", "list")
	err := lVol.List(s.blockstorage, opts).EachPage(func(page pagination.IPage) (bool, error) {
		vols = append(vols, page.GetBody().(*lVol.ListResponse).ToListVolumeObjects()...)
		return true, nil
	})

	if mc.ObserveRequest(err) != nil {
		return nil, err
	}

	return vols, nil
}

func (s *vContainer) CreateVolume(name string, size uint64, vtype, availability string, snapshotID string, sourcevolID string, tags *map[string]string) (*objects.Volume, error) {
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
	vol, err := lVol.Create(s.blockstorage, opts)

	if mc.ObserveRequest(err) != nil {
		return nil, err
	}

	return vol, nil
}

func (s *vContainer) DeleteVolume(volID string) error {
	used, err := s.diskIsUsed(volID)
	if err != nil {
		return err
	}

	if used {
		return fmt.Errorf("cannot delete the volume %q, it's still attached to a node", volID)
	}

	mc := metrics.NewMetricContext("volume", "delete")
	err = lVol.Delete(s.blockstorage, lVol.NewDeleteOpts(s.extraInfo.ProjectID, volID))
	return mc.ObserveRequest(err)
}

func (s *vContainer) GetVolume(volumeID string) (*objects.Volume, error) {
	opts := lVol.NewGetOpts(s.extraInfo.ProjectID, volumeID)
	mc := metrics.NewMetricContext("volume", "get")
	result, err := lVol.Get(s.blockstorage, opts)
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}

	return result, nil
}

func (s *vContainer) GetInstanceByID(instanceID string) (*objects.Server, error) {
	opts := lServer.NewGetOpts(s.extraInfo.ProjectID, instanceID)
	mc := metrics.NewMetricContext("server", "get")
	result, err := lServer.Get(s.compute, opts)
	if mc.ObserveRequest(err.Error) != nil {
		return nil, err.Error
	}

	return result, nil
}

func (s *vContainer) AttachVolume(instanceID, volumeID string) (string, error) {
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

func (s *vContainer) GetAttachmentDiskPath(instanceID, volumeID string) (string, error) {
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

func (s *vContainer) WaitDiskAttached(instanceID string, volumeID string) error {
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

func (s *vContainer) DetachVolume(instanceID, volumeID string) error {
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

func (s *vContainer) WaitDiskDetached(instanceID string, volumeID string) error {
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

func (s *vContainer) ExpandVolume(volumeTypeID, volumeID string, newSize uint64) error {
	opts := lVolAct.NewResizeOpts(s.extraInfo.ProjectID, volumeTypeID, volumeID, newSize)
	mc := metrics.NewMetricContext("volume", "extend")
	_, err := lVolAct.Resize(s.blockstorage, opts)
	if mc.ObserveRequest(err) != nil {
		return err
	}

	return nil
}

func (s *vContainer) WaitVolumeTargetStatus(volumeID string, tStatus []string) error {
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

func (s *vContainer) GetMaxVolLimit() int64 {
	if s.bsOpts.NodeVolumeAttachLimit > 0 && s.bsOpts.NodeVolumeAttachLimit <= 256 {
		return s.bsOpts.NodeVolumeAttachLimit
	}

	return defaultMaxVolAttachLimit
}

func (s *vContainer) CreateSnapshot(name, volID string) (*objects.Snapshot, error) {
	opts := lSnapV2.NewCreateOpts(s.extraInfo.ProjectID, volID,
		fmt.Sprintf("vContainer volume snapshot of volume %s", volID), true, name, 7)
	mc := metrics.NewMetricContext("snapshot", "create")
	snap, err := lSnapV2.Create(s.blockstorage, opts)
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}

	return snap, nil
}

func (s *vContainer) WaitSnapshotReady(snapshotID string) error {
	backoff := wait.Backoff{
		Duration: snapReadyDuration,
		Factor:   snapReadyFactor,
		Steps:    snapReadySteps,
	}

	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		ready, err := s.snapshotIsReady(snapshotID)
		if err != nil {
			return false, err
		}
		return ready, nil
	})

	if wait.Interrupted(err) {
		err = fmt.Errorf("Timeout, Snapshot  %s is still not Ready %v", snapshotID, err)
	}

	return err
}

func (s *vContainer) ListSnapshots(page string, size int, volumeID, status, name string) ([]*objects.Snapshot, string, error) {
	var nextPage string
	var lstSnapshot []*objects.Snapshot

	p_, s_ := standardPaging(size, page)
	opts := lSnap.NewListOpts(p_, s_, volumeID, status, name)
	mc := metrics.NewMetricContext("snapshot", "list")
	fmt.Printf("The url is: %s\n", s.vBackUp.Endpoint)
	err := lSnap.List(s.vBackUp, opts).EachPage(func(page pagination.IPage) (bool, error) {
		tfPage := page.GetBody().(*lSnap.ListResponse)
		nextPage = tfPage.NextPage()
		lstSnapshot = append(lstSnapshot, tfPage.ToListSnapshotObjects()...)
		return true, nil
	})

	if mc.ObserveRequest(err) != nil {
		return nil, nextPage, err
	}

	return lstSnapshot, nextPage, nil
}

func (s *vContainer) GetSnapshotByID(snapshotID string) (*objects.Snapshot, error) {
	var snapshot *objects.Snapshot
	opts := lSnap.NewListOpts(1, 10, "", "", "")
	mc := metrics.NewMetricContext("snapshot", "get")
	err := lSnap.List(s.blockstorage, opts).EachPage(func(page pagination.IPage) (bool, error) {
		tfPage := page.GetBody().(*lSnap.ListResponse)
		for _, snap := range tfPage.ToListSnapshotObjects() {
			if snap.ID == snapshotID {
				snapshot = snap
				return false, nil
			}
		}

		return true, nil
	})

	if mc.ObserveRequest(err) != nil {
		return nil, err
	}

	return snapshot, nil
}

func (s *vContainer) DeleteSnapshot(volumeID, snapshotID string) error {
	opts := lSnapV2.NewDeleteOpts(s.extraInfo.ProjectID, volumeID, snapshotID)
	mc := metrics.NewMetricContext("snapshot", "delete")
	err := lSnapV2.Delete(s.blockstorage, opts)
	if mc.ObserveRequest(err) != nil {
		return err
	}

	return nil
}

func (s *vContainer) GetBlockStorageOpts() BlockStorageOpts {
	return s.bsOpts
}

func (s *vContainer) GetMappingVolume(volumeID string) (string, error) {
	opts := lVolAct.NewMappingOpts(s.extraInfo.ProjectID, volumeID)
	mc := metrics.NewMetricContext("volume-mapping", "get")
	result, err := lVolAct.GetMappingVolume(s.blockstorage, opts)
	if mc.ObserveRequest(err) != nil {
		return "", err
	}

	if result.UUID == "" {
		return "", fmt.Errorf("volume %s not found", volumeID)
	}

	return result.UUID, nil
}

func (s *vContainer) diskIsUsed(volumeID string) (bool, error) {
	vol, err := s.GetVolume(volumeID)
	if err != nil || vol == nil {
		return false, err
	}

	if vol.VmId != nil {
		return true, nil
	}

	return false, nil
}

func (s *vContainer) diskIsAttached(instanceID string, volumeID string) (bool, error) {
	vol, err := s.GetVolume(volumeID)
	if err != nil || vol == nil {
		return false, err
	}

	if strings.ToUpper(vol.Status) != VolumeInUseStatus {
		return false, nil
	}

	return true, nil
}

func (s *vContainer) snapshotIsReady(snapshotID string) (bool, error) {
	snap, err := s.GetSnapshotByID(snapshotID)
	if err != nil {
		return false, err
	}

	return strings.ToLower(snap.Status) == snapshotReadyStatus, nil
}
