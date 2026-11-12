//go:build windows

package autosync

import (
	"syscall"
	"unsafe"
)

func availableMemory() uint64 {
	var status struct {
		Length, Load                                                                                         uint32
		TotalPhys, AvailPhys, TotalPageFile, AvailPageFile, TotalVirtual, AvailVirtual, AvailExtendedVirtual uint64
	}
	status.Length = uint32(unsafe.Sizeof(status))
	fn := syscall.NewLazyDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")
	ok, _, _ := fn.Call(uintptr(unsafe.Pointer(&status)))
	if ok == 0 {
		return 2 << 30
	}
	return status.AvailPhys
}
