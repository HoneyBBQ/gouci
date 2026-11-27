package gouci

import (
	"cmp"
	"io/fs"
	"log/slog"
)

const (
	// DefaultConfDir is the default primary OpenWrt UCI configuration directory.
	DefaultConfDir = "/etc/config"
	// DefaultConf2Dir is the default OpenWrt runtime override configuration directory.
	DefaultConf2Dir = "/var/run/uci"
	// DefaultDeltaDir is the default OpenWrt uncommitted delta directory.
	DefaultDeltaDir = "/tmp/.uci"
	// DefaultFileMode is the default permission mode for newly created UCI config files (matching UCI_FILEMODE).
	DefaultFileMode fs.FileMode = 0o600
)

const (
	// DeltaMerge replays uncommitted delta files on Load and merges and truncates
	// them on Update/RestoreRaw, matching libuci's default behavior.
	DeltaMerge DeltaPolicy = iota
	// DeltaReject fails with ErrDirtyDelta when a non-empty uncommitted delta
	// file exists for the target package.
	DeltaReject
)

var discardLogger = slog.New(slog.DiscardHandler)

// DeltaPolicy controls how Store handles uncommitted files in DeltaDir (/tmp/.uci).
type DeltaPolicy uint8

// Option configures a Store.
type Option interface {
	apply(store *Store)
}

type optionFunc func(*Store)

// String returns a human-readable name for p.
func (p DeltaPolicy) String() string {
	switch p {
	case DeltaMerge:
		return "merge"
	case DeltaReject:
		return "reject"
	default:
		return "unknown"
	}
}

// WithConfDir sets the primary UCI configuration directory (default "/etc/config").
func WithConfDir(dir string) Option {
	return optionFunc(func(store *Store) {
		store.confDir = dir
	})
}

// WithConf2Dir sets the runtime override configuration directory (default "/var/run/uci").
// Passing "" disables Conf2Dir lookup.
func WithConf2Dir(dir string) Option {
	return optionFunc(func(store *Store) {
		store.conf2Dir = dir
	})
}

// WithDeltaDir sets the uncommitted delta directory (default "/tmp/.uci").
// Passing "" disables delta lookup and flushing.
func WithDeltaDir(dir string) Option {
	return optionFunc(func(store *Store) {
		store.deltaDir = dir
	})
}

// WithDeltaPolicy sets how Store handles uncommitted delta files (default DeltaMerge).
func WithDeltaPolicy(policy DeltaPolicy) Option {
	return optionFunc(func(store *Store) {
		store.deltaPolicy = policy
	})
}

// WithFileMode sets the permission mode used when creating a new UCI config file
// (default 0600). Existing files preserve their current permission mode.
func WithFileMode(mode fs.FileMode) Option {
	return optionFunc(func(store *Store) {
		if mode != 0 {
			store.fileMode = mode.Perm()
		}
	})
}

// WithReadOnly configures whether Store rejects write operations (Update, RestoreRaw)
// with ErrReadOnly.
func WithReadOnly(readOnly bool) Option {
	return optionFunc(func(store *Store) {
		store.readOnly = readOnly
	})
}

// WithLogger sets the structured logger used for non-fatal warnings such as
// skipped malformed delta lines under DeltaMerge. By default logs are discarded.
func WithLogger(logger *slog.Logger) Option {
	return optionFunc(func(store *Store) {
		store.logger = cmp.Or(logger, discardLogger)
	})
}

func (f optionFunc) apply(store *Store) {
	f(store)
}
