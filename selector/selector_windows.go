// Package selector muestra el selector de carpetas moderno de Windows
// (IFileOpenDialog con FOS_PICKFOLDERS), que puede abrirse en una carpeta
// concreta y deja navegar libremente al resto del equipo.
package selector

import (
	"errors"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ole32   = windows.NewLazySystemDLL("ole32.dll")
	shell32 = windows.NewLazySystemDLL("shell32.dll")

	procCoCreateInstance            = ole32.NewProc("CoCreateInstance")
	procCoInitializeEx              = ole32.NewProc("CoInitializeEx")
	procCoUninitialize              = ole32.NewProc("CoUninitialize")
	procSHCreateItemFromParsingName = shell32.NewProc("SHCreateItemFromParsingName")

	clsidFileOpenDialog = windows.GUID{Data1: 0xDC1C5A9C, Data2: 0xE88A, Data3: 0x4DDE, Data4: [8]byte{0xA5, 0xA1, 0x60, 0xF8, 0x2A, 0x20, 0xAE, 0xF7}}
	iidIFileOpenDialog  = windows.GUID{Data1: 0xD57C7288, Data2: 0xD4AD, Data3: 0x4768, Data4: [8]byte{0xBE, 0x02, 0x9D, 0x96, 0x95, 0x32, 0xD9, 0x60}}
	iidIShellItem       = windows.GUID{Data1: 0x43826D1E, Data2: 0xE718, Data3: 0x42EE, Data4: [8]byte{0xBC, 0x55, 0xA1, 0xE2, 0x61, 0xC3, 0x7B, 0xFE}}
)

const (
	clsctxInprocServer  = 0x1
	fosPickFolders      = 0x20
	fosForceFileSystem  = 0x40
	fosPathMustExist    = 0x800
	sigdnFileSysPath    = 0x80058000
	hrCancelled         = 0x800704C7 // HRESULT_FROM_WIN32(ERROR_CANCELLED)
	coinitApartmentThrd = 0x2
)

// Índices de los métodos en la vtable de IFileOpenDialog / IShellItem.
const (
	mRelease     = 2
	mShow        = 3
	mSetOptions  = 9
	mGetOptions  = 10
	mSetFolder   = 12
	mSetTitle    = 17
	mGetResult   = 20
	mGetDispName = 5 // IShellItem::GetDisplayName
)

// ErrNoDisponible indica que no se pudo crear el diálogo moderno.
var ErrNoDisponible = errors.New("selector de carpetas moderno no disponible")

// call invoca el método n de la interfaz COM obj.
func call(obj unsafe.Pointer, n int, args ...uintptr) uintptr {
	vtbl := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vtbl, uintptr(n)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{uintptr(obj)}, args...)...)
	return r
}

func failed(hr uintptr) bool { return int32(hr) < 0 }

// Carpeta abre el selector en la carpeta inicial (si existe) y devuelve la
// carpeta elegida. ok es false si el usuario canceló.
func Carpeta(owner uintptr, titulo, inicial string) (ruta string, ok bool, err error) {
	// S_OK (0) o S_FALSE (1): COM iniciado (o ya lo estaba) -> hay que equilibrar con CoUninitialize.
	// RPC_E_CHANGED_MODE: ya estaba iniciado en otro modo; se puede seguir sin más.
	switch hr, _, _ := procCoInitializeEx.Call(0, coinitApartmentThrd); uint32(hr) {
	case 0, 1:
		defer procCoUninitialize.Call()
	case 0x80010106:
	default:
		return "", false, ErrNoDisponible
	}

	var dlg unsafe.Pointer
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidFileOpenDialog)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIFileOpenDialog)), uintptr(unsafe.Pointer(&dlg)))
	if failed(hr) || dlg == nil {
		return "", false, ErrNoDisponible
	}
	defer call(dlg, mRelease)

	var opts uint32
	call(dlg, mGetOptions, uintptr(unsafe.Pointer(&opts)))
	call(dlg, mSetOptions, uintptr(opts|fosPickFolders|fosForceFileSystem|fosPathMustExist))

	if t, err := windows.UTF16PtrFromString(titulo); err == nil {
		call(dlg, mSetTitle, uintptr(unsafe.Pointer(t)))
	}

	if inicial != "" {
		if p, err := windows.UTF16PtrFromString(inicial); err == nil {
			var item unsafe.Pointer
			hr, _, _ := procSHCreateItemFromParsingName.Call(uintptr(unsafe.Pointer(p)), 0,
				uintptr(unsafe.Pointer(&iidIShellItem)), uintptr(unsafe.Pointer(&item)))
			if !failed(hr) && item != nil {
				call(dlg, mSetFolder, uintptr(item)) // SetFolder: siempre abre aquí, aunque Windows recuerde otra
				call(item, mRelease)
			}
		}
	}

	hr = call(dlg, mShow, owner)
	if uint32(hr) == hrCancelled {
		return "", false, nil
	}
	if failed(hr) {
		return "", false, ErrNoDisponible
	}

	var res unsafe.Pointer
	if hr := call(dlg, mGetResult, uintptr(unsafe.Pointer(&res))); failed(hr) || res == nil {
		return "", false, ErrNoDisponible
	}
	defer call(res, mRelease)

	var name *uint16
	if hr := call(res, mGetDispName, sigdnFileSysPath, uintptr(unsafe.Pointer(&name))); failed(hr) || name == nil {
		return "", false, ErrNoDisponible
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(name))
	return windows.UTF16PtrToString(name), true, nil
}

// Escritorio devuelve la ruta real del Escritorio del usuario (también si está en OneDrive).
func Escritorio() string {
	p, err := windows.KnownFolderPath(windows.FOLDERID_Desktop, 0)
	if err != nil {
		return ""
	}
	return p
}
