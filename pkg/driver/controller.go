package driver

import (
	lctx "context"
	"fmt"
	lstr "strings"

	lcsi "github.com/container-storage-interface/spec/lib/go/csi"
	ljoat "github.com/cuongpiger/joat/parser"
	"github.com/vngcloud/vngcloud-csi-volume-modifier/pkg/rpc"
	lsdkEH "github.com/vngcloud/vngcloud-go-sdk/vngcloud/errors"
	lsdkObj "github.com/vngcloud/vngcloud-go-sdk/vngcloud/objects"
	lvolV2 "github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/blockstorage/v2/volume"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"

	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/cloud"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/driver/internal"
	lsutil "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/util"
)

var (
	// NewMetadataFunc is a variable for the cloud.NewMetadata function that can
	// be overwritten in unit tests.
	NewMetadataFunc = cloud.NewMetadataService
	NewCloudFunc    = cloud.NewCloud
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
	cloud               cloud.Cloud
	inFlight            *internal.InFlight
	modifyVolumeManager *modifyVolumeManager
	driverOptions       *DriverOptions

	rpc.UnimplementedModifyServer
}

// newControllerService creates a new controller service it panics if failed to create the service
func newControllerService(driverOptions *DriverOptions) controllerService {
	metadata, err := NewMetadataFunc(cloud.DefaultVServerMetadataClient)
	if err != nil {
		klog.ErrorS(err, "Could not determine the metadata information for the driver")
		panic(err)
	}

	klog.Infof("The driver options are: %v", driverOptions)
	cloudSrv, err := NewCloudFunc(driverOptions.identityURL, driverOptions.vServerURL, driverOptions.clientID, driverOptions.clientSecret, metadata)
	if err != nil {
		panic(err)
	}

	return controllerService{
		cloud:               cloudSrv,
		inFlight:            internal.NewInFlight(),
		driverOptions:       driverOptions,
		modifyVolumeManager: newModifyVolumeManager(),
	}
}

func (s *controllerService) CreateVolume(pctx lctx.Context, preq *lcsi.CreateVolumeRequest) (*lcsi.CreateVolumeResponse, error) {
	klog.V(5).InfoS("CreateVolume: called", "preq", *preq)

	// Validate the create volume request
	if err := validateCreateVolumeRequest(preq); err != nil {
		klog.Errorf("CreateVolume: invalid request: %v", err)
		return nil, err
	}

	// Validate volume size, if volume size is less than the default volume size of cloud provider, set it to the default volume size
	volSizeBytes := getVolSizeBytes(preq)
	volName := preq.GetName()              // get the name of the volume, always in the format of pvc-<random-uuid>
	volCap := preq.GetVolumeCapabilities() // get volume capabilities
	multiAttach := isMultiAttach(volCap)   // check if the volume is multi-attach, true if multi-attach, false otherwise

	// check if a request is already in-flight
	if ok := s.inFlight.Insert(volName); !ok {
		msg := fmt.Sprintf(volumeCreatingInProgress, volName)
		return nil, status.Error(codes.Aborted, msg)
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
				return nil, status.Errorf(codes.InvalidArgument, "Could not parse blockSize (%s)", pv)
			}
			cvr = cvr.WithBlockSize(pv)
		case InodeSizeKey:
			if isAlphanumeric := parser.StringIsAlphanumeric(pv); !isAlphanumeric {
				return nil, status.Errorf(codes.InvalidArgument, "Could not parse inodeSize (%s)", pv)
			}
			cvr = cvr.WithInodeSize(pv)
		case BytesPerInodeKey:
			if isAlphanumeric := parser.StringIsAlphanumeric(pv); !isAlphanumeric {
				return nil, status.Errorf(codes.InvalidArgument, "Could not parse bytesPerInode (%s)", pv)
			}
			cvr = cvr.WithBytesPerInode(pv)
		case NumberOfInodesKey:
			if isAlphanumeric := parser.StringIsAlphanumeric(pv); !isAlphanumeric {
				return nil, status.Errorf(codes.InvalidArgument, "Could not parse numberOfInodes (%s)", pv)
			}
			cvr = cvr.WithNumberOfInodes(pv)
		case Ext4ClusterSizeKey:
			if isAlphanumeric := parser.StringIsAlphanumeric(pv); !isAlphanumeric {
				return nil, status.Errorf(codes.InvalidArgument, "Could not parse ext4ClusterSize (%s)", pv)
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
		klog.Errorf("CreateVolume: invalid request: %v", err)
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
		klog.Errorf("CreateVolume: failed to parse response context: %v", err)
		return nil, err
	}

	cvr = cvr.WithVolumeName(volName).
		WithMultiAttach(multiAttach).
		WithVolumeSize(uint64(lsutil.RoundUpSize(volSizeBytes, 1024*1024*1024))).
		WithVolumeTypeID(modifyOpts.VolumeType).
		WithClusterID(s.driverOptions.clusterID)

	resp, err := s.cloud.CreateVolume(cvr.ToSdkCreateVolumeOpts(s.driverOptions))
	if err != nil {
		klog.Errorf("CreateVolume: failed to create volume: %v", err)
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
		msg := fmt.Sprintf(internal.VolumeOperationAlreadyExistsErrorMsg, volumeID)
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
		klog.Errorf("ControllerPublishVolume: invalid request: %v", err)
		return nil, err
	}

	volumeID := preq.GetVolumeId() // get the cloud volume ID
	nodeID := preq.GetNodeId()     // get the cloud node ID

	// Make sure there are no 2 operations on the same volume and node at the same time
	if !s.inFlight.Insert(volumeID + nodeID) {
		return nil, status.Error(codes.Aborted, fmt.Sprintf(internal.VolumeOperationAlreadyExistsErrorMsg, volumeID))
	}
	defer s.inFlight.Delete(volumeID + nodeID)

	klog.V(2).InfoS("ControllerPublishVolume: attaching volume into the instance", "volumeID", volumeID, "nodeID", nodeID)

	// Attach the volume and wait for it to be attached
	_, err = s.cloud.AttachVolume(nodeID, volumeID)
	if err != nil {
		klog.Errorf("ControllerPublishVolume; failed to attach volume %s to instance %s; ERR: %v", volumeID, nodeID, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to attach volume; ERR: %v", err))
	}

	devicePath, err := s.cloud.GetDeviceDiskID(volumeID)
	if err != nil {
		klog.ErrorS(err, "ControllerPublishVolume; failed to get device path for volume %s", volumeID)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get device path for volume; ERR: %v", err))
	}

	klog.V(5).InfoS("ControllerPublishVolume; volume attached to instance successfully", "volumeID", volumeID, "nodeID", nodeID)
	return newControllerPublishVolumeResponse(devicePath), nil
}

func (s *controllerService) ControllerUnpublishVolume(ctx lctx.Context, req *lcsi.ControllerUnpublishVolumeRequest) (*lcsi.ControllerUnpublishVolumeResponse, error) {
	klog.V(4).InfoS("ControllerUnpublishVolume: called", "args", *req)

	if err := validateControllerUnpublishVolumeRequest(req); err != nil {
		return nil, err
	}

	volumeID := req.GetVolumeId()
	nodeID := req.GetNodeId()

	if !s.inFlight.Insert(volumeID + nodeID) {
		return nil, status.Error(codes.Aborted, fmt.Sprintf(internal.VolumeOperationAlreadyExistsErrorMsg, volumeID))
	}
	defer s.inFlight.Delete(volumeID + nodeID)
	if volumeID == "" {
		klog.Errorf("ControllerUnpublishVolume; Volume ID is required")
		return nil, status.Error(codes.InvalidArgument, "Volume ID is required")
	}

	_, getErr := s.cloud.GetVolume(volumeID)
	if getErr != nil && getErr.Code == lsdkEH.ErrCodeVolumeNotFound {
		klog.InfoS("ControllerUnpublishVolume; volume not found", "volumeID", volumeID)
		return &lcsi.ControllerUnpublishVolumeResponse{}, nil
	}

	err := s.cloud.DetachVolume(nodeID, volumeID)
	if err != nil {
		klog.Errorf("ControllerUnpublishVolume; failed to detach volume %s from instance %s; ERR: %v", volumeID, nodeID, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to detach volume; ERR: %v", err))
	}

	klog.V(4).Infof("ControllerUnpublishVolume; volume %s detached from instance %s successfully", volumeID, nodeID)
	return &lcsi.ControllerUnpublishVolumeResponse{}, nil
}

func (s *controllerService) CreateSnapshot(ctx lctx.Context, req *lcsi.CreateSnapshotRequest) (*lcsi.CreateSnapshotResponse, error) {
	return nil, status.Error(codes.Unimplemented, "CreateSnapshot is not yet implemented")
}

func (s *controllerService) DeleteSnapshot(ctx lctx.Context, req *lcsi.DeleteSnapshotRequest) (*lcsi.DeleteSnapshotResponse, error) {
	return nil, status.Error(codes.Unimplemented, "DeleteSnapshot is not yet implemented")
}

func (s *controllerService) ListSnapshots(ctx lctx.Context, req *lcsi.ListSnapshotsRequest) (*lcsi.ListSnapshotsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "ListSnapshots is not yet implemented")
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
		if err.Code == lvolV2.ErrVolumeNotFound {
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

func (s *controllerService) ControllerExpandVolume(ctx lctx.Context, req *lcsi.ControllerExpandVolumeRequest) (*lcsi.ControllerExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "ControllerExpandVolume is not yet implemented")
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

func (s *controllerService) GetCSIDriverModificationCapability(
	_ lctx.Context,
	_ *rpc.GetCSIDriverModificationCapabilityRequest,
) (*rpc.GetCSIDriverModificationCapabilityResponse, error) {
	return &rpc.GetCSIDriverModificationCapabilityResponse{}, nil
}

func (s *controllerService) ModifyVolumeProperties(ctx lctx.Context, req *rpc.ModifyVolumePropertiesRequest) (*rpc.ModifyVolumePropertiesResponse, error) {
	klog.V(4).InfoS("ModifyVolumeProperties called", "req", req)
	if err := validateModifyVolumePropertiesRequest(req); err != nil {
		return nil, err
	}

	options, err := parseModifyVolumeParameters(req.GetParameters())
	if err != nil {
		return nil, err
	}

	name := req.GetName()
	err = s.modifyVolumeWithCoalescing(ctx, name, options)
	if err != nil {
		return nil, err
	}

	return &rpc.ModifyVolumePropertiesResponse{}, nil
}

func isAttachment(vmId *string) bool {
	if vmId == nil {
		return false
	}
	return true
}

const (
	ModificationKeyVolumeType = "volumeType"
)

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
