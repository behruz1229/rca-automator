package main

import (
	"syscall"
	"unsafe"
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procCreateMutexW = kernel32.NewProc("CreateMutexW")
	procCloseHandle  = kernel32.NewProc("CloseHandle")

	user32          = syscall.NewLazyDLL("user32.dll")
	procMessageBoxW = user32.NewProc("MessageBoxW")
)

// singleInstance удерживает системный mutex, пока программа работает
type singleInstance struct {
	handle syscall.Handle
}

// acquireSingleInstance возвращает nil, если программа УЖЕ запущена
func acquireSingleInstance(name string) *singleInstance {
	namePtr, _ := syscall.UTF16PtrFromString(name)
	handle, _, errno := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(namePtr)))
	if handle == 0 {
		// Не удалось создать mutex — не блокируем запуск
		return &singleInstance{}
	}

	if errno == syscall.ERROR_ALREADY_EXISTS {
		procCloseHandle.Call(handle)
		return nil
	}

	return &singleInstance{handle: syscall.Handle(handle)}
}

// Иконки для окон сообщений (Win32)
const (
	mbIconInfo    = 0x00000040
	mbIconWarning = 0x00000030
	mbIconError   = 0x00000010
)

// msgBox показывает нативное окно сообщения Windows
func msgBox(title, text string, icon uint32) {
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	textPtr, _ := syscall.UTF16PtrFromString(text)
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(textPtr)), uintptr(unsafe.Pointer(titlePtr)), uintptr(icon))
}
