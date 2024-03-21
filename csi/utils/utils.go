package utils

import (
	"github.com/container-storage-interface/spec/lib/go/csi"
	"k8s.io/klog/v2"
	"time"
)

type MyDuration struct {
	time.Duration
}

func RoundUpSize(volumeSizeBytes int64, allocationUnitBytes int64) int64 {
	roundedUp := volumeSizeBytes / allocationUnitBytes
	if volumeSizeBytes%allocationUnitBytes > 0 {
		roundedUp++
	}
	return roundedUp
}

func GetAZFromTopology(topologyKey string, requirement *csi.TopologyRequirement) string {
	var zone string
	var exists bool

	defer func() { klog.V(1).Infof("detected AZ from the topology: %s", zone) }()
	klog.V(4).Infof("preferred topology requirement: %+v", requirement.GetPreferred())
	klog.V(4).Infof("requisite topology requirement: %+v", requirement.GetRequisite())

	for _, topology := range requirement.GetPreferred() {
		zone, exists = topology.GetSegments()[topologyKey]
		if exists {
			return zone
		}
	}

	for _, topology := range requirement.GetRequisite() {
		zone, exists = topology.GetSegments()[topologyKey]
		if exists {
			return zone
		}
	}

	return zone
}
