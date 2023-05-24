package control

import (
	"fmt"
	"os"
)

func cleanData(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("cleaning data path '%s': %w", path, err)
	}
	return nil
}
