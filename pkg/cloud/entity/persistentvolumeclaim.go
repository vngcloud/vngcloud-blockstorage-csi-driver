package entity

import (
	lcorev1 "k8s.io/api/core/v1"

	lsconst "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/constant"
)

const (
	encryptedKey = lsconst.VngCloudCsiDriverNameAnnotation + "/" + lsconst.EncryptedSuffixAnnotation
)

type PersistentVolumeClaim struct {
	*lcorev1.PersistentVolumeClaim
}

func NewPersistentVolumeClaim(ppvc *lcorev1.PersistentVolumeClaim) *PersistentVolumeClaim {
	return &PersistentVolumeClaim{
		PersistentVolumeClaim: ppvc,
	}
}

func (s *PersistentVolumeClaim) GetStorageClassName() string {
	return *s.Spec.StorageClassName
}

func (s *PersistentVolumeClaim) GetCsiEncryptedAnnotation() string {
	return s.Annotations[encryptedKey]
}
