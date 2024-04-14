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
	lsdkVolV2 "github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/blockstorage/v2/volume"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"k8s.io/klog/v2"

	lscloud "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/cloud"
	lsinternal "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/driver/internal"
	lsutil "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/util"
)

var (
	// NewMetadataFunc is a variable for the cloud.NewMetadata function that can
	// be overwritten in unit tests.
	NewMetadataFunc = lscloud.NewMetadataService
	NewCloudFunc    = lscloud.NewCloud
)

var (
	// controllerCaps represents the capability of controller service
	controllerCaps = []lcsi.ControllerServiceCapability_RPC_Type{
		lcsi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		lcsi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
		lcsi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT,
		lcsi.ControllerServiceCapability_RPC_LIST_SNAPSHOTS,
		lcsi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
		lcsi.ControllerServiceCapability_RPC_MODIFY_VOLUME,
	}
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
		klog.ErrorS(err, "Could not determine the metadata information for the driver")
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
	klog.V(5).InfoS("CreateVolume: called", "preq", *preq)

	// Validate the create volume request
	if err := validateCreateVolumeRequest(preq); err != nil {
		klog.ErrorS(err, "CreateVolume: invalid request")
		return nil, err
	}

	// Validate volume size, if volume size is less than the default volume size of cloud provider, set it to the default volume size
	volSizeBytes := getVolSizeBytes(preq)
	volName := preq.GetName()              // get the name of the volume, always in the format of pvc-<random-uuid>
	volCap := preq.GetVolumeCapabilities() // get volume capabilities
	multiAttach := isMultiAttach(volCap)   // check if the volume is multi-attach, true if multi-attach, false otherwise

	// check if a request is already in-flight
	if ok := s.inFlight.Insert(volName); !ok {
		klog.V(5).InfoS("CreateVolume: volume is already in-flight", "volumeName", volName)
		return nil, ErrVolumeIsCreating(volName)
	}
	defer s.inFlight.Delete(volName)

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
		klog.ErrorS(err, "CreateVolume: invalid request")
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
		klog.ErrorS(err, "CreateVolume: failed to parse response context", "volumeID", volName)
		return nil, err
	}

	cvr = cvr.WithVolumeName(volName).
		WithMultiAttach(multiAttach).
		WithVolumeSize(uint64(lsutil.RoundUpSize(volSizeBytes, 1024*1024*1024))).
		WithVolumeTypeID(modifyOpts.VolumeType).
		WithClusterID(s.getClusterID())

	resp, err := s.cloud.CreateVolume(cvr.ToSdkCreateVolumeOpts(s.driverOptions))
	if err != nil {
		klog.ErrorS(err, "CreateVolume: failed to create volume", "volumeID", volName)
		return nil, err
	}

	return newCreateVolumeResponse(resp, respCtx), nil
}

func (s *controllerService) DeleteVolume(pctx lctx.Context, preq *lcsi.DeleteVolumeRequest) (*lcsi.DeleteVolumeResponse, error) {
	klog.V(4).InfoS("DeleteVolume: called", "args", *preq)
	if err := validateDeleteVolumeRequest(preq); err != nil {
		return nil, err
	}

	volumeID := preq.GetVolumeId()
	// check if a request is already in-flight
	if ok := s.inFlight.Insert(volumeID); !ok {
		msg := lfmt.Sprintf(lsinternal.VolumeOperationAlreadyExistsErrorMsg, volumeID)
		return nil, status.Error(codes.Aborted, msg)
	}
	defer s.inFlight.Delete(volumeID)

	if err := s.cloud.DeleteVolume(volumeID); err != nil {
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Could not delete volume ID %q: %v", volumeID, err)
		}
	}

	return &lcsi.DeleteVolumeResponse{}, nil
}

func (s *controllerService) ControllerPublishVolume(pctx lctx.Context, preq *lcsi.ControllerPublishVolumeRequest) (result *lcsi.ControllerPublishVolumeResponse, err error) {
	klog.V(5).InfoS("ControllerPublishVolume: called", "args", *preq)

	if err = validateControllerPublishVolumeRequest(preq); err != nil {
		klog.ErrorS(err, "ControllerPublishVolume: invalid request")
		return nil, err
	}

	volumeID := preq.GetVolumeId() // get the cloud volume ID
	nodeID := preq.GetNodeId()     // get the cloud node ID

	// Make sure there are no 2 operations on the same volume and node at the same time
	if !s.inFlight.Insert(volumeID + nodeID) {
		return nil, status.Error(codes.Aborted, lfmt.Sprintf(lsinternal.VolumeOperationAlreadyExistsErrorMsg, volumeID))
	}
	defer s.inFlight.Delete(volumeID + nodeID)

	klog.V(2).InfoS("ControllerPublishVolume: attaching volume into the instance", "volumeID", volumeID, "nodeID", nodeID)

	// Attach the volume and wait for it to be attached
	_, err = s.cloud.AttachVolume(nodeID, volumeID)
	if err != nil {
		klog.ErrorS(err, "ControllerPublishVolume; failed to attach volume to instance", "volumeID", volumeID, "nodeID", nodeID)
		return nil, status.Error(codes.Internal, lfmt.Sprintf("failed to attach volume; ERR: %v", err))
	}

	devicePath, err := s.cloud.GetDeviceDiskID(volumeID)
	if err != nil {
		klog.ErrorS(err, "ControllerPublishVolume; failed to get device path for volume %s", volumeID)
		return nil, status.Error(codes.Internal, lfmt.Sprintf("failed to get device path for volume; ERR: %v", err))
	}

	klog.V(5).InfoS("ControllerPublishVolume; volume attached to instance successfully", "volumeID", volumeID, "nodeID", nodeID)
	return newControllerPublishVolumeResponse(devicePath), nil
}

func (s *controllerService) ControllerUnpublishVolume(ctx lctx.Context, preq *lcsi.ControllerUnpublishVolumeRequest) (*lcsi.ControllerUnpublishVolumeResponse, error) {
	klog.V(4).InfoS("ControllerUnpublishVolume: called", "preq", *preq)

	if err := validateControllerUnpublishVolumeRequest(preq); err != nil {
		return nil, err
	}

	volumeID := preq.GetVolumeId()
	nodeID := preq.GetNodeId()

	if !s.inFlight.Insert(volumeID + nodeID) {
		return nil, status.Error(codes.Aborted, lfmt.Sprintf(lsinternal.VolumeOperationAlreadyExistsErrorMsg, volumeID))
	}
	defer s.inFlight.Delete(volumeID + nodeID)
	if volumeID == "" {
		klog.Errorf("ControllerUnpublishVolume; VolumeID is required")
		return nil, status.Error(codes.InvalidArgument, "Volume ID is required")
	}

	_, getErr := s.cloud.GetVolume(volumeID)
	if getErr != nil && getErr.Code == lsdkEH.ErrCodeVolumeNotFound {
		klog.InfoS("ControllerUnpublishVolume; volume not found", "volumeID", volumeID)
		return &lcsi.ControllerUnpublishVolumeResponse{}, nil
	}

	err := s.cloud.DetachVolume(nodeID, volumeID)
	if err != nil {
		klog.ErrorS(err, "ControllerUnpublishVolume; failed to detach volume from instance", "volumeID", volumeID, "nodeID", nodeID)
		return nil, status.Error(codes.Internal, lfmt.Sprintf("failed to detach volume; ERR: %v", err))
	}

	klog.V(4).InfoS("ControllerUnpublishVolume; volume detached from instance successfully", "volumeID", volumeID, "nodeID", nodeID)
	return &lcsi.ControllerUnpublishVolumeResponse{}, nil
}

func (s *controllerService) CreateSnapshot(_ lctx.Context, preq *lcsi.CreateSnapshotRequest) (*lcsi.CreateSnapshotResponse, error) {
	klog.V(4).InfoS("CreateSnapshot: called", "args", preq)
	if err := validateCreateSnapshotRequest(preq); err != nil {
		return nil, err
	}

	snapshotName := preq.GetName()
	volumeID := preq.GetSourceVolumeId()

	// check if a request is already in-flight
	if ok := s.inFlight.Insert(snapshotName); !ok {
		msg := lfmt.Sprintf(lsinternal.VolumeOperationAlreadyExistsErrorMsg, snapshotName)
		return nil, status.Error(codes.Aborted, msg)
	}
	defer s.inFlight.Delete(snapshotName)

	snapshot, err := s.cloud.GetVolumeSnapshotByName(volumeID, snapshotName)
	if err != nil {
		if !lerr.Is(err, lscloud.ErrSnapshotNotFound) {
			klog.ErrorS(err, "Error looking for the snapshot", "snapshotName", snapshotName)
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
		klog.ErrorS(err, "CreateSnapshot: Error creating snapshot", "snapshotName", snapshotName, "volumeID", volumeID)
		return nil, err
	}

	return newCreateSnapshotResponse(snapshot)
}

func (s *controllerService) DeleteSnapshot(_ lctx.Context, preq *lcsi.DeleteSnapshotRequest) (*lcsi.DeleteSnapshotResponse, error) {
	klog.V(4).InfoS("DeleteSnapshot: called", "preq", preq)
	if err := validateDeleteSnapshotRequest(preq); err != nil {
		return nil, err
	}

	snapshotID := preq.GetSnapshotId()

	// check if a request is already in-flight
	if ok := s.inFlight.Insert(snapshotID); !ok {
		return nil, status.Error(
			codes.Aborted,
			lfmt.Sprintf("DeleteSnapshot for Snapshot %s is already in progress", snapshotID))
	}
	defer s.inFlight.Delete(snapshotID)

	if err := s.cloud.DeleteSnapshot(snapshotID); err != nil {
		klog.Error(err, "DeleteSnapshot: Error deleting snapshot", "snapshotID", snapshotID)
		return nil, status.Errorf(codes.Internal, "Could not delete snapshot ID %q: %v", snapshotID, err)
	}

	klog.V(5).InfoS("DeleteSnapshot: snapshot deleted successfully", "snapshotID", snapshotID)
	return &lcsi.DeleteSnapshotResponse{}, nil
}

func (s *controllerService) ListSnapshots(_ lctx.Context, preq *lcsi.ListSnapshotsRequest) (*lcsi.ListSnapshotsResponse, error) {
	klog.V(4).InfoS("ListSnapshots: called", "args", preq)

	volumeID := preq.GetSourceVolumeId()
	nextToken := parsePage(preq.GetStartingToken())
	maxEntries := int(preq.GetMaxEntries())

	cloudSnapshots, err := s.cloud.ListSnapshots(volumeID, nextToken, maxEntries)
	if err != nil {
		klog.ErrorS(err, "ListSnapshots: Error listing snapshots", "volumeID", volumeID)
		return nil, status.Errorf(codes.Internal, "Could not list snapshots: %v", err)
	}

	response := newListSnapshotsResponse(cloudSnapshots)
	return response, nil
}

func (s *controllerService) ValidateVolumeCapabilities(pctx lctx.Context, preq *lcsi.ValidateVolumeCapabilitiesRequest) (*lcsi.ValidateVolumeCapabilitiesResponse, error) {
	klog.V(4).InfoS("ValidateVolumeCapabilities: called", "args", *preq)
	volumeID := preq.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID not provided")
	}

	volCaps := preq.GetVolumeCapabilities()
	if len(volCaps) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume capabilities not provided")
	}

	if _, err := s.cloud.GetVolume(volumeID); err != nil {
		if err.Code == lsdkVolV2.ErrVolumeNotFound {
			return nil, status.Error(codes.NotFound, "Volume not found")
		}

		return nil, status.Errorf(codes.Internal, "Could not get volume with ID %q: %v", volumeID, err)
	}

	var confirmed *lcsi.ValidateVolumeCapabilitiesResponse_Confirmed
	if isValidVolumeCapabilities(volCaps) {
		confirmed = &lcsi.ValidateVolumeCapabilitiesResponse_Confirmed{VolumeCapabilities: volCaps}
	}
	return &lcsi.ValidateVolumeCapabilitiesResponse{
		Confirmed: confirmed,
	}, nil
}

func (s *controllerService) ListVolumes(ctx lctx.Context, req *lcsi.ListVolumesRequest) (*lcsi.ListVolumesResponse, error) {
	klog.V(4).InfoS("ListVolumes: called", "args", *req)
	return nil, status.Error(codes.Unimplemented, "")
}

func (s *controllerService) GetCapacity(ctx lctx.Context, req *lcsi.GetCapacityRequest) (*lcsi.GetCapacityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "GetCapacity is not yet implemented")
}

func (s *controllerService) ControllerGetCapabilities(ctx lctx.Context, req *lcsi.ControllerGetCapabilitiesRequest) (*lcsi.ControllerGetCapabilitiesResponse, error) {
	klog.V(4).InfoS("ControllerGetCapabilities: called", "args", *req)
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
	klog.V(4).Infof("ControllerExpandVolume: called with request %+v", preq)

	volumeID := preq.GetVolumeId()
	if volumeID == "" {
		klog.Errorf("ControllerExpandVolume: Volume ID is required")
		return nil, status.Error(codes.InvalidArgument, "Volume ID is required")
	}

	// check if a request is already in-flight
	if ok := s.inFlight.Insert(volumeID); !ok {
		msg := lfmt.Sprintf("ControllerExpandVolume: "+lsinternal.VolumeOperationAlreadyExistsErrorMsg, volumeID)
		return nil, status.Error(codes.Aborted, msg)
	}
	defer s.inFlight.Delete(volumeID)

	capRange := preq.GetCapacityRange()
	if capRange == nil {
		klog.Errorf("ControllerExpandVolume: Capacity range is required")
		return nil, status.Error(codes.InvalidArgument, "Capacity range is required")
	}

	volSizeBytes := preq.GetCapacityRange().GetRequiredBytes()
	volSizeGB := uint64(lsutil.RoundUpSize(volSizeBytes, 1024*1024*1024))
	maxVolSize := capRange.GetLimitBytes()

	if maxVolSize > 0 && volSizeBytes > maxVolSize {
		klog.Errorf("ControllerExpandVolume: Requested size %d exceeds limit %d", volSizeBytes, maxVolSize)
		return nil, status.Error(codes.OutOfRange, lfmt.Sprintf("Requested size %d exceeds limit %d", volSizeBytes, maxVolSize))
	}

	volume, err := s.cloud.GetVolume(volumeID)
	if err != nil {
		klog.ErrorS(err.Error, "ControllerExpandVolume: failed to get volume", "volumeID", volumeID)
		return nil, status.Error(codes.Internal, lfmt.Sprintf("failed to get volume; ERR: %v", err))
	}

	if volume == nil {
		klog.Errorf("ControllerExpandVolume: volume %s not found", volumeID)
		return nil, status.Error(codes.NotFound, lfmt.Sprintf("volume %s not found", volumeID))
	}

	if volume.Size >= volSizeGB {
		klog.V(2).Infof("ControllerExpandVolume; volume %s already has size %d GiB", volumeID, volume.Size)
		return &lcsi.ControllerExpandVolumeResponse{
			CapacityBytes:         int64(volume.Size * 1024 * 1024 * 1024),
			NodeExpansionRequired: true,
		}, nil
	}

	klog.V(5).InfoS("ControllerExpandVolume: expanding volume", "volumeID", volumeID, "newSize", volSizeGB)
	// Expand the volume
	err1 := s.cloud.ExpandVolume(volume.VolumeTypeID, volumeID, volSizeGB)
	if err1 != nil {
		klog.ErrorS(err1, "ControllerExpandVolume: failed to expand volume", "volumeID", volumeID)
		return nil, status.Error(codes.Internal, "failed to expand volume")
	}

	klog.V(4).InfoS("ControllerExpandVolume: volume expanded successfully", "volumeID", volumeID, "newSize", volSizeGB)
	return &lcsi.ControllerExpandVolumeResponse{
		CapacityBytes:         volSizeBytes,
		NodeExpansionRequired: true,
	}, nil
}

func (s *controllerService) ControllerGetVolume(ctx lctx.Context, req *lcsi.ControllerGetVolumeRequest) (*lcsi.ControllerGetVolumeResponse, error) {
	klog.V(4).InfoS("ControllerGetVolume: called", "args", *req)
	return nil, status.Error(codes.Unimplemented, "")
}

func (s *controllerService) ControllerModifyVolume(ctx lctx.Context, req *lcsi.ControllerModifyVolumeRequest) (*lcsi.ControllerModifyVolumeResponse, error) {
	klog.V(4).InfoS("ControllerModifyVolume: called", "args", *req)

	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID not provided")
	}

	options, err := parseModifyVolumeParameters(req.GetMutableParameters())
	if err != nil {
		return nil, err
	}

	err = s.modifyVolumeWithCoalescing(ctx, volumeID, options)
	if err != nil {
		return nil, err
	}

	return &lcsi.ControllerModifyVolumeResponse{}, nil
}

func (s *controllerService) GetCSIDriverModificationCapability(_ lctx.Context, _ *lvmrpc.GetCSIDriverModificationCapabilityRequest) (*lvmrpc.GetCSIDriverModificationCapabilityResponse, error) {
	return &lvmrpc.GetCSIDriverModificationCapabilityResponse{}, nil
}

func (s *controllerService) ModifyVolumeProperties(pctx lctx.Context, preq *lvmrpc.ModifyVolumePropertiesRequest) (*lvmrpc.ModifyVolumePropertiesResponse, error) {
	klog.V(4).InfoS("ModifyVolumeProperties called", "preq", preq)

	if err := validateModifyVolumePropertiesRequest(preq); err != nil {
		return nil, err
	}

	options, err := parseModifyVolumeParameters(preq.GetParameters())
	if err != nil {
		return nil, err
	}

	volumeID := preq.GetName()

	// check if a request is already in-flight
	if ok := s.inFlight.Insert(volumeID); !ok {
		msg := lfmt.Sprintf(lsinternal.VolumeOperationAlreadyExistsErrorMsg, volumeID)
		return nil, status.Error(codes.Aborted, msg)
	}
	defer s.inFlight.Delete(volumeID)
	volume, err1 := s.cloud.GetVolume(volumeID)
	if err1 != nil {
		return nil, status.Error(codes.Internal, lfmt.Sprintf("failed to get volume; ERR: %v", err1))
	}

	if volume.VolumeTypeID == options.VolumeType {
		klog.V(2).Infof("ModifyVolumeProperties; volume %s already has volume type %s", volumeID, options.VolumeType)
		return &lvmrpc.ModifyVolumePropertiesResponse{}, nil
	}

	err = s.cloud.ExpandVolume(volumeID, options.VolumeType, volume.Size)
	if err != nil {
		return nil, status.Error(codes.Internal, lfmt.Sprintf("failed to modify volume; ERR: %v", err))
	}

	return &lvmrpc.ModifyVolumePropertiesResponse{}, nil
}

func (s *controllerService) getClusterID() string {
	return s.driverOptions.clusterID
}

func newCreateVolumeResponse(disk *lsdkObj.Volume, prespCtx map[string]string) *lcsi.CreateVolumeResponse {
	//var src *lcsi.VolumeContentSource

	return &lcsi.CreateVolumeResponse{
		Volume: &lcsi.Volume{
			VolumeId:      disk.VolumeId,
			CapacityBytes: int64(disk.Size * 1024 * 1024 * 1024),
			VolumeContext: prespCtx,
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
			CreationTime:   timestamppb.New(creationTime),
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
			CreationTime:   timestamppb.New(creationTime),
			ReadyToUse:     snapshot.Status == lscloud.SnapshotActiveStatus,
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
