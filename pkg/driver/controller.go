package driver

import (
	lctx "context"
	"fmt"
	lcsi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/vngcloud/vngcloud-csi-volume-modifier/pkg/rpc"
	lsdkObj "github.com/vngcloud/vngcloud-go-sdk/vngcloud/objects"
	lvolV2 "github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/blockstorage/v2/volume"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"

	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/cloud"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/driver/internal"
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
	volSizeBytes, err := getVolSizeBytes(preq)
	if err != nil {
		klog.Errorf("CreateVolume: invalid request: %v", err)
		return nil, err
	}

	volName := preq.GetName()              // get the name of the volume, always in the format of pvc-<random-uuid>
	volCap := preq.GetVolumeCapabilities() // get volume capabilities
	multiAttach := isMultiAttach(volCap)   // check if the volume is multi-attach, true if multi-attach, false otherwise

	// check if a request is already in-flight
	if ok := s.inFlight.Insert(volName); !ok {
		msg := fmt.Sprintf(volumeCreatingInProgress, volName)
		return nil, status.Error(codes.Aborted, msg)
	}
	defer s.inFlight.Delete(volName)

	_, err = parseModifyVolumeParameters(preq.GetMutableParameters())
	if err != nil {
		klog.Errorf("CreateVolume: invalid request: %v", err)
		return nil, ErrModifyMutableParam
	}

	reqOpts := new(lvolV2.CreateOpts)
	reqOpts.Name = volName
	reqOpts.MultiAttach = multiAttach
	reqOpts.Size = uint64(volSizeBytes)
	reqOpts.VolumeTypeId = preq.GetParameters()["type"]
	reqOpts.IsPoc = false

	resp, err := s.cloud.CreateVolume(reqOpts)
	if err != nil {
		klog.Errorf("CreateVolume: failed to create volume: %v", err)
		return nil, err
	}

	return newCreateVolumeResponse(resp), nil
}

func (s *controllerService) DeleteVolume(ctx lctx.Context, req *lcsi.DeleteVolumeRequest) (*lcsi.DeleteVolumeResponse, error) {
	return nil, nil
}
func (s *controllerService) ControllerPublishVolume(ctx lctx.Context, req *lcsi.ControllerPublishVolumeRequest) (result *lcsi.ControllerPublishVolumeResponse, err error) {
	return nil, nil
}

func (s *controllerService) ControllerUnpublishVolume(ctx lctx.Context, req *lcsi.ControllerUnpublishVolumeRequest) (*lcsi.ControllerUnpublishVolumeResponse, error) {
	return nil, nil
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

func (s *controllerService) ValidateVolumeCapabilities(ctx lctx.Context, req *lcsi.ValidateVolumeCapabilitiesRequest) (*lcsi.ValidateVolumeCapabilitiesResponse, error) {
	return nil, status.Error(codes.Unimplemented, "ValidateVolumeCapabilities is not yet implemented")
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
	klog.V(4).Infof("ControllerGetVolume; called with request %+v", req)

	volumeID := req.GetVolumeId()
	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "Volume ID is required")
	}

	volume, err := s.cloud.GetVolume(volumeID)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get volume; ERR: %v", err))
	}

	volEntry := lcsi.ControllerGetVolumeResponse{
		Volume: &lcsi.Volume{
			VolumeId:      volume.VolumeId,
			CapacityBytes: int64(volume.Size * (1024 ^ 3))}}

	csiStatus := &lcsi.ControllerGetVolumeResponse_VolumeStatus{}
	if isAttachment(volume.VmId) {
		csiStatus.PublishedNodeIds = []string{*volume.VmId}
	}

	volEntry.Status = csiStatus

	return &volEntry, nil
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

	ModificationVolumeSize = "volumeSize"
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
