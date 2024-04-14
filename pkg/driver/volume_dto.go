package driver

import (
	"fmt"
	lstr "strings"

	lcsi "github.com/container-storage-interface/spec/lib/go/csi"
	ljoat "github.com/cuongpiger/joat/string"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	lscloud "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/cloud"
	lsdkVolV2 "github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/blockstorage/v2/volume"
)

type CreateVolumeRequest struct {
	ClusterID          string // ClusterID is the ID of the cluster where the volume will be created.
	VolumeTypeID       string // the volume type UUID from the VngCloud portal API.
	VolumeName         string // the name of the PersistentVolume
	IsMultiAttach      bool   // whether the volume can be attached to multiple nodes
	VolumeSize         uint64 // the size of the volume in GB
	EncryptedAlgorithm string
	PvcNameTag         string // the name of the PVC on the PVC's Annotation
	PvcNamespaceTag    string // the namespace of the PVC on the PVC's Annotation
	PvNameTag          string // the name of the PV on the PVC's Annotation
	IsPoc              bool   // whether the volume is a PoC volume
	SnapshotID         string // the ID of the snapshot to create the volume from
	CreateFrom         lsdkVolV2.CreateOptsCreateFrom

	// The scope of mount commands
	BlockSize       string
	InodeSize       string
	BytesPerInode   string
	NumberOfInodes  string
	Ext4ClusterSize string
	Ext4BigAlloc    bool
}

type encryptedAlgorithm string

const (
	AesXtsPlain64_128 = encryptedAlgorithm("aes-xts-plain64_128")
	AesXtsPlain64_256 = encryptedAlgorithm("aes-xts-plain64_256")
)

func NewCreateVolumeRequest() *CreateVolumeRequest {
	return &CreateVolumeRequest{
		CreateFrom: lsdkVolV2.CreateFromNew, // set default value for createFrom field
	}
}

func (s *CreateVolumeRequest) WithClusterID(pclusterID string) *CreateVolumeRequest {
	s.ClusterID = pclusterID
	return s
}

func (s *CreateVolumeRequest) WithVolumeTypeID(pvolumeTypeID string) *CreateVolumeRequest {
	if pvolumeTypeID == "" {
		return s
	}

	s.VolumeTypeID = pvolumeTypeID
	return s
}

func (s *CreateVolumeRequest) WithVolumeName(pvolumeName string) *CreateVolumeRequest {
	s.VolumeName = pvolumeName
	return s
}

func (s *CreateVolumeRequest) WithMultiAttach(pisMultiAttach bool) *CreateVolumeRequest {
	s.IsMultiAttach = pisMultiAttach
	return s
}

func (s *CreateVolumeRequest) WithVolumeSize(pvolumeSize uint64) *CreateVolumeRequest {
	s.VolumeSize = pvolumeSize
	return s
}

func (s *CreateVolumeRequest) WithPvcNameTag(ppvcNameTag string) *CreateVolumeRequest {
	s.PvcNameTag = ppvcNameTag
	return s
}

func (s *CreateVolumeRequest) WithPvcNamespaceTag(ppvcNamespaceTag string) *CreateVolumeRequest {
	s.PvcNamespaceTag = ppvcNamespaceTag
	return s
}

func (s *CreateVolumeRequest) WithPvNameTag(ppvNameTag string) *CreateVolumeRequest {
	s.PvNameTag = ppvNameTag
	return s
}

func (s *CreateVolumeRequest) WithBlockSize(pblockSize string) *CreateVolumeRequest {
	s.BlockSize = pblockSize
	return s
}

func (s *CreateVolumeRequest) WithInodeSize(pinodeSize string) *CreateVolumeRequest {
	s.InodeSize = pinodeSize
	return s
}

func (s *CreateVolumeRequest) WithBytesPerInode(pbytesPerInode string) *CreateVolumeRequest {
	s.BytesPerInode = pbytesPerInode
	return s
}

func (s *CreateVolumeRequest) WithNumberOfInodes(pnumberOfInodes string) *CreateVolumeRequest {
	s.NumberOfInodes = pnumberOfInodes
	return s
}

func (s *CreateVolumeRequest) WithExt4BigAlloc(pext4BigAlloc bool) *CreateVolumeRequest {
	s.Ext4BigAlloc = pext4BigAlloc
	return s
}

func (s *CreateVolumeRequest) WithPoc(pisPoc bool) *CreateVolumeRequest {
	s.IsPoc = pisPoc
	return s
}

func (s *CreateVolumeRequest) WithExt4ClusterSize(pext4ClusterSize string) *CreateVolumeRequest {
	s.Ext4ClusterSize = pext4ClusterSize
	return s
}

func (s *CreateVolumeRequest) WithSnapshotID(psnapshotID string) *CreateVolumeRequest {
	s.SnapshotID = psnapshotID
	if psnapshotID != "" {
		s.CreateFrom = lsdkVolV2.CreateFromSnapshot
	}

	return s
}

func (s *CreateVolumeRequest) WithEncrypted(pencrypted string) *CreateVolumeRequest {
	pencrypted = lstr.ToLower(pencrypted)
	switch pencrypted {
	case string(AesXtsPlain64_128):
		s.EncryptedAlgorithm = pencrypted
	case string(AesXtsPlain64_256):
		s.EncryptedAlgorithm = pencrypted
	default:
		s.EncryptedAlgorithm = ""
	}

	return s
}

func (s *CreateVolumeRequest) ToSdkCreateVolumeOpts(pdo *DriverOptions) *lsdkVolV2.CreateOpts {
	opts := new(lsdkVolV2.CreateOpts)

	opts.IsPoc = s.IsPoc
	opts.MultiAttach = s.IsMultiAttach
	opts.Name = s.VolumeName
	opts.Size = s.VolumeSize
	opts.VolumeTypeId = s.VolumeTypeID
	opts.CreatedFrom = s.CreateFrom
	opts.EncryptionType = s.EncryptedAlgorithm
	opts.Tags = s.prepareTag(pdo.GetTagKeyLength(), pdo.GetTagValueLength())

	if s.SnapshotID != "" {
		opts.ConfigureVolumeRestore = &lsdkVolV2.ConfigureVolumeRestore{
			SnapshotVolumePointId: s.SnapshotID,
			VolumeTypeId:          s.VolumeTypeID,
		}
	}

	return opts
}

func (s *CreateVolumeRequest) prepareTag(ptkl, ptvl int) []lsdkVolV2.CreateOptsTag {
	var vts []lsdkVolV2.CreateOptsTag
	if s.ClusterID != "" {
		vts = append(vts, lsdkVolV2.CreateOptsTag{
			Key:   ljoat.Truncate(lscloud.VksClusterIdTagKey, ptkl),
			Value: ljoat.Truncate(s.ClusterID, ptvl),
		})
	}

	if s.PvcNameTag != "" {
		vts = append(vts, lsdkVolV2.CreateOptsTag{
			Key:   ljoat.Truncate(lscloud.VksPvcNameTagKey, ptkl),
			Value: ljoat.Truncate(s.PvcNameTag, ptvl),
		})
	}

	if s.PvcNamespaceTag != "" {
		vts = append(vts, lsdkVolV2.CreateOptsTag{
			Key:   ljoat.Truncate(lscloud.VksPvcNamespaceTagKey, ptkl),
			Value: ljoat.Truncate(s.PvcNamespaceTag, ptvl),
		})
	}

	if s.PvcNameTag != "" {
		vts = append(vts, lsdkVolV2.CreateOptsTag{
			Key:   ljoat.Truncate(lscloud.VksPvNameTagKey, ptkl),
			Value: ljoat.Truncate(s.PvNameTag, ptvl),
		})
	}

	if s.SnapshotID != "" {
		vts = append(vts, lsdkVolV2.CreateOptsTag{
			Key:   ljoat.Truncate(lscloud.VksSnapshotIdTagKey, ptkl),
			Value: ljoat.Truncate(s.SnapshotID, ptvl),
		})
	}

	return vts
}

func (s *CreateVolumeRequest) ToResponseContext(pvolCap []*lcsi.VolumeCapability) (map[string]string, error) {
	responseCtx := map[string]string{}
	if len(s.BlockSize) > 0 {
		responseCtx[BlockSizeKey] = s.BlockSize
		if err := validateFormattingOption(pvolCap, BlockSizeKey, FileSystemConfigs); err != nil {
			return nil, err
		}
	}

	if len(s.InodeSize) > 0 {
		responseCtx[InodeSizeKey] = s.InodeSize
		if err := validateFormattingOption(pvolCap, InodeSizeKey, FileSystemConfigs); err != nil {
			return nil, err
		}
	}
	if len(s.BytesPerInode) > 0 {
		responseCtx[BytesPerInodeKey] = s.BytesPerInode
		if err := validateFormattingOption(pvolCap, BytesPerInodeKey, FileSystemConfigs); err != nil {
			return nil, err
		}
	}
	if len(s.NumberOfInodes) > 0 {
		responseCtx[NumberOfInodesKey] = s.NumberOfInodes
		if err := validateFormattingOption(pvolCap, NumberOfInodesKey, FileSystemConfigs); err != nil {
			return nil, err
		}
	}
	if s.Ext4BigAlloc {
		responseCtx[Ext4BigAllocKey] = "true"
		if err := validateFormattingOption(pvolCap, Ext4BigAllocKey, FileSystemConfigs); err != nil {
			return nil, err
		}
	}
	if len(s.Ext4ClusterSize) > 0 {
		responseCtx[Ext4ClusterSizeKey] = s.Ext4ClusterSize
		if err := validateFormattingOption(pvolCap, Ext4ClusterSizeKey, FileSystemConfigs); err != nil {
			return nil, err
		}
	}

	return responseCtx, nil
}

func validateFormattingOption(volumeCapabilities []*lcsi.VolumeCapability, paramName string, fsConfigs map[string]fileSystemConfig) error {
	for _, volCap := range volumeCapabilities {
		switch volCap.GetAccessType().(type) {
		case *lcsi.VolumeCapability_Block:
			return status.Error(codes.InvalidArgument, fmt.Sprintf("Cannot use %s with block volume", paramName))
		}

		mountVolume := volCap.GetMount()
		if mountVolume == nil {
			return status.Error(codes.InvalidArgument, "CreateVolume: mount is nil within volume capability")
		}

		fsType := mountVolume.GetFsType()
		if supported := fsConfigs[fsType].isParameterSupported(paramName); !supported {
			return status.Errorf(codes.InvalidArgument, "Cannot use %s with fstype %s", paramName, fsType)
		}
	}

	return nil
}

func (fsConfig fileSystemConfig) isParameterSupported(paramName string) bool {
	_, notSupported := fsConfig.NotSupportedParams[paramName]
	return !notSupported
}

type fileSystemConfig struct {
	NotSupportedParams map[string]struct{}
}
