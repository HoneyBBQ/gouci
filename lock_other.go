//go:build !unix

package gouci

import (
	"context"
	"os"
)

func flockFile(ctx context.Context, _ *os.File, _ bool) error {
	if ctx != nil {
		return ctx.Err()
	}

	return nil
}

func funlockFile(_ *os.File) {}

func syncDir(_ string) error {
	return nil
}
