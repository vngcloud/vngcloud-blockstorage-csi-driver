package driver

import "sync"

type modifyVolumeManager struct {
	requestHandlerMap sync.Map
}

func newModifyVolumeManager() *modifyVolumeManager {
	return &modifyVolumeManager{
		requestHandlerMap: sync.Map{},
	}
}
