package entity

import lsdkEntity "github.com/vngcloud/vngcloud-go-sdk/v2/vngcloud/entity"

const (
	VolumeInUseStatus = "IN-USE"
)

type Volume struct {
	*lsdkEntity.Volume
}

func (s *Volume) IsError() bool {
	return s.Status == "ERROR"
}

func (s *Volume) IsMigration() bool {
	return s.Status == "MIGRATING"
}

func (s *Volume) IsCreating() bool {
	return s.Status == "CREATING" || s.Status == "CREATING-BILLING"
}

func (s *Volume) AttachedTheInstance(pinstanceId string) bool {
	if s.VmId != nil && *s.VmId == pinstanceId {
		return true
	}

	for _, machineId := range s.AttachedMachine {
		if machineId == pinstanceId {
			return true
		}
	}

	return false
}

func (s *Volume) CanDelete() bool {
	if len(s.AttachedMachine) < 1 {
		return true
	}

	if s.VmId == nil {
		return true
	}

	if *s.VmId == "" {
		return true
	}

	return false
}

func (s *Volume) IsAttched() bool {
	if s.Status == VolumeInUseStatus {
		return true
	}

	if len(s.AttachedMachine) > 0 {
		return true
	}

	if s.VmId != nil && *s.VmId != "" {
		return true
	}

	return false
}
