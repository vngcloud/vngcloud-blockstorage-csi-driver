package mount

import (
	"sync"
	"time"
)

const (
	operationFinishInitDelay = 1 * time.Second
	operationFinishFactor    = 1.1
	operationFinishSteps     = 15
)

var (
	mountIns     IMount
	mountInsOnce sync.Once
)
