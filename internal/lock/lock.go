package lock

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

type Lock struct{ f *os.File }

func Acquire(id string) (*Lock, error) {
	dirs := []string{"/run/lock/sbackup", filepath.Join(os.TempDir(), "sbackup-locks")}
	var f *os.File
	var err error
	for i, dir := range dirs {
		mode := os.FileMode(0755)
		if i > 0 {
			mode = 0700
		}
		if err = os.MkdirAll(dir, mode); err != nil {
			continue
		}
		f, err = os.OpenFile(filepath.Join(dir, id+".lock"), os.O_CREATE|os.O_RDWR, 0600)
		if err == nil {
			break
		}
	}
	if f == nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("任务已在运行")
	}
	return &Lock{f: f}, nil
}
func (l *Lock) Release() {
	if l != nil && l.f != nil {
		_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
		_ = l.f.Close()
	}
}
