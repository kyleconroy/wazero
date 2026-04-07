package wasip3

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// preopenFS abstracts filesystem operations for a preopen directory.
// This allows preopens to be backed by host paths, fs.FS, or os.Root.
type preopenFS interface {
	// Lstat returns file info without following symlinks.
	Lstat(name string) (fs.FileInfo, error)
	// Stat returns file info, following symlinks.
	Stat(name string) (fs.FileInfo, error)
	// ReadDir returns the directory entries for the named directory.
	ReadDir(name string) ([]fs.DirEntry, error)
	// Open opens the named file for reading.
	Open(name string) (fs.File, error)
	// OpenFile opens the named file with the specified flags and permissions.
	// Returns an error for read-only implementations.
	OpenFile(name string, flag int, perm fs.FileMode) (*os.File, error)
	// Mkdir creates a directory. Returns an error for read-only implementations.
	Mkdir(name string, perm fs.FileMode) error
	// Remove removes a file or empty directory.
	Remove(name string) error
	// Rename renames oldname to newname within this filesystem.
	Rename(oldname, newname string) error
	// Link creates a hard link from oldname to newname.
	Link(oldname, newname string) error
	// Symlink creates a symbolic link from oldname to newname.
	Symlink(oldname, newname string) error
	// Truncate changes the size of the named file.
	Truncate(name string, size int64) error
	// Chtimes changes the access and modification times of the named file.
	Chtimes(name string, atime, mtime time.Time) error
	// EvalSymlinks resolves all symlinks in the path.
	EvalSymlinks(name string) (string, error)
	// DisplayPath returns a stable path for display and hashing.
	DisplayPath(name string) string
}

// resolve joins a preopen-relative name to an absolute path, handling "." as root.
func resolve(root, name string) string {
	if name == "." || name == "" {
		return root
	}
	return filepath.Join(root, name)
}

// hostPathFS implements preopenFS backed by a host directory path.
// All operations delegate to os.* functions with joined paths.
type hostPathFS struct {
	root string
}

func (f *hostPathFS) Lstat(name string) (fs.FileInfo, error) {
	return os.Lstat(resolve(f.root, name))
}

func (f *hostPathFS) Stat(name string) (fs.FileInfo, error) {
	return os.Stat(resolve(f.root, name))
}

func (f *hostPathFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return os.ReadDir(resolve(f.root, name))
}

func (f *hostPathFS) Open(name string) (fs.File, error) {
	return os.Open(resolve(f.root, name))
}

func (f *hostPathFS) OpenFile(name string, flag int, perm fs.FileMode) (*os.File, error) {
	return os.OpenFile(resolve(f.root, name), flag, perm)
}

func (f *hostPathFS) Mkdir(name string, perm fs.FileMode) error {
	return os.Mkdir(resolve(f.root, name), perm)
}

func (f *hostPathFS) Remove(name string) error {
	return os.Remove(resolve(f.root, name))
}

func (f *hostPathFS) Rename(oldname, newname string) error {
	return os.Rename(resolve(f.root, oldname), resolve(f.root, newname))
}

func (f *hostPathFS) Link(oldname, newname string) error {
	return os.Link(resolve(f.root, oldname), resolve(f.root, newname))
}

func (f *hostPathFS) Symlink(oldname, newname string) error {
	return os.Symlink(oldname, resolve(f.root, newname))
}

func (f *hostPathFS) Truncate(name string, size int64) error {
	return os.Truncate(resolve(f.root, name), size)
}

func (f *hostPathFS) Chtimes(name string, atime, mtime time.Time) error {
	return os.Chtimes(resolve(f.root, name), atime, mtime)
}

func (f *hostPathFS) EvalSymlinks(name string) (string, error) {
	full, err := filepath.EvalSymlinks(resolve(f.root, name))
	if err != nil {
		return name, err
	}
	// Return relative to root if possible.
	rel, err := filepath.Rel(f.root, full)
	if err != nil {
		return name, nil
	}
	return rel, nil
}

func (f *hostPathFS) DisplayPath(name string) string {
	return resolve(f.root, name)
}

// errReadOnly is returned by mutating operations on read-only filesystems.
var errReadOnly = errors.New("read-only filesystem")

// goFS implements preopenFS backed by a Go fs.FS (read-only).
type goFS struct {
	fsys fs.FS
	name string // display name for the filesystem
}

func (f *goFS) Lstat(name string) (fs.FileInfo, error) {
	// fs.FS has no Lstat; fall back to Stat.
	return fs.Stat(f.fsys, f.normalize(name))
}

func (f *goFS) Stat(name string) (fs.FileInfo, error) {
	return fs.Stat(f.fsys, f.normalize(name))
}

func (f *goFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return fs.ReadDir(f.fsys, f.normalize(name))
}

func (f *goFS) Open(name string) (fs.File, error) {
	return f.fsys.Open(f.normalize(name))
}

func (f *goFS) OpenFile(string, int, fs.FileMode) (*os.File, error) {
	return nil, errReadOnly
}

func (f *goFS) Mkdir(string, fs.FileMode) error { return errReadOnly }
func (f *goFS) Remove(string) error             { return errReadOnly }
func (f *goFS) Rename(string, string) error     { return errReadOnly }
func (f *goFS) Link(string, string) error       { return errReadOnly }
func (f *goFS) Symlink(string, string) error    { return errReadOnly }
func (f *goFS) Truncate(string, int64) error    { return errReadOnly }
func (f *goFS) Chtimes(string, time.Time, time.Time) error {
	return errReadOnly
}

func (f *goFS) EvalSymlinks(name string) (string, error) {
	return name, nil // no symlink resolution available
}

func (f *goFS) DisplayPath(name string) string {
	if name == "." || name == "" {
		return f.name
	}
	return f.name + "/" + name
}

// normalize converts a path for use with fs.FS (forward slash, "." for root).
func (f *goFS) normalize(name string) string {
	if name == "" {
		return "."
	}
	return name
}

// rootFS implements preopenFS backed by an os.Root (Go 1.24+).
// Operations not available on os.Root return appropriate errors.
type rootFS struct {
	root *os.Root
}

func (f *rootFS) Lstat(name string) (fs.FileInfo, error) {
	return f.root.Lstat(f.normalize(name))
}

func (f *rootFS) Stat(name string) (fs.FileInfo, error) {
	return f.root.Stat(f.normalize(name))
}

func (f *rootFS) ReadDir(name string) ([]fs.DirEntry, error) {
	file, err := f.root.Open(f.normalize(name))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return file.ReadDir(-1)
}

func (f *rootFS) Open(name string) (fs.File, error) {
	return f.root.Open(f.normalize(name))
}

func (f *rootFS) OpenFile(name string, flag int, perm fs.FileMode) (*os.File, error) {
	return f.root.OpenFile(f.normalize(name), flag, perm)
}

func (f *rootFS) Mkdir(name string, perm fs.FileMode) error {
	return f.root.Mkdir(f.normalize(name), perm)
}

func (f *rootFS) Remove(name string) error {
	return f.root.Remove(f.normalize(name))
}

var errUnsupported = errors.New("operation not supported")

func (f *rootFS) Rename(string, string) error  { return errUnsupported }
func (f *rootFS) Link(string, string) error    { return errUnsupported }
func (f *rootFS) Symlink(string, string) error { return errUnsupported }

func (f *rootFS) Truncate(name string, size int64) error {
	file, err := f.root.OpenFile(f.normalize(name), os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Truncate(size)
}

func (f *rootFS) Chtimes(string, time.Time, time.Time) error {
	return errUnsupported
}

func (f *rootFS) EvalSymlinks(name string) (string, error) {
	return name, nil // os.Root handles symlinks internally
}

func (f *rootFS) DisplayPath(name string) string {
	if name == "." || name == "" {
		return f.root.Name()
	}
	return filepath.Join(f.root.Name(), name)
}

func (f *rootFS) normalize(name string) string {
	if name == "" {
		return "."
	}
	return name
}

// seekOrSkip seeks to the given offset if the file supports it,
// otherwise reads and discards bytes.
func seekOrSkip(f fs.File, offset int64) {
	if offset <= 0 {
		return
	}
	if seeker, ok := f.(io.Seeker); ok {
		_, _ = seeker.Seek(offset, io.SeekStart)
		return
	}
	_, _ = io.CopyN(io.Discard, f, offset)
}
