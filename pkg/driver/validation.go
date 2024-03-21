package driver

import (
	"errors"
	"fmt"
)

func ValidateDriverOptions(options *DriverOptions) error {
	if err := validateMode(options.mode); err != nil {
		return fmt.Errorf("Invalid mode: %w", err)
	}

	if options.modifyVolumeRequestHandlerTimeout == 0 {
		return errors.New("Invalid modifyVolumeRequestHandlerTimeout: Timeout cannot be zero")
	}

	return nil
}

func validateMode(mode Mode) error {
	if mode != AllMode && mode != ControllerMode && mode != NodeMode {
		return fmt.Errorf("Mode is not supported (actual: %s, supported: %v)", mode, []Mode{AllMode, ControllerMode, NodeMode})
	}

	return nil
}
