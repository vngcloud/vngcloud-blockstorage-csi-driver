package driver

import (
	"fmt"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/csi/utils/blockdevice"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/csi/utils/metadata"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/csi/utils/mount"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/csi/vcontainer/vcontainer"
	obj "github.com/vngcloud/vngcloud-go-sdk/vngcloud/objects"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
	mountutil "k8s.io/mount-utils"
	utilpath "k8s.io/utils/path"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type nodeServer struct {
	Driver   *Driver
	Mount    mount.IMount
	Metadata metadata.IMetadata
	Cloud    vcontainer.IVContainer
}

func (s *nodeServer) NodeGetInfo(_ context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	klog.V(5).Infof("NodeGetInfo; called with req: %#v", req)
	nodeUUID, err := s.Metadata.GetInstanceID()
	if err != nil {
		return nil, err
	}

	zone, err := s.Metadata.GetAvailabilityZone()
	if err != nil {
		return nil, err
	}

	return &csi.NodeGetInfoResponse{
		NodeId: nodeUUID,
		AccessibleTopology: &csi.Topology{
			Segments: map[string]string{
				topologyKey: zone}},
		MaxVolumesPerNode: s.Cloud.GetMaxVolLimit(),
	}, nil
}

func (s *nodeServer) NodeGetCapabilities(_ context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	klog.V(5).Infof("NodeGetCapabilities; called with req: %#v", req)

	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: s.Driver.nscap,
	}, nil
}

func (s *nodeServer) NodeGetVolumeStats(_ context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	klog.V(5).Infof("NodeGetVolumeStats; called with req: %#v", req)

	volumeID := req.GetVolumeId()
	if volumeID == "" {
		klog.Errorf("NodeGetVolumeStats; volume ID is required")
		return nil, status.Error(codes.InvalidArgument, "volume ID is required")
	}

	volumePath := req.GetVolumePath()
	if volumePath == "" {
		klog.Errorf("NodeGetVolumeStats; volume path is required")
		return nil, status.Error(codes.InvalidArgument, "volume path is required")
	}

	exists, err := utilpath.Exists(utilpath.CheckFollowSymlink, volumePath)
	if err != nil {
		klog.Errorf("NodeGetVolumeStats; failed to check if volume path exists: %v", err)
		return nil, status.Error(codes.Internal, err.Error())
	}

	if !exists {
		klog.Errorf("NodeGetVolumeStats; volume path %s does not exist", volumePath)
		return nil, status.Error(codes.NotFound, "volume path does not exist")
	}

	stats, err := s.Mount.GetDeviceStats(volumePath)
	if err != nil {
		klog.Errorf("NodeGetVolumeStats; failed to get device stats: %v", err)
		return nil, status.Error(codes.Internal, err.Error())
	}

	if stats.Block {
		return &csi.NodeGetVolumeStatsResponse{
			Usage: []*csi.VolumeUsage{
				{
					Total: stats.TotalBytes,
					Unit:  csi.VolumeUsage_BYTES,
				},
			},
		}, nil
	}

	return &csi.NodeGetVolumeStatsResponse{
		Usage: []*csi.VolumeUsage{
			{Total: stats.TotalBytes, Available: stats.AvailableBytes, Used: stats.UsedBytes, Unit: csi.VolumeUsage_BYTES},
			{Total: stats.TotalInodes, Available: stats.AvailableInodes, Used: stats.UsedInodes, Unit: csi.VolumeUsage_INODES},
		},
	}, nil
}

func (s *nodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	klog.V(5).Infof("NodePublishVolume; called with req: %#v", req)

	volumeID := req.GetVolumeId()
	source := req.GetStagingTargetPath()
	targetPath := req.GetTargetPath()
	volumeCapability := req.GetVolumeCapability()

	if volumeID == "" {
		klog.Errorf("NodePublishVolume; volume ID is required")
		return nil, status.Error(codes.InvalidArgument, "volume ID is required")
	}

	if targetPath == "" {
		klog.Errorf("NodePublishVolume; target path is required")
		return nil, status.Error(codes.InvalidArgument, "target path is required")
	}

	if volumeCapability == nil {
		klog.Errorf("NodePublishVolume; volume capability is required")
		return nil, status.Error(codes.InvalidArgument, "volume capability is required")
	}

	ephemeralVolume := req.GetVolumeContext()["csi.storage.k8s.io/ephemeral"] == "true"
	if ephemeralVolume {
		klog.Warningf("NodePublishVolume; ephemeral volumes is deprecated inm 1.24 release")
		return s.nodePublishEphemeral(req)
	}

	if source == "" {
		klog.Errorf("NodePublishVolume; source path is required")
		return nil, status.Error(codes.InvalidArgument, "source path is required")
	}

	_, err := s.Cloud.GetVolume(volumeID)
	if err != nil {
		klog.Errorf("NodePublishVolume; failed to get volume %s; ERR: %v", volumeID, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get volume %s", volumeID))
	}

	mountOpts := []string{"bind"}
	if req.GetReadonly() {
		mountOpts = append(mountOpts, "ro")
	} else {
		mountOpts = append(mountOpts, "rw")
	}

	if blk := volumeCapability.GetBlock(); blk != nil {
		return s.nodePublishVolumeForBlock(req, mountOpts)
	}

	notMnt, err := s.Mount.IsLikelyNotMountPointAttach(targetPath)
	if err != nil {
		klog.Errorf("NodePublishVolume; failed to check if target path %s is a mount point; ERR: %v", targetPath, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to check if target path %s is a mount point", targetPath))
	}

	if notMnt {
		fsType_ := "ext4"
		if mnt := volumeCapability.GetMount(); mnt != nil {
			if mnt.FsType != "" {
				fsType_ = mnt.FsType
			}
		}

		err = s.Mount.Mounter().Mount(source, targetPath, fsType_, mountOpts)
		if err != nil {
			klog.Errorf("NodePublishVolume; failed to mount volume %s to target path %s; ERR: %v", volumeID, targetPath, err)
			return nil, status.Error(codes.Internal, fmt.Sprintf("failed to mount volume %s to target path %s", volumeID, targetPath))
		}
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

func (s *nodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	klog.V(4).Infof("NodeUnpublishVolume; called with req: %#v", req)

	volumeID := req.GetVolumeId()
	targetPath := req.GetTargetPath()

	if targetPath == "" {
		klog.Errorf("NodeUnpublishVolume; target path is required")
		return nil, status.Error(codes.InvalidArgument, "target path is required")
	}

	if volumeID == "" {
		klog.Errorf("NodeUnpublishVolume; volume ID is required")
		return nil, status.Error(codes.InvalidArgument, "volume ID is required")
	}

	ephemeralVolume := false

	vol, err := s.Cloud.GetVolume(volumeID)
	if err != nil {
		klog.Errorf("NodeUnpublishVolume; failed to get volume %s; ERR: %v", volumeID, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get volume %s", volumeID))
	}

	if vol == nil {
		klog.Errorf("NodeUnpublishVolume; failed to get volume %s; ERR: %v", volumeID, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get volume %s", volumeID))
	}

	if strings.HasPrefix(vol.Name, "ephemeral-") {
		ephemeralVolume = true
	}

	err = s.Mount.UnmountPath(targetPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Unmount of targetpath %s failed with error %v", targetPath, err)
	}

	if ephemeralVolume {
		return s.nodeUnpublishEphemeral(req, vol)
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (s *nodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	klog.V(5).Infof("NodeStageVolume; called with req: %#v", req)

	stagingTarget := req.GetStagingTargetPath()
	volumeCapability := req.GetVolumeCapability()
	volumeID := req.GetVolumeId()

	if volumeID == "" {
		klog.Errorf("NodeStageVolume; volume ID is required")
		return nil, status.Error(codes.InvalidArgument, "volume ID is required")
	}

	if stagingTarget == "" {
		klog.Errorf("NodeStageVolume; staging target path is required")
		return nil, status.Error(codes.InvalidArgument, "staging target path is required")
	}

	if volumeCapability == nil {
		klog.Errorf("NodeStageVolume; volume capability is required")
		return nil, status.Error(codes.InvalidArgument, "volume capability is required")
	}

	vol, err := s.Cloud.GetVolume(volumeID)
	if err != nil {
		klog.Errorf("NodeStageVolume; failed to get volume %s; ERR: %v", volumeID, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get volume %s", volumeID))
	}

	if vol == nil {
		klog.Errorf("NodeStageVolume; failed to get volume %s; ERR: %v", volumeID, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get volume %s", volumeID))
	}

	mappingID, err := s.Cloud.GetMappingVolume(volumeID)
	if err != nil {
		klog.Errorf("NodeStageVolume; failed to get mapping volume %s; ERR: %v", volumeID, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get mapping volume %s", volumeID))
	}

	devicePath, err := getDevicePath(mappingID, s.Mount)
	if err != nil {
		klog.Errorf("NodeStageVolume; failed to get device path for volume %s; ERR: %v", volumeID, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get device path for volume %s", volumeID))
	}

	if blk := volumeCapability.GetBlock(); blk != nil {
		return &csi.NodeStageVolumeResponse{}, nil
	}

	notMnt, err := s.Mount.IsLikelyNotMountPointAttach(stagingTarget)
	if err != nil {
		klog.Errorf("NodeStageVolume; failed to check if staging target path %s is a mount point; ERR: %v", stagingTarget, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to check if staging target path %s is a mount point", stagingTarget))
	}

	if notMnt {
		fsType_ := "ext4"
		var options []string
		if mnt := volumeCapability.GetMount(); mnt != nil {
			if mnt.FsType != "" {
				fsType_ = mnt.FsType
			}

			mountFlags := mnt.GetMountFlags()
			options = append(options, collectMountOptions(fsType_, mountFlags)...)
		}

		// mount
		err := s.Mount.Mounter().FormatAndMount(devicePath, stagingTarget, fsType_, options)
		if err != nil {
			klog.Errorf("NodeStageVolume; failed to mount volume %s to staging target path %s; ERR: %v", volumeID, stagingTarget, err)
			return nil, status.Error(codes.Internal, fmt.Sprintf("failed to mount volume %s to staging target path %s", volumeID, stagingTarget))
		}
	}

	return &csi.NodeStageVolumeResponse{}, nil
}

func (s *nodeServer) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	klog.V(5).Infof("NodeUnstageVolume; called with req: %#v", req)

	volumeID := req.GetVolumeId()
	if volumeID == "" {
		klog.Errorf("NodeUnstageVolume; volume ID is required")
		return nil, status.Error(codes.InvalidArgument, "volume ID is required")
	}

	stagingTarget := req.GetStagingTargetPath()
	if stagingTarget == "" {
		klog.Errorf("NodeUnstageVolume; staging target path is required")
		return nil, status.Error(codes.InvalidArgument, "staging target path is required")
	}

	vol, err := s.Cloud.GetVolume(volumeID)
	if err != nil {
		klog.Errorf("NodeUnstageVolume; failed to get volume %s; ERR: %v", volumeID, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get volume %s", volumeID))
	}

	if vol == nil {
		klog.Errorf("NodeUnstageVolume; failed to get volume %s; ERR: %v", volumeID, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get volume %s", volumeID))
	}

	err = s.Mount.UnmountPath(stagingTarget)
	if err != nil {
		klog.Errorf("NodeUnstageVolume; failed to unmount staging target path %s; ERR: %v", stagingTarget, err)
		return nil, status.Errorf(codes.Internal, "Unmount of staging targetpath %s failed with error %v", stagingTarget, err)
	}

	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (s *nodeServer) NodeExpandVolume(ctx context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	klog.V(4).Infof("NodeExpandVolume; called with req: %#v", req)

	volumeID := req.GetVolumeId()
	if volumeID == "" {
		klog.Errorf("NodeExpandVolume; volume ID is required")
		return nil, status.Error(codes.InvalidArgument, "volume ID is required")
	}

	volumePath := req.GetVolumePath()
	if volumePath == "" {
		klog.Errorf("NodeExpandVolume; volume path is required")
		return nil, status.Error(codes.InvalidArgument, "volume path is required")
	}

	output, err := s.Mount.GetMountFs(volumePath)
	if err != nil {
		klog.Errorf("NodeExpandVolume; failed to get mount fs: %v", err)
		return nil, status.Error(codes.Internal, err.Error())
	}

	devicePath := strings.TrimSpace(string(output))
	if devicePath == "" {
		klog.Errorf("NodeExpandVolume; failed to get device path for volume %s", volumeID)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get device path for volume %s", volumeID))
	}

	if s.Cloud.GetBlockStorageOpts().RescanOnResize {
		newSize := req.GetCapacityRange().GetRequiredBytes()
		if err := blockdevice.RescanBlockDeviceGeometry(devicePath, volumePath, newSize); err != nil {
			klog.Errorf("NodeExpandVolume; failed to rescan block device geometry: %v", err)
			return nil, status.Error(codes.Internal, err.Error())
		}
	}

	r := mountutil.NewResizeFs(s.Mount.Mounter().Exec)
	if _, err := r.Resize(devicePath, volumePath); err != nil {
		klog.Errorf("NodeExpandVolume; failed to resize filesystem: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to resize filesystem for volume %s: %v", volumeID, err)
	}

	return &csi.NodeExpandVolumeResponse{}, nil
}

func (s *nodeServer) nodePublishEphemeral(req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	var size int = 1
	var err error

	volID := req.GetVolumeId()
	volName := fmt.Sprintf("ephemeral-%s", volID)
	capacity, ok := req.GetVolumeContext()["capacity"]
	volAvailability, err := s.Metadata.GetAvailabilityZone()
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("retrieving availability zone from MetaData service failed with error %v", err))
	}

	if ok && strings.HasSuffix(capacity, "Gi") {
		size, err = strconv.Atoi(strings.TrimSuffix(capacity, "Gi"))
		if err != nil {
			klog.Errorf("NodePublishVolume; unable to parse capacity %s; ERR: %v", capacity, err)
			return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("unable to parse capacity %s", capacity))
		}
	}

	volumeType, ok := req.GetVolumeContext()["type"]
	if !ok {
		volumeType = ""
	}

	evol, err := s.Cloud.CreateVolume(volName, uint64(size), volumeType, volAvailability, "", "", nil)
	if err != nil {
		klog.Errorf("NodePublishVolume; failed to create volume %s; ERR: %v", volName, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to create volume %s", volName))
	}

	if evol == nil {
		klog.Errorf("NodePublishVolume; failed to create volume %s; ERR: %v", volName, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to create volume %s", volName))
	}

	if evol.Status != vcontainer.VolumeAvailableStatus {
		targetStatus := []string{vcontainer.VolumeAvailableStatus}
		err := s.Cloud.WaitVolumeTargetStatus(evol.VolumeId, targetStatus)
		if err != nil {
			return nil, status.Errorf(codes.Internal, err.Error())
		}
	}

	klog.V(4).Infof("NodePublishVolume; volume %s (%s) created successfully", volName, evol.VolumeId)

	nodeID, err := s.Metadata.GetInstanceID()
	if err != nil {
		klog.Errorf("NodePublishVolume; failed to get node ID; ERR: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("retrieving instance ID from MetaData service failed with error %v", err))
	}

	_, err = s.Cloud.AttachVolume(nodeID, evol.VolumeId)
	if err != nil {
		klog.Errorf("NodePublishVolume; failed to attach volume %s to node %s; ERR: %v", evol.VolumeId, nodeID, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to attach volume %s to node %s", evol.VolumeId, nodeID))
	}

	err = s.Cloud.WaitDiskAttached(nodeID, evol.VolumeId)
	if err != nil {
		klog.Errorf("NodePublishVolume; failed to wait for volume %s to be attached to node %s; ERR: %v", evol.VolumeId, nodeID, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to wait for volume %s to be attached to node %s", evol.VolumeId, nodeID))
	}

	devicePath, err := getDevicePath(evol.VolumeId, s.Mount)
	if err != nil {
		klog.Errorf("NodePublishVolume; failed to get device path for volume %s; ERR: %v", evol.VolumeId, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get device path for volume %s", evol.VolumeId))
	}

	targetPath := req.GetTargetPath()
	notMnt, err := s.Mount.IsLikelyNotMountPointAttach(targetPath)
	if err != nil {
		klog.Errorf("NodePublishVolume; failed to check if target path %s is a mount point; ERR: %v", targetPath, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to check if target path %s is a mount point", targetPath))
	}

	if notMnt {
		err = s.Mount.Mounter().FormatAndMount(devicePath, targetPath, fsType, nil)
		if err != nil {
			klog.Errorf("NodePublishVolume; failed to mount volume %s to target path %s; ERR: %v", evol.VolumeId, targetPath, err)
			return nil, status.Error(codes.Internal, fmt.Sprintf("failed to mount volume %s to target path %s", evol.VolumeId, targetPath))
		}
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

func (s *nodeServer) nodePublishVolumeForBlock(req *csi.NodePublishVolumeRequest, mountOpts []string) (*csi.NodePublishVolumeResponse, error) {
	klog.V(4).Infof("NodePublishVolume; called with req: %#v", req)

	volumeID := req.GetVolumeId()
	targetPath := req.GetTargetPath()
	podVolumePath := filepath.Dir(targetPath)

	source, err := getDevicePath(volumeID, s.Mount)
	if err != nil {
		klog.Errorf("NodePublishVolume; failed to get device path for volume %s; ERR: %v", volumeID, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get device path for volume %s", volumeID))
	}

	exists, err := utilpath.Exists(utilpath.CheckFollowSymlink, podVolumePath)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !exists {
		if err := s.Mount.MakeDir(podVolumePath); err != nil {
			klog.Errorf("NodePublishVolume; failed to create dir %q: %v", podVolumePath, err)
			return nil, status.Errorf(codes.Internal, "Could not create dir %q: %v", podVolumePath, err)
		}
	}
	err = s.Mount.MakeFile(targetPath)
	if err != nil {
		klog.Errorf("NodePublishVolume; failed to create file %q: %v", targetPath, err)
		return nil, status.Errorf(codes.Internal, "Error in making file %v", err)
	}

	if err := s.Mount.Mounter().Mount(source, targetPath, "", mountOpts); err != nil {
		if removeErr := os.Remove(targetPath); removeErr != nil {
			klog.Errorf("NodePublishVolume; failed to remove mount target %q: %v", targetPath, removeErr)
			return nil, status.Errorf(codes.Internal, "Could not remove mount target %q: %v", targetPath, err)
		}

		klog.Errorf("NodePublishVolume; failed to mount %q at %q: %v", source, targetPath, err)
		return nil, status.Errorf(codes.Internal, "Could not mount %q at %q: %v", source, targetPath, err)
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

func (s *nodeServer) nodeUnpublishEphemeral(_ *csi.NodeUnpublishVolumeRequest, vol *obj.Volume) (*csi.NodeUnpublishVolumeResponse, error) {
	volumeID := vol.VolumeId
	var instanceID string

	if isAttachment(vol.VmId) {
		instanceID = *vol.VmId
	} else {
		return nil, status.Error(codes.FailedPrecondition, "Volume attachment not found in request")
	}

	_ = s.Cloud.DetachVolume(instanceID, volumeID)

	if err := s.Cloud.WaitDiskDetached(instanceID, volumeID); err != nil {
		klog.V(3).Infof("Failed to WaitDiskDetached: %v", err)
		return nil, status.Error(codes.Internal, err.Error())
	}

	if err := s.Cloud.DeleteVolume(volumeID); err != nil {
		klog.V(3).Infof("Failed to DeleteVolume: %v", err)
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}
