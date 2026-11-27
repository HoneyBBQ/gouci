//go:build unix

package gouci

import (
	"context"
	"errors"
	"math"
	"os"
	"syscall"
	"time"
)

const (
	flockInitialBackoff = time.Millisecond
	flockMaxBackoff     = 25 * time.Millisecond
)

func flockFile(ctx context.Context, file *os.File, exclusive bool) error {
	how := syscall.LOCK_SH
	if exclusive {
		how = syscall.LOCK_EX
	}
	if ctx == nil || ctx.Done() == nil {
		return flockBlocking(file, how)
	}

	return flockWithContext(ctx, file, how)
}

func flockBlocking(file *os.File, how int) error {
	for {
		err := callFlock(file, how)
		if err == nil || errors.Is(err, syscall.ENOSYS) {
			return nil
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}

		return err
	}
}

func flockWithContext(ctx context.Context, file *os.File, how int) error {
	backoff := flockInitialBackoff
	for {
		err := ctx.Err()
		if err != nil {
			return err
		}
		err = callFlock(file, how|syscall.LOCK_NB)
		if err == nil || errors.Is(err, syscall.ENOSYS) {
			return nil
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return err
		}
		err = sleepContext(ctx, backoff)
		if err != nil {
			return err
		}
		backoff = min(backoff*2, flockMaxBackoff)
	}
}

func callFlock(file *os.File, how int) error {
	rawConn, err := file.SyscallConn()
	if err != nil {
		return err
	}
	var flockErr error
	ctrlErr := rawConn.Control(func(fd uintptr) {
		if fd > math.MaxInt {
			flockErr = syscall.EBADF

			return
		}
		flockErr = syscall.Flock(int(fd), how)
	})
	if ctrlErr != nil {
		return ctrlErr
	}

	return flockErr
}

func funlockFile(file *os.File) {
	if file == nil {
		return
	}
	_ = callFlock(file, syscall.LOCK_UN)
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func syncDir(dir string) error {
	//nolint:gosec // G304: dir is a configured or symlink-resolved UCI directory.
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	syncErr := d.Sync()
	closeErr := d.Close()
	if syncErr != nil && !errors.Is(syncErr, syscall.EINVAL) && !errors.Is(syncErr, syscall.EROFS) {
		return syncErr
	}

	return closeErr
}
