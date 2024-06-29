package entity

import (
	lcorev1 "k8s.io/api/core/v1"
	lk8sstoragev1 "k8s.io/api/storage/v1"
)

type StorageClass struct {
	*lk8sstoragev1.StorageClass
}

func NewStorageClass(psc *lk8sstoragev1.StorageClass) *StorageClass {
	return &StorageClass{
		StorageClass: psc,
	}
}

func (s *StorageClass) GetReclaimPolicyAsString() string {
	if s.ReclaimPolicy == nil {
		return "undefined"
	}

	switch *s.ReclaimPolicy {
	case lcorev1.PersistentVolumeReclaimRecycle:
		return "recycle"
	case lcorev1.PersistentVolumeReclaimDelete:
		return "delete"
	case lcorev1.PersistentVolumeReclaimRetain:
		return "retain"
	}

	return "undefined"
}
