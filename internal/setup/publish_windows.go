package setup

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Matches catalog publication: native no-replace rename, including empty targets.
func publishNoReplace(parent *os.File, from, to string) error {
	objectName, err := windows.NewNTUnicodeString(from)
	if err != nil {
		return err
	}
	attrs := windows.OBJECT_ATTRIBUTES{RootDirectory: windows.Handle(parent.Fd()), ObjectName: objectName, Attributes: windows.OBJ_DONT_REPARSE}
	attrs.Length = uint32(unsafe.Sizeof(attrs))
	var handle windows.Handle
	var status windows.IO_STATUS_BLOCK
	err = windows.NtCreateFile(&handle, windows.DELETE|windows.SYNCHRONIZE, &attrs, &status, nil, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, windows.FILE_OPEN,
		windows.FILE_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT|windows.FILE_SYNCHRONOUS_IO_NONALERT, 0, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	var info struct {
		ReplaceIfExists uint8
		RootDirectory   windows.Handle
		FileNameLength  uint32
		FileName        [64]uint16
	}
	u16, err := windows.UTF16FromString(to)
	if err != nil {
		return err
	}
	if len(u16)-1 > len(info.FileName) {
		return windows.ERROR_INVALID_NAME
	}
	copy(info.FileName[:], u16)
	info.RootDirectory = windows.Handle(parent.Fd())
	info.FileNameLength = uint32((len(u16) - 1) * 2)
	return windows.NtSetInformationFile(handle, &status, (*byte)(unsafe.Pointer(&info)),
		uint32(unsafe.Offsetof(info.FileName))+info.FileNameLength, windows.FileRenameInformation)
}
