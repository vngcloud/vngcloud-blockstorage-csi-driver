package driver

import (
	lctx "context"
	"fmt"
	lstr "strings"

	lcsi "github.com/container-storage-interface/spec/lib/go/csi"
	ljoat "github.com/cuongpiger/joat/parser"
	"github.com/vngcloud/vngcloud-csi-volume-modifier/pkg/rpc"
	lsdkObj "github.com/vngcloud/vngcloud-go-sdk/vngcloud/objects"
	lvolV2 "github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/blockstorage/v2/volume"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"

	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/cloud"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/driver/internal"
	ldto "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/dto"
	lsutil "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/util"
)

var (
	// NewMetadataFunc is a variable for the cloud.NewMetadata function that can
	// be overwritten in unit tests.
	NewMetadataFunc = cloud.NewMetadataService
	NewCloudFunc    = cloud.NewCloud
)

// Supported access modes
const (
	vContainerCSIClusterIDKey = DriverName + "/cluster"
	SingleNodeWriter          = lcsi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	MultiNodeMultiWriter      = lcsi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER
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

// newControllerService creates a new controller service
// it panics if failed to create the service
func newControllerService(driverOptions *DriverOptions) controllerService {
	metadata, err := NewMetadataFunc(cloud.DefaultVServerMetadataClient)
	if err != nil {
		klog.ErrorS(err, "Could not determine region from any metadata service. The region can be manually supplied via the AWS_REGION environment variable.")
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

	// Validate volume size, if volume size is less than the default volume size of cloud provider,
	// set it to the default volume size
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

	cvr := ldto.NewCreateVolumeRequest()
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

	cvr = cvr.WithVolumeName(volName).
		WithMultiAttach(multiAttach).
		WithVolumeSize(uint64(lsutil.RoundUpSize(volSizeBytes, 1024*1024*1024))).
		WithVolumeTypeID(modifyOpts.VolumeType)

	reqOpts := new(lvolV2.CreateOpts)
	reqOpts.IsPoc = false
	reqOpts.CreatedFrom = cloud.CreateFromNew

	resp, err := s.cloud.CreateVolume(reqOpts)
	if err != nil {
		klog.Errorf("CreateVolume: failed to create volume: %v", err)
		return nil, err
	}

	return newCreateVolumeResponse(resp), nil
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
	klog.V(4).InfoS("ControllerPublishVolume: called", "args", *preq)
	if err = validateControllerPublishVolumeRequest(preq); err != nil {
		klog.Errorf("ControllerPublishVolume: invalid request: %v", err)
		return nil, err
	}

	volumeID := preq.GetVolumeId()
	nodeID := preq.GetNodeId()

	vol, err1 := s.cloud.GetVolume(volumeID)
	if err1 != nil {
		klog.Errorf("ControllerPublishVolume; failed to get volume %s; ERR: %v", volumeID, err1.Error)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get volume; ERR: %v", err1.Error))
	}

	if !s.inFlight.Insert(volumeID + nodeID) {
		return nil, status.Error(codes.Aborted, fmt.Sprintf(internal.VolumeOperationAlreadyExistsErrorMsg, volumeID))
	}
	defer s.inFlight.Delete(volumeID + nodeID)

	klog.V(2).InfoS("ControllerPublishVolume: attaching", "volumeID", volumeID, "nodeID", nodeID)
	_, err = s.cloud.AttachVolume(nodeID, volumeID)
	if err != nil {
		if vol != nil && vol.Status == cloud.VolumeInUseStatus {
			klog.V(4).Infof("ControllerPublishVolume; volume %s attached to instance %s successfully", volumeID, nodeID)
			return &lcsi.ControllerPublishVolumeResponse{}, nil
		}

		klog.Errorf("ControllerPublishVolume; failed to attach volume %s to instance %s; ERR: %v", volumeID, nodeID, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to attach volume; ERR: %v", err))
	}

	err = s.cloud.WaitDiskAttached(nodeID, volumeID)
	if err != nil {
		klog.Errorf("ControllerPublishVolume; failed to wait disk attached to instance %s; ERR: %v", nodeID, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to wait disk attached; ERR: %v", err))
	}

	_, err = s.cloud.GetAttachmentDiskPath(nodeID, volumeID)
	if err != nil {
		klog.Errorf("ControllerPublishVolume; failed to get device path for volume %s; ERR: %v", volumeID, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get device path; ERR: %v", err))
	}

	klog.V(4).Infof("ControllerPublishVolume; volume %s attached to instance %s successfully", volumeID, nodeID)
	return &lcsi.ControllerPublishVolumeResponse{}, nil
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

	_ = s.cloud.DetachVolume(nodeID, volumeID)

	if err := s.cloud.WaitDiskDetached(nodeID, volumeID); err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to wait disk detached; ERR: %v", err))
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

func (s *controllerService) ModifyVolumeProperties(
	ctx lctx.Context,
	req *rpc.ModifyVolumePropertiesRequest,
) (*rpc.ModifyVolumePropertiesResponse, error) {
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

func validateModifyVolumePropertiesRequest(req *rpc.ModifyVolumePropertiesRequest) error {
	name := req.GetName()
	if name == "" {
		return status.Error(codes.InvalidArgument, "Volume name not provided")
	}
	return nil
}

const (
	ModificationKeyVolumeType = "volumeType"
)

func newCreateVolumeResponse(disk *lsdkObj.Volume) *lcsi.CreateVolumeResponse {
	//var src *lcsi.VolumeContentSource

	return &lcsi.CreateVolumeResponse{
		Volume: &lcsi.Volume{
			VolumeId:      disk.VolumeId,
			CapacityBytes: int64(disk.Size * 1024 * 1024 * 1024),
		},
	}
}
