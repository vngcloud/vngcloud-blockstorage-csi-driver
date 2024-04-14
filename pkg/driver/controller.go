package driver

import (
	lctx "context"
	lerr "errors"
	lfmt "fmt"
	lstrconv "strconv"
	lstr "strings"
	ltime "time"

	lcsi "github.com/container-storage-interface/spec/lib/go/csi"
	ljoat "github.com/cuongpiger/joat/parser"
	lvmrpc "github.com/vngcloud/vngcloud-csi-volume-modifier/pkg/rpc"
	lsdkEH "github.com/vngcloud/vngcloud-go-sdk/vngcloud/errors"
	lsdkObj "github.com/vngcloud/vngcloud-go-sdk/vngcloud/objects"
	lsdkSnapshotV2 "github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/blockstorage/v2/snapshot"
	lts "google.golang.org/protobuf/types/known/timestamppb"
	llog "k8s.io/klog/v2"

	lscloud "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/cloud"
	lsinternal "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/driver/internal"
	lsutil "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/util"
)

type controllerService struct {
	cloud               lscloud.Cloud
	inFlight            *lsinternal.InFlight
	modifyVolumeManager *modifyVolumeManager
	driverOptions       *DriverOptions

	lvmrpc.UnimplementedModifyServer
}

// newControllerService creates a new controller service it panics if failed to create the service
func newControllerService(driverOptions *DriverOptions) controllerService {
	metadata, err := NewMetadataFunc(lscloud.DefaultVServerMetadataClient)
	if err != nil {
		llog.ErrorS(err, "Could not determine the metadata information for the driver")
		panic(err)
	}

	cloudSrv, err := NewCloudFunc(driverOptions.identityURL, driverOptions.vServerURL, driverOptions.clientID, driverOptions.clientSecret, metadata)
	if err != nil {
		panic(err)
	}

	return controllerService{
		cloud:               cloudSrv,
		inFlight:            lsinternal.NewInFlight(),
		driverOptions:       driverOptions,
		modifyVolumeManager: newModifyVolumeManager(),
	}
}

func (s *controllerService) CreateVolume(_ lctx.Context, preq *lcsi.CreateVolumeRequest) (*lcsi.CreateVolumeResponse, error) {
	llog.V(5).InfoS("CreateVolume: called", "preq", *preq)

	// Validate the create volume request
	if err := validateCreateVolumeRequest(preq); err != nil {
		llog.ErrorS(err, "CreateVolume: invalid request")
		return nil, err
	}

	// Validate volume size, if volume size is less than the default volume size of cloud provider, set it to the default volume size
	volSizeBytes := getVolSizeBytes(preq)
	volName := preq.GetName()              // get the name of the volume, always in the format of pvc-<random-uuid>
	volCap := preq.GetVolumeCapabilities() // get volume capabilities
	multiAttach := isMultiAttach(volCap)   // check if the volume is multi-attach, true if multi-attach, false otherwise

	// check if a request is already in-flight
	if ok := s.inFlight.Insert(volName); !ok {
		llog.V(5).InfoS("CreateVolume: volume is already in-flight", "volumeName", volName)
		return nil, ErrVolumeIsCreating(volName)
	}
	defer s.inFlight.Delete(volName)

	vl, err := s.cloud.GetVolumeByName(volName)
	if err != nil {
		llog.ErrorS(err, "CreateVolume: failed to get volume", "volumeName", volName)
		return nil, ErrFailedToListVolumeByName(volName)
	}

	if vl != nil && len(vl.Items) == 1 {
		if vl.Items[0].Status != lscloud.VolumeAvailableStatus {
			llog.V(5).Infof("CreateVolume: volume %s is already in progress", volName)
			return nil, ErrVolumeIsCreating(volName)
		}
	} else if vl != nil && len(vl.Items) > 1 {
		llog.Errorf("Multiple volumes found with the same name %s", volName)
		return nil, ErrFailedToListVolumeByName(volName)
	}

	cvr := NewCreateVolumeRequest()
	parser, _ := ljoat.GetParser()
	for pk, pv := range preq.GetParameters() {
		switch lstr.ToLower(pk) {
		case VolumeTypeKey:
			cvr = cvr.WithVolumeTypeID(pv)
		case EncryptedKey:
			cvr = cvr.WithEncrypted(pv)
		case PVCNameKey:
			cvr = cvr.WithPvcNameTag(pv)
		case PVCNamespaceKey:
			cvr = cvr.WithPvcNamespaceTag(pv)
		case PVNameKey:
			cvr = cvr.WithPvNameTag(pv)
		case BlockSizeKey:
			if isAlphanumeric := parser.StringIsAlphanumeric(pv); !isAlphanumeric {
				return nil, ErrCanNotParseRequestArguments(BlockSizeKey, pv)
			}
			cvr = cvr.WithBlockSize(pv)
		case InodeSizeKey:
			if isAlphanumeric := parser.StringIsAlphanumeric(pv); !isAlphanumeric {
				return nil, ErrCanNotParseRequestArguments(InodeSizeKey, pv)
			}
			cvr = cvr.WithInodeSize(pv)
		case BytesPerInodeKey:
			if isAlphanumeric := parser.StringIsAlphanumeric(pv); !isAlphanumeric {
				return nil, ErrCanNotParseRequestArguments(BytesPerInodeKey, pv)
			}
			cvr = cvr.WithBytesPerInode(pv)
		case NumberOfInodesKey:
			if isAlphanumeric := parser.StringIsAlphanumeric(pv); !isAlphanumeric {
				return nil, ErrCanNotParseRequestArguments(NumberOfInodesKey, pv)
			}
			cvr = cvr.WithNumberOfInodes(pv)
		case Ext4ClusterSizeKey:
			if isAlphanumeric := parser.StringIsAlphanumeric(pv); !isAlphanumeric {
				return nil, ErrCanNotParseRequestArguments(Ext4ClusterSizeKey, pv)
			}
			cvr = cvr.WithExt4ClusterSize(pv)
		case Ext4BigAllocKey:
			cvr = cvr.WithExt4BigAlloc(pv == "true")
		case IsPoc:
			cvr = cvr.WithPoc(pv == "true")
		}
	}

	modifyOpts, err := parseModifyVolumeParameters(preq.GetMutableParameters())
	if err != nil {
		llog.ErrorS(err, "CreateVolume: invalid request")
		return nil, ErrModifyMutableParam
	}

	volumeSource := preq.GetVolumeContentSource()
	if volumeSource != nil {
		if _, ok := volumeSource.GetType().(*lcsi.VolumeContentSource_Snapshot); !ok {
			return nil, ErrVolumeContentSourceNotSupported
		}
		sourceSnapshot := volumeSource.GetSnapshot()
		if sourceSnapshot == nil {
			return nil, ErrSnapshotIsNil
		}
		cvr = cvr.WithSnapshotID(sourceSnapshot.GetSnapshotId())
	}

	respCtx, err := cvr.ToResponseContext(volCap)
	if err != nil {
		llog.ErrorS(err, "CreateVolume: failed to parse response context", "volumeID", volName)
		return nil, err
	}

	cvr = cvr.WithVolumeName(volName).
		WithMultiAttach(multiAttach).
		WithVolumeSize(uint64(lsutil.RoundUpSize(volSizeBytes, 1024*1024*1024))).
		WithVolumeTypeID(modifyOpts.VolumeType).
		WithClusterID(s.getClusterID())

	resp, err := s.cloud.CreateVolume(cvr.ToSdkCreateVolumeOpts(s.driverOptions))
	if err != nil {
		llog.ErrorS(err, "CreateVolume: failed to create volume", "volumeID", volName)
		return nil, err
	}

	return newCreateVolumeResponse(resp, cvr, respCtx), nil
}

func (s *controllerService) DeleteVolume(pctx lctx.Context, preq *lcsi.DeleteVolumeRequest) (*lcsi.DeleteVolumeResponse, error) {
	llog.V(4).InfoS("DeleteVolume: called", "args", *preq)
	if err := validateDeleteVolumeRequest(preq); err != nil {
		return nil, err
	}

	volumeID := preq.GetVolumeId()
	// check if a request is already in-flight
	if ok := s.inFlight.Insert(volumeID); !ok {
		return nil, ErrOperationAlreadyExists(volumeID)
	}
	defer s.inFlight.Delete(volumeID)

	if err := s.cloud.DeleteVolume(volumeID); err != nil {
		if err != nil {
			llog.ErrorS(err, "DeleteVolume: failed to delete volume", "volumeID", volumeID)
			return nil, ErrFailedToDeleteVolume(volumeID)
		}
	}

	return &lcsi.DeleteVolumeResponse{}, nil
}

func (s *controllerService) ControllerPublishVolume(pctx lctx.Context, preq *lcsi.ControllerPublishVolumeRequest) (result *lcsi.ControllerPublishVolumeResponse, err error) {
	llog.V(5).InfoS("ControllerPublishVolume: called", "preq", *preq)

	if err = validateControllerPublishVolumeRequest(preq); err != nil {
		llog.ErrorS(err, "ControllerPublishVolume: invalid request")
		return nil, err
	}

	volumeID := preq.GetVolumeId() // get the cloud volume ID
	nodeID := preq.GetNodeId()     // get the cloud node ID

	// Make sure there are no 2 operations on the same volume and node at the same time
	if !s.inFlight.Insert(volumeID + nodeID) {
		return nil, ErrOperationAlreadyExists(volumeID)
	}
	defer s.inFlight.Delete(volumeID + nodeID)

	llog.V(2).InfoS("ControllerPublishVolume: attaching volume into the instance", "volumeID", volumeID, "nodeID", nodeID)

	// Attach the volume and wait for it to be attached
	_, err = s.cloud.AttachVolume(nodeID, volumeID)
	if err != nil {
		llog.ErrorS(err, "ControllerPublishVolume; failed to attach volume to instance", "volumeID", volumeID, "nodeID", nodeID)
		return nil, ErrAttachVolume(volumeID, nodeID)
	}

	devicePath, err := s.cloud.GetDeviceDiskID(volumeID)
	if err != nil {
		llog.ErrorS(err, "ControllerPublishVolume; failed to get device path for volume", "volumeID", volumeID)
		return nil, ErrFailedToGetDevicePath(volumeID, nodeID)
	}

	llog.V(5).InfoS("ControllerPublishVolume; volume attached to instance successfully", "volumeID", volumeID, "nodeID", nodeID)
	return newControllerPublishVolumeResponse(devicePath), nil
}

func (s *controllerService) ControllerUnpublishVolume(_ lctx.Context, preq *lcsi.ControllerUnpublishVolumeRequest) (*lcsi.ControllerUnpublishVolumeResponse, error) {
	llog.V(4).InfoS("ControllerUnpublishVolume: called", "preq", *preq)

	if err := validateControllerUnpublishVolumeRequest(preq); err != nil {
		llog.ErrorS(err, "ControllerUnpublishVolume: invalid request")
		return nil, err
	}

	volumeID := preq.GetVolumeId()
	nodeID := preq.GetNodeId()

	if !s.inFlight.Insert(volumeID + nodeID) {
		return nil, ErrOperationAlreadyExists(volumeID)
	}
	defer s.inFlight.Delete(volumeID + nodeID)

	if volumeID == "" {
		llog.Errorf("ControllerUnpublishVolume: VolumeID is required")
		return nil, ErrVolumeIDNotProvided
	}

	_, getErr := s.cloud.GetVolume(volumeID)
	if getErr != nil && getErr.Code == lsdkEH.ErrCodeVolumeNotFound {
		llog.InfoS("ControllerUnpublishVolume: volume not found", "volumeID", volumeID)
		return &lcsi.ControllerUnpublishVolumeResponse{}, nil
	}

	err := s.cloud.DetachVolume(nodeID, volumeID)
	if err != nil {
		llog.ErrorS(err, "ControllerUnpublishVolume: failed to detach volume from instance", "volumeID", volumeID, "nodeID", nodeID)
		return nil, ErrDetachVolume(volumeID, nodeID)
	}

	llog.V(4).InfoS("ControllerUnpublishVolume: volume detached from instance successfully", "volumeID", volumeID, "nodeID", nodeID)
	return &lcsi.ControllerUnpublishVolumeResponse{}, nil
}

func (s *controllerService) CreateSnapshot(_ lctx.Context, preq *lcsi.CreateSnapshotRequest) (*lcsi.CreateSnapshotResponse, error) {
	llog.V(4).InfoS("CreateSnapshot: called", "preq", *preq)
	if err := validateCreateSnapshotRequest(preq); err != nil {
		llog.ErrorS(err, "CreateSnapshot: invalid request")
		return nil, err
	}

	snapshotName := preq.GetName()
	volumeID := preq.GetSourceVolumeId()

	// check if a request is already in-flight
	if ok := s.inFlight.Insert(snapshotName); !ok {
		return nil, ErrOperationAlreadyExists(volumeID)
	}
	defer s.inFlight.Delete(snapshotName)

	snapshot, err := s.cloud.GetVolumeSnapshotByName(volumeID, snapshotName)
	if err != nil {
		if !lerr.Is(err, lscloud.ErrSnapshotNotFound) {
			llog.ErrorS(err, "Error looking for the snapshot", "snapshotName", snapshotName)
			return nil, err
		}
	}

	if snapshot != nil {
		return newCreateSnapshotResponse(snapshot)
	}

	snapshot, err = s.cloud.CreateSnapshotFromVolume(volumeID,
		&lsdkSnapshotV2.CreateOpts{
			Name:        snapshotName,
			Description: lfmt.Sprintf(patternSnapshotDescription, volumeID, s.getClusterID()),
			Permanently: true,
		},
	)

	if err != nil {
		llog.ErrorS(err, "CreateSnapshot: Error creating snapshot", "snapshotName", snapshotName, "volumeID", volumeID)
		return nil, err
	}

	return newCreateSnapshotResponse(snapshot)
}

func (s *controllerService) DeleteSnapshot(_ lctx.Context, preq *lcsi.DeleteSnapshotRequest) (*lcsi.DeleteSnapshotResponse, error) {
	llog.V(4).InfoS("DeleteSnapshot: called", "preq", *preq)

	if err := validateDeleteSnapshotRequest(preq); err != nil {
		llog.ErrorS(err, "DeleteSnapshot: invalid request")
		return nil, err
	}

	snapshotID := preq.GetSnapshotId()

	// check if a request is already in-flight
	if ok := s.inFlight.Insert(snapshotID); !ok {
		return nil, ErrSnapshotIsDeleting(snapshotID)
	}
	defer s.inFlight.Delete(snapshotID)

	if err := s.cloud.DeleteSnapshot(snapshotID); err != nil {
		llog.ErrorS(err, "DeleteSnapshot: Error deleting snapshot", "snapshotID", snapshotID)
		return nil, ErrFailedToDeleteSnapshot(snapshotID)
	}

	llog.V(5).InfoS("DeleteSnapshot: snapshot deleted successfully", "snapshotID", snapshotID)
	return &lcsi.DeleteSnapshotResponse{}, nil
}

func (s *controllerService) ListSnapshots(_ lctx.Context, preq *lcsi.ListSnapshotsRequest) (*lcsi.ListSnapshotsResponse, error) {
	llog.V(4).InfoS("ListSnapshots: called", "preq", *preq)

	volumeID := preq.GetSourceVolumeId()
	nextToken := parsePage(preq.GetStartingToken())
	maxEntries := int(preq.GetMaxEntries())

	cloudSnapshots, err := s.cloud.ListSnapshots(volumeID, nextToken, maxEntries)
	if err != nil {
		llog.ErrorS(err, "ListSnapshots: Error listing snapshots", "volumeID", volumeID, "nextToken", nextToken, "maxEntries", maxEntries)
		return nil, ErrFailedToListSnapshot(volumeID)
	}

	response := newListSnapshotsResponse(cloudSnapshots)
	return response, nil
}

func (s *controllerService) ValidateVolumeCapabilities(pctx lctx.Context, preq *lcsi.ValidateVolumeCapabilitiesRequest) (*lcsi.ValidateVolumeCapabilitiesResponse, error) {
	llog.V(4).InfoS("ValidateVolumeCapabilities: called", "preq", *preq)

	volumeID := preq.GetVolumeId()
	if volumeID == "" {
		return nil, ErrVolumeIDNotProvided
	}

	volCaps := preq.GetVolumeCapabilities()
	if len(volCaps) < 1 {
		return nil, ErrVolumeCapabilitiesNotProvided
	}

	if _, err := s.cloud.GetVolume(volumeID); err != nil {
		if err.Code == lsdkEH.ErrCodeVolumeNotFound {
			return nil, ErrVolumeNotFound(volumeID)
		}

		return nil, ErrFailedToGetVolume(volumeID)
	}

	var confirmed *lcsi.ValidateVolumeCapabilitiesResponse_Confirmed
	if isValidVolumeCapabilities(volCaps) {
		confirmed = &lcsi.ValidateVolumeCapabilitiesResponse_Confirmed{VolumeCapabilities: volCaps}
	}
	return &lcsi.ValidateVolumeCapabilitiesResponse{
		Confirmed: confirmed,
	}, nil
}

func (s *controllerService) ControllerGetCapabilities(ctx lctx.Context, req *lcsi.ControllerGetCapabilitiesRequest) (*lcsi.ControllerGetCapabilitiesResponse, error) {
	llog.V(4).InfoS("ControllerGetCapabilities: called", "args", *req)
	var caps []*lcsi.ControllerServiceCapability
	for _, capa := range controllerCaps {
		c := &lcsi.ControllerServiceCapability{
			Type: &lcsi.ControllerServiceCapability_Rpc{
				Rpc: &lcsi.ControllerServiceCapability_RPC{
					Type: capa,
				},
			},
		}
		caps = append(caps, c)
	}
	return &lcsi.ControllerGetCapabilitiesResponse{Capabilities: caps}, nil
}

func (s *controllerService) ControllerExpandVolume(_ lctx.Context, preq *lcsi.ControllerExpandVolumeRequest) (*lcsi.ControllerExpandVolumeResponse, error) {
	llog.V(4).InfoS("ControllerExpandVolume: called", "preq", *preq)

	volumeID := preq.GetVolumeId()
	if volumeID == "" {
		return nil, ErrVolumeIDNotProvided
	}

	// check if a request is already in-flight
	if ok := s.inFlight.Insert(volumeID); !ok {
		return nil, ErrOperationAlreadyExists(volumeID)
	}
	defer s.inFlight.Delete(volumeID)

	capRange := preq.GetCapacityRange()
	if capRange == nil {
		llog.Errorf("ControllerExpandVolume: Capacity range is required")
		return nil, ErrCapacityRangeNotProvided
	}

	volSizeBytes := preq.GetCapacityRange().GetRequiredBytes()
	volSizeGB := uint64(lsutil.RoundUpSize(volSizeBytes, 1024*1024*1024))
	maxVolSize := capRange.GetLimitBytes()

	if maxVolSize > 0 && volSizeBytes > maxVolSize {
		llog.Errorf("ControllerExpandVolume: Requested size %d exceeds limit %d", volSizeBytes, maxVolSize)
		return nil, ErrRequestExceedLimit(volSizeBytes, maxVolSize)
	}

	volume, err := s.cloud.GetVolume(volumeID)
	if err != nil {
		llog.ErrorS(err.Error, "ControllerExpandVolume: failed to get volume", "volumeID", volumeID)
		return nil, ErrFailedToGetVolume(volumeID)
	}

	if volume == nil {
		llog.Errorf("ControllerExpandVolume: volume %s not found", volumeID)
		return nil, ErrVolumeNotFound(volumeID)
	}

	if volume.Size >= volSizeGB {
		llog.V(2).Infof("ControllerExpandVolume; volume %s already has size %d GiB", volumeID, volume.Size)
		return &lcsi.ControllerExpandVolumeResponse{
			CapacityBytes:         int64(volume.Size * 1024 * 1024 * 1024),
			NodeExpansionRequired: true,
		}, nil
	}

	llog.V(5).InfoS("ControllerExpandVolume: expanding volume", "volumeID", volumeID, "newSize", volSizeGB)
	// Expand the volume
	err1 := s.cloud.ExpandVolume(volumeID, volume.VolumeTypeID, volSizeGB)
	if err1 != nil {
		llog.ErrorS(err1, "ControllerExpandVolume: failed to expand volume", "volumeID", volumeID)
		return nil, ErrFailedToExpandVolume(volumeID, int64(volSizeGB))
	}

	llog.V(4).InfoS("ControllerExpandVolume: volume expanded successfully", "volumeID", volumeID, "newSize", volSizeGB)
	return &lcsi.ControllerExpandVolumeResponse{
		CapacityBytes:         volSizeBytes,
		NodeExpansionRequired: true,
	}, nil
}

func (s *controllerService) ControllerModifyVolume(ctx lctx.Context, preq *lcsi.ControllerModifyVolumeRequest) (*lcsi.ControllerModifyVolumeResponse, error) {
	llog.V(4).InfoS("ControllerModifyVolume: called", "preq", *preq)

	volumeID := preq.GetVolumeId()
	if volumeID == "" {
		return nil, ErrVolumeIDNotProvided
	}

	options, err := parseModifyVolumeParameters(preq.GetMutableParameters())
	if err != nil {
		llog.ErrorS(err, "ControllerModifyVolume: invalid request")
		return nil, err
	}

	err = s.modifyVolumeWithCoalescing(ctx, volumeID, options)
	if err != nil {
		llog.ErrorS(err, "ControllerModifyVolume: failed to modify volume", "volumeID", volumeID)
		return nil, err
	}

	return &lcsi.ControllerModifyVolumeResponse{}, nil
}

func (s *controllerService) ModifyVolumeProperties(pctx lctx.Context, preq *lvmrpc.ModifyVolumePropertiesRequest) (*lvmrpc.ModifyVolumePropertiesResponse, error) {
	llog.V(5).InfoS("ModifyVolumeProperties: called", "preq", preq)

	if err := validateModifyVolumePropertiesRequest(preq); err != nil {
		return nil, err
	}

	options, _ := parseModifyVolumeParameters(preq.GetParameters())
	volumeID := preq.GetName()

	// check if a request is already in-flight
	if ok := s.inFlight.Insert(volumeID); !ok {
		return nil, ErrOperationAlreadyExists(volumeID)
	}
	defer s.inFlight.Delete(volumeID)

	volume, errSdk := s.cloud.GetVolume(volumeID)
	if errSdk != nil {
		llog.ErrorS(errSdk.Error, "ModifyVolumeProperties: failed to get volume", "volumeID", volumeID)
		return nil, ErrFailedToGetVolume(volumeID)
	}

	if volume.VolumeTypeID == options.VolumeType {
		llog.V(2).Infof("ModifyVolumeProperties: volume %s already has volume type %s", volumeID, options.VolumeType)
		return &lvmrpc.ModifyVolumePropertiesResponse{}, nil
	}

	err := s.cloud.ExpandVolume(volumeID, options.VolumeType, volume.Size)
	if err != nil {
		llog.ErrorS(err, "ModifyVolumeProperties: failed to modify volume", "volumeID", volumeID)
		return nil, ErrFailedToModifyVolume(volumeID)
	}

	return &lvmrpc.ModifyVolumePropertiesResponse{}, nil
}

func (s *controllerService) GetCSIDriverModificationCapability(_ lctx.Context, _ *lvmrpc.GetCSIDriverModificationCapabilityRequest) (*lvmrpc.GetCSIDriverModificationCapabilityResponse, error) {
	return &lvmrpc.GetCSIDriverModificationCapabilityResponse{}, nil
}

func (s *controllerService) ControllerGetVolume(_ lctx.Context, preq *lcsi.ControllerGetVolumeRequest) (*lcsi.ControllerGetVolumeResponse, error) {
	llog.V(4).InfoS("ControllerGetVolume: called", "preq", *preq)
	return nil, ErrNotImplemented("ControllerGetVolume")
}

func (s *controllerService) ListVolumes(ctx lctx.Context, req *lcsi.ListVolumesRequest) (*lcsi.ListVolumesResponse, error) {
	llog.V(4).InfoS("ListVolumes: called", "args", *req)
	return nil, ErrNotImplemented("ListVolumes")
}

func (s *controllerService) GetCapacity(ctx lctx.Context, req *lcsi.GetCapacityRequest) (*lcsi.GetCapacityResponse, error) {
	return nil, ErrNotImplemented("GetCapacity")
}

func (s *controllerService) getClusterID() string {
	return s.driverOptions.clusterID
}

func newCreateVolumeResponse(disk *lsdkObj.Volume, pcvr *CreateVolumeRequest, prespCtx map[string]string) *lcsi.CreateVolumeResponse {
	var vcs *lcsi.VolumeContentSource
	if pcvr.SnapshotID != "" {
		vcs = &lcsi.VolumeContentSource{
			Type: &lcsi.VolumeContentSource_Snapshot{
				Snapshot: &lcsi.VolumeContentSource_SnapshotSource{
					SnapshotId: pcvr.SnapshotID,
				},
			},
		}
	}

	return &lcsi.CreateVolumeResponse{
		Volume: &lcsi.Volume{
			VolumeId:      disk.VolumeId,
			CapacityBytes: int64(disk.Size * 1024 * 1024 * 1024),
			VolumeContext: prespCtx,
			ContentSource: vcs,
		},
	}
}

func newCreateSnapshotResponse(snapshot *lsdkObj.Snapshot) (*lcsi.CreateSnapshotResponse, error) {
	creationTime, err := ltime.Parse("2006-01-02T15:04:05.000-07:00", snapshot.CreatedAt)
	if err != nil {
		creationTime = ltime.Now()
	}

	return &lcsi.CreateSnapshotResponse{
		Snapshot: &lcsi.Snapshot{
			SnapshotId:     snapshot.ID,
			SourceVolumeId: snapshot.VolumeID,
			SizeBytes:      snapshot.Size * lsutil.GiB,
			CreationTime:   lts.New(creationTime),
			ReadyToUse:     true,
		},
	}, nil
}

func newListSnapshotsResponse(psnapshotList *lsdkObj.SnapshotList) *lcsi.ListSnapshotsResponse {
	var entries []*lcsi.ListSnapshotsResponse_Entry
	for _, snapshot := range psnapshotList.Items {
		snapshotResponseEntry := newListSnapshotsResponseEntry(&snapshot)
		entries = append(entries, snapshotResponseEntry)
	}

	nextToken := ""
	if psnapshotList.Page < psnapshotList.TotalPages {
		nextToken = lstrconv.Itoa(psnapshotList.Page + 1)
	}

	return &lcsi.ListSnapshotsResponse{
		Entries:   entries,
		NextToken: nextToken,
	}
}

func newListSnapshotsResponseEntry(snapshot *lsdkObj.Snapshot) *lcsi.ListSnapshotsResponse_Entry {
	creationTime, err := ltime.Parse("2006-01-02T15:04:05.000-07:00", snapshot.CreatedAt)
	if err != nil {
		creationTime = ltime.Now()
	}

	return &lcsi.ListSnapshotsResponse_Entry{
		Snapshot: &lcsi.Snapshot{
			SnapshotId:     snapshot.ID,
			SourceVolumeId: snapshot.VolumeID,
			SizeBytes:      snapshot.Size * lsutil.GiB,
			CreationTime:   lts.New(creationTime),
			ReadyToUse:     snapshot.Status == lscloud.SnapshotActiveStatus,
		},
	}
}

func newControllerPublishVolumeResponse(pdevicePath string) *lcsi.ControllerPublishVolumeResponse {
	return &lcsi.ControllerPublishVolumeResponse{
		PublishContext: map[string]string{
			DevicePathKey: pdevicePath,
		},
	}
}

func parsePage(nextToken string) int {
	if nextToken == "" {
		return 1
	}

	page, err := lstrconv.Atoi(nextToken)
	if err != nil {
		return 1
	}

	return page
}
