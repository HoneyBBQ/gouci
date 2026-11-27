package gouci

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/honeybbq/gouci/internal/errs"
)

// CommitResult reports the outcome of a Store.Update transaction.
type CommitResult struct {
	// Changes lists the semantic differences committed to disk (empty when Changed is false).
	Changes []Change
	// Changed is true when the configuration on disk was updated.
	Changed bool
}

// Store provides safe, concurrent, transactional access to OpenWrt UCI files
// on disk using libuci-compatible flock ordering, Conf2Dir resolution, delta
// replay, and crash-safe atomic writes.
type Store struct {
	logger      *slog.Logger
	pkgLocks    map[string]*sync.RWMutex
	confDir     string
	conf2Dir    string
	deltaDir    string
	mu          sync.Mutex
	fileMode    fs.FileMode
	deltaPolicy DeltaPolicy
	readOnly    bool
}

type targetInfo struct {
	path   string
	dir    string
	mode   fs.FileMode
	exists bool
}

type lockedDelta struct {
	file    *os.File
	data    []byte
	hasText bool
}

// NewStore creates a Store with default OpenWrt directories (/etc/config,
// /var/run/uci, /tmp/.uci), DeltaMerge policy, and 0600 mode for new files.
func NewStore(opts ...Option) *Store {
	s := &Store{
		logger:      discardLogger,
		pkgLocks:    make(map[string]*sync.RWMutex),
		confDir:     DefaultConfDir,
		conf2Dir:    DefaultConf2Dir,
		deltaDir:    DefaultDeltaDir,
		fileMode:    DefaultFileMode,
		deltaPolicy: DeltaMerge,
	}
	for _, opt := range opts {
		if opt != nil {
			opt.apply(s)
		}
	}

	return s
}

// Packages lists available UCI package names in ConfDir in sorted order,
// skipping directories, filenames containing '.', and invalid package names
// (matching libuci uci_list_config_files).
func (s *Store) Packages() ([]string, error) {
	entries, err := os.ReadDir(s.confDir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.ContainsRune(name, '.') || !validatePackageName(name) {
			continue
		}
		if entry.IsDir() {
			continue
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			info, statErr := os.Stat(filepath.Join(s.confDir, name))
			if statErr != nil || !info.Mode().IsRegular() {
				continue
			}
		} else if !entry.Type().IsRegular() {
			continue
		}
		names = append(names, name)
	}

	return names, nil
}

// Load reads and parses pkg from Conf2Dir (if present) or ConfDir under a
// shared flock, applying or rejecting DeltaDir/<pkg> according to DeltaPolicy.
func (s *Store) Load(pkg string) (*Package, error) {
	if !validatePackageName(pkg) {
		return nil, errs.Wrapf(ErrInvalidName, "invalid package name %q", pkg)
	}
	rw := s.packageLock(pkg)
	rw.RLock()
	defer rw.RUnlock()

	raw, delta, targetPath, err := s.readLockedPair(context.Background(), pkg)
	if err != nil {
		return nil, err
	}
	if s.deltaPolicy == DeltaReject && delta.hasText {
		return nil, errs.Wrapf(ErrDirtyDelta, "package %q has uncommitted delta", pkg)
	}
	p, err := parsePackage(raw, targetPath, pkg, true)
	if err != nil {
		return nil, err
	}
	if s.deltaPolicy == DeltaMerge && delta.hasText {
		replayDelta(p, delta.data, s.logger)
	}

	return p, nil
}

// Update runs fn inside an exclusive per-package transaction: it loads the
// latest on-disk package (or an empty package when creating a new file),
// applies DeltaDir/<pkg> per DeltaPolicy, executes fn on a cloned working copy,
// computes semantic Diff against the on-disk baseline, and atomically commits
// only when net changes exist (protecting SPI-NAND flash on no-ops).
func (s *Store) Update(ctx context.Context, pkg string, fn func(*Package) error) (CommitResult, error) {
	if !validatePackageName(pkg) {
		return CommitResult{}, errs.Wrapf(ErrInvalidName, "invalid package name %q", pkg)
	}
	if s.readOnly {
		return CommitResult{}, errs.Wrapf(ErrReadOnly, "cannot update package %q", pkg)
	}
	err := ctx.Err()
	if err != nil {
		return CommitResult{}, err
	}

	rw := s.packageLock(pkg)
	rw.Lock()
	defer rw.Unlock()

	return s.updateLocked(ctx, pkg, fn)
}

// ExportRaw returns the exact raw bytes of pkg's resolved backing file under a
// shared flock, suitable for snapshotting before confirmed-apply changes.
func (s *Store) ExportRaw(pkg string) ([]byte, error) {
	if !validatePackageName(pkg) {
		return nil, errs.Wrapf(ErrInvalidName, "invalid package name %q", pkg)
	}
	rw := s.packageLock(pkg)
	rw.RLock()
	defer rw.RUnlock()

	info, err := s.resolveTarget(pkg)
	if err != nil {
		return nil, err
	}
	if !info.exists {
		return nil, errs.Wrapf(ErrNotFound, "package %q", pkg)
	}
	f, _, found, err := openLockedExisting(context.Background(), info.path, false)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errs.Wrapf(ErrNotFound, "package %q", pkg)
	}
	defer unlockAndClose(f)

	return io.ReadAll(f)
}

// RestoreRaw atomically restores raw bytes to pkg's resolved backing file under
// an exclusive flock after validating that raw parses as valid UCI syntax.
// Identical on-disk bytes skip rewriting; under DeltaMerge any stale delta file
// is truncated so old changes do not resurface after restore.
func (s *Store) RestoreRaw(ctx context.Context, pkg string, raw []byte) error {
	if !validatePackageName(pkg) {
		return errs.Wrapf(ErrInvalidName, "invalid package name %q", pkg)
	}
	if s.readOnly {
		return errs.Wrapf(ErrReadOnly, "cannot restore package %q", pkg)
	}
	err := ctx.Err()
	if err != nil {
		return err
	}
	_, err = parsePackage(raw, pkg, pkg, true)
	if err != nil {
		return err
	}

	rw := s.packageLock(pkg)
	rw.Lock()
	defer rw.Unlock()

	return s.restoreLocked(ctx, pkg, raw)
}

func (s *Store) packageLock(pkg string) *sync.RWMutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	rw, ok := s.pkgLocks[pkg]
	if !ok {
		rw = &sync.RWMutex{}
		s.pkgLocks[pkg] = rw
	}

	return rw
}

func (s *Store) readLockedPair(ctx context.Context, pkg string) ([]byte, lockedDelta, string, error) {
	info, err := s.resolveTarget(pkg)
	if err != nil {
		return nil, lockedDelta{}, "", err
	}
	if !info.exists {
		return nil, lockedDelta{}, "", errs.Wrapf(ErrNotFound, "package %q", pkg)
	}
	confFile, _, found, err := openLockedExisting(ctx, info.path, false)
	if err != nil {
		return nil, lockedDelta{}, "", err
	}
	if !found {
		return nil, lockedDelta{}, "", errs.Wrapf(ErrNotFound, "package %q", pkg)
	}
	defer unlockAndClose(confFile)

	raw, err := io.ReadAll(confFile)
	if err != nil {
		return nil, lockedDelta{}, "", err
	}
	delta, err := s.openLockedDelta(ctx, pkg, false)
	if err != nil {
		return nil, lockedDelta{}, "", err
	}
	delta.close()

	return raw, delta, info.path, nil
}

func (s *Store) updateLocked(ctx context.Context, pkg string, fn func(*Package) error) (CommitResult, error) {
	lockFile, info, existingRaw, err := s.acquireWriteLock(ctx, pkg)
	if err != nil {
		return CommitResult{}, err
	}
	defer unlockAndClose(lockFile)

	delta, err := s.openLockedDelta(ctx, pkg, s.deltaPolicy == DeltaMerge)
	if err != nil {
		return CommitResult{}, err
	}
	defer delta.close()

	if s.deltaPolicy == DeltaReject && delta.hasText {
		return CommitResult{}, errs.Wrapf(ErrDirtyDelta, "package %q has uncommitted delta", pkg)
	}

	basePkg, err := parseBaseline(existingRaw, info, pkg)
	if err != nil {
		return CommitResult{}, err
	}

	working, err := s.runUpdateCallback(ctx, basePkg, delta, fn)
	if err != nil {
		return CommitResult{}, err
	}

	return s.commitUpdate(info, pkg, basePkg, working, delta)
}

func (s *Store) runUpdateCallback(
	ctx context.Context,
	basePkg *Package,
	delta lockedDelta,
	fn func(*Package) error,
) (*Package, error) {
	working := basePkg.Clone()
	if s.deltaPolicy == DeltaMerge && delta.hasText {
		replayDelta(working, delta.data, s.logger)
	}
	err := fn(working)
	if err != nil {
		return nil, err
	}
	err = ctx.Err()
	if err != nil {
		return nil, err
	}

	return working, nil
}

func (s *Store) commitUpdate(
	info targetInfo,
	pkg string,
	basePkg, working *Package,
	delta lockedDelta,
) (CommitResult, error) {
	changes := Diff(basePkg, working)
	if len(changes) == 0 {
		err := flushDeltaIfNeeded(s.deltaPolicy, delta)
		if err != nil {
			return CommitResult{}, err
		}

		return CommitResult{Changed: false}, nil
	}

	out, err := working.Format()
	if err != nil {
		return CommitResult{}, err
	}
	err = atomicWriteFile(info.dir, info.path, pkg, out, info.mode)
	if err != nil {
		return CommitResult{}, err
	}
	err = flushDeltaIfNeeded(s.deltaPolicy, delta)
	if err != nil {
		return CommitResult{}, err
	}

	return CommitResult{Changed: true, Changes: changes}, nil
}

func (s *Store) restoreLocked(ctx context.Context, pkg string, raw []byte) error {
	lockFile, info, existingRaw, err := s.acquireWriteLock(ctx, pkg)
	if err != nil {
		return err
	}
	defer unlockAndClose(lockFile)

	delta, err := s.openLockedDelta(ctx, pkg, s.deltaPolicy == DeltaMerge)
	if err != nil {
		return err
	}
	defer delta.close()

	if s.deltaPolicy == DeltaReject && delta.hasText {
		return errs.Wrapf(ErrDirtyDelta, "package %q has uncommitted delta", pkg)
	}
	if info.exists && bytes.Equal(existingRaw, raw) {
		return flushDeltaIfNeeded(s.deltaPolicy, delta)
	}
	err = atomicWriteFile(info.dir, info.path, pkg, raw, info.mode)
	if err != nil {
		return err
	}

	return flushDeltaIfNeeded(s.deltaPolicy, delta)
}

// acquireWriteLock locks an existing backing file (verifying inode stability)
// or locks ConfDir when creating a new package without leaving a 0-byte file.
func (s *Store) acquireWriteLock(ctx context.Context, pkg string) (*os.File, targetInfo, []byte, error) {
	for {
		info, err := s.resolveTarget(pkg)
		if err != nil {
			return nil, targetInfo{}, nil, err
		}
		if info.exists {
			f, st, found, openErr := openLockedExisting(ctx, info.path, true)
			if openErr != nil {
				return nil, targetInfo{}, nil, openErr
			}
			if !found {
				continue
			}
			raw, readErr := io.ReadAll(f)
			if readErr != nil {
				unlockAndClose(f)

				return nil, targetInfo{}, nil, readErr
			}
			info.mode = st.Mode().Perm()

			return f, info, raw, nil
		}

		dirFile, retry, err := s.lockNewTargetDir(ctx, pkg, info.dir)
		if err != nil {
			return nil, targetInfo{}, nil, err
		}
		if retry {
			continue
		}
		info.mode = s.fileMode

		return dirFile, info, nil, nil
	}
}

func (s *Store) lockNewTargetDir(ctx context.Context, pkg, dir string) (*os.File, bool, error) {
	//nolint:gosec // G304: dir is the configured ConfDir.
	dirFile, err := os.Open(dir)
	if err != nil {
		return nil, false, err
	}
	err = flockFile(ctx, dirFile, true)
	if err != nil {
		_ = dirFile.Close()

		return nil, false, err
	}
	// Re-check whether a concurrent writer created the file before we acquired dir lock.
	recheck, err := s.resolveTarget(pkg)
	if err != nil {
		unlockAndClose(dirFile)

		return nil, false, err
	}
	if recheck.exists {
		unlockAndClose(dirFile)

		return nil, true, nil
	}

	return dirFile, false, nil
}

func (s *Store) resolveTarget(pkg string) (targetInfo, error) {
	if s.conf2Dir != "" {
		info, ok, err := resolveExisting(filepath.Join(s.conf2Dir, pkg))
		if err != nil {
			return targetInfo{}, err
		}
		if ok {
			return info, nil
		}
	}
	info, ok, err := resolveExisting(filepath.Join(s.confDir, pkg))
	if err != nil {
		return targetInfo{}, err
	}
	if ok {
		return info, nil
	}

	return targetInfo{
		path:   filepath.Join(s.confDir, pkg),
		dir:    s.confDir,
		mode:   s.fileMode,
		exists: false,
	}, nil
}

func resolveExisting(candidate string) (targetInfo, bool, error) {
	st, err := os.Stat(candidate)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return targetInfo{}, false, nil
		}

		return targetInfo{}, false, err
	}
	if !st.Mode().IsRegular() {
		return targetInfo{}, false, errs.Wrapf(ErrNotFound, "%s is not a regular file", candidate)
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return targetInfo{}, false, err
	}

	return targetInfo{
		path:   resolved,
		dir:    filepath.Dir(resolved),
		mode:   st.Mode().Perm(),
		exists: true,
	}, true, nil
}

func (s *Store) openLockedDelta(ctx context.Context, pkg string, exclusive bool) (lockedDelta, error) {
	if s.deltaDir == "" {
		return lockedDelta{}, nil
	}
	deltaPath := filepath.Join(s.deltaDir, pkg)
	f, _, found, err := openLockedExisting(ctx, deltaPath, exclusive)
	if err != nil {
		return lockedDelta{}, err
	}
	if !found {
		return lockedDelta{}, nil
	}
	data, err := io.ReadAll(f)
	if err != nil {
		unlockAndClose(f)

		return lockedDelta{}, err
	}

	return lockedDelta{
		file:    f,
		data:    data,
		hasText: len(bytes.TrimSpace(data)) > 0,
	}, nil
}

func (d *lockedDelta) close() {
	if d.file != nil {
		unlockAndClose(d.file)
		d.file = nil
	}
}

func flushDeltaIfNeeded(policy DeltaPolicy, delta lockedDelta) error {
	if policy != DeltaMerge || delta.file == nil || !delta.hasText {
		return nil
	}
	err := delta.file.Truncate(0)
	if err != nil {
		return err
	}

	return delta.file.Sync()
}

func parseBaseline(raw []byte, info targetInfo, pkg string) (*Package, error) {
	if !info.exists {
		return &Package{name: pkg}, nil
	}

	return parsePackage(raw, info.path, pkg, true)
}

func openLockedExisting(ctx context.Context, path string, exclusive bool) (*os.File, os.FileInfo, bool, error) {
	flags := os.O_RDONLY
	if exclusive {
		flags = os.O_RDWR
	}
	for {
		err := ctx.Err()
		if err != nil {
			return nil, nil, false, err
		}
		//nolint:gosec // G304: path is built from configured UCI directory and validated package name.
		f, err := os.OpenFile(path, flags, 0)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil, nil, false, nil
			}

			return nil, nil, false, err
		}
		err = flockFile(ctx, f, exclusive)
		if err != nil {
			_ = f.Close()

			return nil, nil, false, err
		}
		fStat, err1 := f.Stat()
		pStat, err2 := os.Stat(path)
		if err1 == nil && err2 == nil && os.SameFile(fStat, pStat) {
			return f, fStat, true, nil
		}
		unlockAndClose(f)
		if errors.Is(err2, fs.ErrNotExist) {
			return nil, nil, false, nil
		}
	}
}

func atomicWriteFile(dir, targetPath, pkg string, data []byte, mode fs.FileMode) error {
	tmp, err := os.CreateTemp(dir, "."+pkg+".uci-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	err = writeTempAndSync(tmp, data, mode)
	if err != nil {
		return err
	}
	err = os.Rename(tmpPath, targetPath)
	if err != nil {
		return err
	}
	cleanup = false

	return syncDir(dir)
}

func writeTempAndSync(tmp *os.File, data []byte, mode fs.FileMode) error {
	err := tmp.Chmod(mode)
	if err != nil {
		_ = tmp.Close()

		return err
	}
	_, err = tmp.Write(data)
	if err != nil {
		_ = tmp.Close()

		return err
	}
	err = tmp.Sync()
	if err != nil {
		_ = tmp.Close()

		return err
	}

	return tmp.Close()
}

func unlockAndClose(f *os.File) {
	if f == nil {
		return
	}
	funlockFile(f)
	_ = f.Close()
}
