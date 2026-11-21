package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"

	ole "github.com/go-ole/go-ole"
	"golang.org/x/sys/windows"
)

var (
	fileOpenClass     = ole.NewGUID("{DC1C5A9C-E88A-4DDE-A5A1-60F82A20AEF7}")
	fileOpenInterface = ole.NewGUID("{D57C7288-D4AD-4768-BE02-9D969532D960}")
	dialogUser32      = windows.NewLazySystemDLL("user32.dll")
)

//go:uintptrescapes
func dialogCall(object *ole.IUnknown, method int, args ...uintptr) uint32 {
	table := *(**[32]uintptr)(unsafe.Pointer(object))
	result, _, _ := syscall.SyscallN(table[method], append([]uintptr{uintptr(unsafe.Pointer(object))}, args...)...)
	runtime.KeepAlive(object)
	return uint32(result)
}

func selectMediaFolders(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// S_FALSE still represents a successful initialization and needs cleanup.
	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
		if h, ok := err.(*ole.OleError); !ok || h.Code() != 1 {
			return nil, err
		}
	}
	defer ole.CoUninitialize()
	dialog, err := ole.CreateInstance(fileOpenClass, fileOpenInterface)
	if err != nil {
		return nil, err
	}
	defer dialog.Release()
	var options uint32
	if hr := dialogCall(dialog, 10, uintptr(unsafe.Pointer(&options))); hr != 0 {
		return nil, fmt.Errorf("get folder picker options: HRESULT %#x", hr)
	}
	const pickFolders, forceFilesystem, allowMultiSelect = 0x20, 0x40, 0x200
	if hr := dialogCall(dialog, 9, uintptr(options|pickFolders|forceFilesystem|allowMultiSelect)); hr != 0 {
		return nil, fmt.Errorf("set folder picker options: HRESULT %#x", hr)
	}
	title, _ := windows.UTF16PtrFromString("Add media folders (Ctrl or Shift to select multiple)")
	if hr := dialogCall(dialog, 17, uintptr(unsafe.Pointer(title))); hr != 0 {
		return nil, fmt.Errorf("set folder picker title: HRESULT %#x", hr)
	}
	runtime.KeepAlive(title)
	owner, _, _ := dialogUser32.NewProc("GetForegroundWindow").Call()
	var pid uint32
	dialogUser32.NewProc("GetWindowThreadProcessId").Call(owner, uintptr(unsafe.Pointer(&pid)))
	if pid != uint32(os.Getpid()) {
		owner = 0
	}
	if hr := dialogCall(dialog, 3, owner); hr != 0 {
		if hr == 0x800704c7 {
			return nil, nil
		}
		return nil, fmt.Errorf("open folder picker: HRESULT %#x", hr)
	}
	var items *ole.IUnknown
	if hr := dialogCall(dialog, 27, uintptr(unsafe.Pointer(&items))); hr != 0 {
		return nil, fmt.Errorf("get selected folders: HRESULT %#x", hr)
	}
	defer items.Release()
	var count uint32
	if hr := dialogCall(items, 7, uintptr(unsafe.Pointer(&count))); hr != 0 {
		return nil, fmt.Errorf("count selected folders: HRESULT %#x", hr)
	}
	paths := make([]string, 0, count)
	for i := uint32(0); i < count; i++ {
		var item *ole.IUnknown
		if hr := dialogCall(items, 8, uintptr(i), uintptr(unsafe.Pointer(&item))); hr != 0 {
			return nil, fmt.Errorf("get selected folder: HRESULT %#x", hr)
		}
		var path *uint16
		const filesystemPath = 0x80058000
		hr := dialogCall(item, 5, filesystemPath, uintptr(unsafe.Pointer(&path)))
		item.Release()
		if hr != 0 {
			return nil, fmt.Errorf("read selected folder path: HRESULT %#x", hr)
		}
		paths = append(paths, windows.UTF16PtrToString(path))
		ole.CoTaskMemFree(uintptr(unsafe.Pointer(path)))
	}
	return paths, nil
}
