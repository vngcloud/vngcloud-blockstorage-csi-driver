package entity

import (
	lcorev1 "k8s.io/api/core/v1"
)

type PersistentVolume struct {
	*lcorev1.PersistentVolume
}

func NewPersistentVolume(ppv *lcorev1.PersistentVolume) *PersistentVolume {
	return &PersistentVolume{
		PersistentVolume: ppv,
	}
}
