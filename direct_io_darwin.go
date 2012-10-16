// Direct IO for darwin

package main

import (
	"fmt"
	"os"
	"syscall"
)

const (
	// OSX doesn't need any alignment
	AlignSize = 0
)

func OpenFile(name string, flag int, perm os.FileMode) (file *os.File, err error) {
	file, err = os.OpenFile(name, flag, perm)
	if err != nil {
		return
	}

	// Set F_NOCACHE to avoid caching
	// F_NOCACHE    Turns data caching off/on. A non-zero value in arg turns data caching off.  A value
	//              of zero in arg turns data caching on.
	_, _, err = syscall.Syscall(syscall.SYS_FCNTL, uintptr(file.Fd()), syscall.F_NOCACHE, 1)
	if err != nil {
		fmt.Errorf("Failed to set F_NOCACHE: %s", err)
		file.Close()
		file = nil
	}

	return
}
