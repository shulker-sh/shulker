package fsutil

import (
	"fmt"
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
