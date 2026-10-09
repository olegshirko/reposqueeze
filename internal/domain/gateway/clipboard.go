package gateway

import (
	"context"
	"errors"
	"time"
)

// Clipboard packs the whole system clipboard (text, images, files) into an
// opaque payload and restores it.
type Clipboard interface {
	// Export returns the packed clipboard and a short human summary.
	Export() (payload []byte, summary string, err error)
	// Import replaces the clipboard with a packed payload.
	Import(payload []byte) (summary string, err error)
	// WatchDoubleCopy calls onDouble every time the same content is copied
	// twice within window (⌘C pressed twice), until ctx is done.
	WatchDoubleCopy(ctx context.Context, window time.Duration, onDouble func()) error
}

// PackageFile is one file stored in a GitLab generic package.
type PackageFile struct {
	ID        int    `json:"id"`
	PackageID int    `json:"package_id"`
	FileName  string `json:"file_name"`
	CreatedAt string `json:"created_at"`
}

// GenericPackages stores files in GitLab's generic package registry. Unlike
// repository commits it keeps no history, and old files can be deleted.
type GenericPackages interface {
	UploadPackageFile(projectID int, pkg, version, file string, data []byte) error
	DownloadPackageFile(projectID int, pkg, version, file string) ([]byte, error)
	// ListPackageFiles returns the files of a package version, oldest first
	// (nil when the package does not exist).
	ListPackageFiles(projectID int, pkg, version string) ([]PackageFile, error)
	DeletePackageFile(projectID, packageID, fileID int) error
}

// ErrNoClip means no clipboard has been pushed yet.
var ErrNoClip = errors.New("nothing pushed yet")

// ClipStore keeps the single, latest encrypted clipboard.
type ClipStore interface {
	// Put replaces the stored clipboard.
	Put(ctx context.Context, blob, meta []byte) error
	// Get returns the stored clipboard, or ErrNoClip.
	Get(ctx context.Context) (blob, meta []byte, err error)
	// Version is a cheap token that changes with every Put ("" if empty).
	Version(ctx context.Context) (string, error)
}
