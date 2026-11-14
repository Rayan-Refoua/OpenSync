//go:build windows

package storageio

import (
	"encoding/binary"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unsafe"
)

// Primary API references:
// https://learn.microsoft.com/windows/win32/fileio/basic-and-dynamic-disks
// https://learn.microsoft.com/windows/win32/api/winioctl/ni-winioctl-ioctl_volume_get_volume_disk_extents
// https://learn.microsoft.com/windows-hardware/drivers/ddi/ntddstor/ns-ntddstor-_device_seek_penalty_descriptor
// https://learn.microsoft.com/windows/win32/api/fileapi/nf-fileapi-getdrivetypew
var (
	kernel32                   = syscall.NewLazyDLL("kernel32.dll")
	getVolumePathName          = kernel32.NewProc("GetVolumePathNameW")
	getVolumeNameForMountPoint = kernel32.NewProc("GetVolumeNameForVolumeMountPointW")
	getDriveType               = kernel32.NewProc("GetDriveTypeW")
)

func resolveVolume(path string) volumeIdentity {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	fallback := filepath.VolumeName(abs)
	if fallback != "" {
		fallback += `\`
	}
	root := fallback
	wide, err := syscall.UTF16PtrFromString(abs)
	if err == nil {
		buf := make([]uint16, 32768)
		ok, _, _ := getVolumePathName.Call(uintptr(unsafe.Pointer(wide)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if ok != 0 {
			root = syscall.UTF16ToString(buf)
		}
	}
	if root == "" {
		return volumeIdentity{key: "unresolved", reason: "Windows could not resolve the source volume"}
	}
	v := volumeIdentity{key: strings.ToLower(root), root: root}
	rootWide, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		v.reason = err.Error()
		return v
	}
	drive, _, _ := getDriveType.Call(uintptr(unsafe.Pointer(rootWide)))
	v.driveType = uint32(drive)
	buf := make([]uint16, 1024)
	ok, _, e := getVolumeNameForMountPoint.Call(uintptr(unsafe.Pointer(rootWide)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if ok != 0 {
		v.device = strings.TrimSuffix(syscall.UTF16ToString(buf), `\`)
		v.key = strings.ToLower(v.device)
	} else {
		v.reason = fmt.Sprintf("volume identity unavailable: %v", e)
		// Drive-letter volume handles are still usable for some virtual drivers.
		if len(fallback) == 3 && fallback[1] == ':' {
			v.device = `\\.\` + fallback[:2]
		}
	}
	return v
}

func openDevice(path string) (syscall.Handle, error) {
	wide, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return syscall.CreateFile(wide, 0, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0, 0)
}

func classifyVolume(v volumeIdentity) Info {
	if v.driveType == 6 { // DRIVE_RAMDISK is an explicit driver classification.
		return Info{Kind: KindRAM, Reason: "Windows reports DRIVE_RAMDISK; parallel source I/O allowed"}
	}
	if v.driveType == 4 { // Remote share physical topology is not locally queryable.
		return unknownVolume(v, "Network volume: remote physical disks are unknown; one source I/O lease per share")
	}
	if v.device == "" {
		return unknownVolume(v, v.reason+"; one source I/O lease per volume")
	}
	h, err := openDevice(v.device)
	if err != nil {
		return unknownVolume(v, fmt.Sprintf("cannot query volume device (%v); one source I/O lease per volume", err))
	}
	defer syscall.CloseHandle(h)
	disks, err := volumeDisks(h)
	if err != nil || len(disks) == 0 {
		// A virtual/RAM driver may report no physical extents but still expose
		// an authoritative non-seeking descriptor. Do not call that an HDD.
		if seek, seekErr := seekPenalty(h); seekErr == nil && !seek {
			return Info{Kind: KindSSD, Reason: "Volume driver reports no seek penalty; physical extents unavailable (possibly virtual/RAM storage)"}
		}
		return unknownVolume(v, fmt.Sprintf("physical disk extents unavailable (%v); one source I/O lease per volume", err))
	}
	info := Info{Kind: KindSSD, DiskIDs: disks}
	var reasons []string
	for _, disk := range disks {
		device := fmt.Sprintf(`\\.\PhysicalDrive%d`, disk)
		physical, err := openDevice(device)
		seek := false
		if err == nil {
			seek, err = seekPenalty(physical)
			syscall.CloseHandle(physical)
		}
		if err != nil {
			if info.Kind != KindHDD {
				info.Kind = KindUnknown
			}
			info.GroupIDs = append(info.GroupIDs, fmt.Sprintf("disk:%d", disk))
			reasons = append(reasons, fmt.Sprintf("disk %d seek penalty unknown (%v); conservatively serialized", disk, err))
		} else if seek {
			info.Kind = KindHDD
			info.GroupIDs = append(info.GroupIDs, fmt.Sprintf("disk:%d", disk))
			reasons = append(reasons, fmt.Sprintf("disk %d reports seek penalty; one active source I/O lease", disk))
		} else {
			reasons = append(reasons, fmt.Sprintf("disk %d reports no seek penalty; parallel source I/O allowed", disk))
		}
	}
	info.Reason = strings.Join(reasons, "; ")
	return info
}

func volumeDisks(h syscall.Handle) ([]uint32, error) {
	// VOLUME_DISK_EXTENTS: DWORD count, alignment padding, then 24-byte
	// DISK_EXTENT values. LONG64 is eight-byte aligned in Windows' ABI.
	buf := make([]byte, 4096)
	for {
		var returned uint32
		err := syscall.DeviceIoControl(h, 0x00560000, nil, 0, &buf[0], uint32(len(buf)), &returned, nil)
		if err == syscall.ERROR_MORE_DATA {
			if len(buf) >= 65536 {
				return nil, fmt.Errorf("volume extent count exceeds bounded descriptor buffer")
			}
			buf = make([]byte, len(buf)*2)
			continue
		}
		if err != nil {
			return nil, err
		}
		if returned < 8 {
			return nil, fmt.Errorf("truncated volume extent header")
		}
		count := int(binary.LittleEndian.Uint32(buf))
		if count < 1 || count > (int(returned)-8)/24 {
			return nil, fmt.Errorf("invalid volume extent count %d", count)
		}
		seen := map[uint32]bool{}
		var disks []uint32
		for i := 0; i < count; i++ {
			disk := binary.LittleEndian.Uint32(buf[8+i*24:])
			if !seen[disk] {
				seen[disk] = true
				disks = append(disks, disk)
			}
		}
		sort.Slice(disks, func(i, j int) bool { return disks[i] < disks[j] })
		return disks, nil
	}
}

func seekPenalty(h syscall.Handle) (bool, error) {
	// STORAGE_PROPERTY_QUERY {StorageDeviceSeekPenaltyProperty=7,
	// PropertyStandardQuery=0, AdditionalParameters[1]}, padded to 12 bytes.
	query := make([]byte, 12)
	binary.LittleEndian.PutUint32(query, 7)
	descriptor := make([]byte, 12)
	var returned uint32
	err := syscall.DeviceIoControl(h, 0x002d1400, &query[0], uint32(len(query)), &descriptor[0], uint32(len(descriptor)), &returned, nil)
	if err != nil {
		return false, err
	}
	if returned < 9 || binary.LittleEndian.Uint32(descriptor[4:]) < 9 {
		return false, fmt.Errorf("truncated seek-penalty descriptor")
	}
	return descriptor[8] != 0, nil
}
