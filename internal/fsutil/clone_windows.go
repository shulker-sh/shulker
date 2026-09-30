package fsutil

import (
	"fmt"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var copyFile2 = windows.NewLazySystemDLL("kernel32.dll").NewProc("CopyFile2")

// copyFile2Params is COPYFILE2_EXTENDED_PARAMETERS.
type copyFile2Params struct {
	size            uint32
	copyFlags       uint32
	cancel          *int32
	progressRoutine uintptr
	callbackContext uintptr
}

const copyFileFailIfExists = 0x1

// cloneFile copies src to dst, which must not exist, with CopyFile2. Windows 11 24H2 and later
// clone on ReFS and Dev Drive volumes and copy everywhere else.
func cloneFile(src, dst string) error {
	from, err := windows.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}
	params := copyFile2Params{copyFlags: copyFileFailIfExists}
	params.size = uint32(unsafe.Sizeof(params))
	hr, _, _ := copyFile2.Call(uintptr(unsafe.Pointer(from)), uintptr(unsafe.Pointer(to)), uintptr(unsafe.Pointer(&params)))
	if hr != 0 {
		return fmt.Errorf("CopyFile2 %s: HRESULT 0x%08x", dst, uint32(hr))
	}
	return nil
}

// canClone can't try a clone: CopyFile2 succeeds whether it cloned or copied. Windows clones on a
// ReFS volume, which a Dev Drive is, from 11 24H2 (build 26100), within one volume.
func canClone(src, dir string) bool {
	vol := filepath.VolumeName(dir)
	if vol == "" || !strings.EqualFold(vol, filepath.VolumeName(src)) {
		return false
	}
	if _, _, build := windows.RtlGetNtVersionNumbers(); build&0xffff < 26100 {
		return false
	}
	root, err := windows.UTF16PtrFromString(vol + `\`)
	if err != nil {
		return false
	}
	name := make([]uint16, windows.MAX_PATH+1)
	if windows.GetVolumeInformation(root, nil, 0, nil, nil, nil, &name[0], uint32(len(name))) != nil {
		return false
	}
	return windows.UTF16ToString(name) == "ReFS"
}
