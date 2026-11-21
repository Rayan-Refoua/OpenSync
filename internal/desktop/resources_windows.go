//go:build windows

package desktop

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var (
	resourceKernel32       = syscall.NewLazyDLL("kernel32.dll")
	createProcessSnapshot  = resourceKernel32.NewProc("CreateToolhelp32Snapshot")
	processFirst           = resourceKernel32.NewProc("Process32FirstW")
	processNext            = resourceKernel32.NewProc("Process32NextW")
	openResourceProcess    = resourceKernel32.NewProc("OpenProcess")
	resourceProcessTimes   = resourceKernel32.NewProc("GetProcessTimes")
	resourceMemoryCounters = resourceKernel32.NewProc("K32GetProcessMemoryInfo")
	resourceSystemTimes    = resourceKernel32.NewProc("GetSystemTimes")
	resourceGlobalMemory   = resourceKernel32.NewProc("GlobalMemoryStatusEx")
)

type processEntry32 struct {
	Size, Usage, ProcessID uint32
	DefaultHeapID          uintptr
	ModuleID, Threads      uint32
	ParentProcessID        uint32
	PriorityClassBase      int32
	Flags                  uint32
	ExeFile                [260]uint16
}

type processMemoryCounters struct {
	CB, PageFaultCount         uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
	PrivateUsage               uintptr
}

func fileTimeValue(v syscall.Filetime) uint64 {
	return uint64(v.HighDateTime)<<32 | uint64(v.LowDateTime)
}

func readResourceUsage() (resourceReading, error) {
	r := resourceReading{Root: uint32(os.Getpid()), Processes: map[uint32]processUsage{}}
	h, _, err := createProcessSnapshot.Call(2, 0) // TH32CS_SNAPPROCESS
	if h == ^uintptr(0) {
		return r, fmt.Errorf("process resource snapshot: %w", err)
	}
	defer syscall.CloseHandle(syscall.Handle(h))
	entry := processEntry32{}
	entry.Size = uint32(unsafe.Sizeof(entry))
	ok, _, err := processFirst.Call(h, uintptr(unsafe.Pointer(&entry)))
	if ok == 0 {
		return r, fmt.Errorf("enumerate process resources: %w", err)
	}
	var list []processUsage
	for ok != 0 {
		list = append(list, processUsage{PID: entry.ProcessID, Parent: entry.ParentProcessID, Name: syscall.UTF16ToString(entry.ExeFile[:])})
		ok, _, _ = processNext.Call(h, uintptr(unsafe.Pointer(&entry)))
	}
	ids := descendantIDs(r.Root, list)
	for _, p := range list {
		if !ids[p.PID] {
			continue
		}
		handle, _, _ := openResourceProcess.Call(0x1000, 0, uintptr(p.PID)) // QUERY_LIMITED_INFORMATION
		if handle == 0 {
			continue // child can exit between snapshot and open
		}
		var creation, exit, kernel, user syscall.Filetime
		timesOK, _, _ := resourceProcessTimes.Call(handle, uintptr(unsafe.Pointer(&creation)), uintptr(unsafe.Pointer(&exit)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
		memory := processMemoryCounters{}
		memory.CB = uint32(unsafe.Sizeof(memory))
		memoryOK, _, _ := resourceMemoryCounters.Call(handle, uintptr(unsafe.Pointer(&memory)), uintptr(memory.CB))
		syscall.CloseHandle(syscall.Handle(handle))
		if timesOK == 0 || memoryOK == 0 {
			continue
		}
		p.Created = fileTimeValue(creation)
		p.CPU100NS = fileTimeValue(kernel) + fileTimeValue(user)
		p.WorkingSet = uint64(memory.WorkingSetSize)
		p.Private = uint64(memory.PrivateUsage)
		r.Processes[p.PID] = p
	}
	if _, ok := r.Processes[r.Root]; !ok {
		return r, fmt.Errorf("OpenSync process counters are unavailable")
	}
	var idle, kernel, user syscall.Filetime
	ok, _, err = resourceSystemTimes.Call(uintptr(unsafe.Pointer(&idle)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	if ok == 0 {
		return r, fmt.Errorf("system CPU counters: %w", err)
	}
	r.System.Idle, r.System.Kernel, r.System.User = fileTimeValue(idle), fileTimeValue(kernel), fileTimeValue(user)
	var memory struct {
		Length, Load                                                                                         uint32
		TotalPhys, AvailPhys, TotalPageFile, AvailPageFile, TotalVirtual, AvailVirtual, AvailExtendedVirtual uint64
	}
	memory.Length = uint32(unsafe.Sizeof(memory))
	ok, _, err = resourceGlobalMemory.Call(uintptr(unsafe.Pointer(&memory)))
	if ok == 0 {
		return r, fmt.Errorf("system memory counters: %w", err)
	}
	r.System.TotalRAM, r.System.FreeRAM = memory.TotalPhys, memory.AvailPhys
	return r, nil
}
