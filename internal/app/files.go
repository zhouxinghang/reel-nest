package app

import (
	"fmt"
	"os"
	"syscall"
)

func fileIdentity(info os.FileInfo) string {
	stat := info.Sys().(*syscall.Stat_t)
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino)
}

func fileFingerprint(info os.FileInfo) string {
	return fmt.Sprintf("%s:%d:%d", fileIdentity(info), info.Size(), info.ModTime().UnixNano())
}
