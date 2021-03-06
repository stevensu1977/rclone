// Package fs is a generic file system interface for rclone object storage systems
package fs

import (
	"io"
	"log"
	"math"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/pkg/errors"
)

// Constants
const (
	// ModTimeNotSupported is a very large precision value to show
	// mod time isn't supported on this Fs
	ModTimeNotSupported = 100 * 365 * 24 * time.Hour
	// MaxLevel is a sentinel representing an infinite depth for listings
	MaxLevel = math.MaxInt32
)

// Globals
var (
	// UserAgent set in the default Transport
	UserAgent = "rclone/" + Version
	// Filesystem registry
	fsRegistry []*RegInfo
	// ErrorNotFoundInConfigFile is returned by NewFs if not found in config file
	ErrorNotFoundInConfigFile        = errors.New("didn't find section in config file")
	ErrorCantPurge                   = errors.New("can't purge directory")
	ErrorCantCopy                    = errors.New("can't copy object - incompatible remotes")
	ErrorCantMove                    = errors.New("can't move object - incompatible remotes")
	ErrorCantDirMove                 = errors.New("can't move directory - incompatible remotes")
	ErrorDirExists                   = errors.New("can't copy directory - destination already exists")
	ErrorCantSetModTime              = errors.New("can't set modified time")
	ErrorCantSetModTimeWithoutDelete = errors.New("can't set modified time without deleting existing object")
	ErrorDirNotFound                 = errors.New("directory not found")
	ErrorObjectNotFound              = errors.New("object not found")
	ErrorLevelNotSupported           = errors.New("level value not supported")
	ErrorListAborted                 = errors.New("list aborted")
	ErrorListBucketRequired          = errors.New("bucket or container name is needed in remote")
	ErrorIsFile                      = errors.New("is a file not a directory")
	ErrorNotAFile                    = errors.New("is a not a regular file")
	ErrorNotDeleting                 = errors.New("not deleting files as there were IO errors")
	ErrorCantMoveOverlapping         = errors.New("can't move files on overlapping remotes")
)

// RegInfo provides information about a filesystem
type RegInfo struct {
	// Name of this fs
	Name string
	// Description of this fs - defaults to Name
	Description string
	// Create a new file system.  If root refers to an existing
	// object, then it should return a Fs which which points to
	// the parent of that object and ErrorIsFile.
	NewFs func(name string, root string) (Fs, error)
	// Function to call to help with config
	Config func(string)
	// Options for the Fs configuration
	Options []Option
}

// Option is describes an option for the config wizard
type Option struct {
	Name       string
	Help       string
	Optional   bool
	IsPassword bool
	Examples   OptionExamples
}

// OptionExamples is a slice of examples
type OptionExamples []OptionExample

// Len is part of sort.Interface.
func (os OptionExamples) Len() int { return len(os) }

// Swap is part of sort.Interface.
func (os OptionExamples) Swap(i, j int) { os[i], os[j] = os[j], os[i] }

// Less is part of sort.Interface.
func (os OptionExamples) Less(i, j int) bool { return os[i].Help < os[j].Help }

// Sort sorts an OptionExamples
func (os OptionExamples) Sort() { sort.Sort(os) }

// OptionExample describes an example for an Option
type OptionExample struct {
	Value string
	Help  string
}

// Register a filesystem
//
// Fs modules  should use this in an init() function
func Register(info *RegInfo) {
	fsRegistry = append(fsRegistry, info)
}

// ListFser is the interface for listing a remote Fs
type ListFser interface {
	// List the objects and directories in dir into entries.  The
	// entries can be returned in any order but should be for a
	// complete directory.
	//
	// dir should be "" to list the root, and should not have
	// trailing slashes.
	//
	// This should return ErrDirNotFound if the directory isn't
	// found.
	List(dir string) (entries DirEntries, err error)

	// NewObject finds the Object at remote.  If it can't be found
	// it returns the error ErrorObjectNotFound.
	NewObject(remote string) (Object, error)
}

// Fs is the interface a cloud storage system must provide
type Fs interface {
	Info
	ListFser

	// Put in to the remote path with the modTime given of the given size
	//
	// May create the object even if it returns an error - if so
	// will return the object and the error, otherwise will return
	// nil and the error
	Put(in io.Reader, src ObjectInfo, options ...OpenOption) (Object, error)

	// Mkdir makes the directory (container, bucket)
	//
	// Shouldn't return an error if it already exists
	Mkdir(dir string) error

	// Rmdir removes the directory (container, bucket) if empty
	//
	// Return an error if it doesn't exist or isn't empty
	Rmdir(dir string) error
}

// Info provides an interface to reading information about a filesystem.
type Info interface {
	// Name of the remote (as passed into NewFs)
	Name() string

	// Root of the remote (as passed into NewFs)
	Root() string

	// String returns a description of the FS
	String() string

	// Precision of the ModTimes in this Fs
	Precision() time.Duration

	// Returns the supported hash types of the filesystem
	Hashes() HashSet

	// Features returns the optional features of this Fs
	Features() *Features
}

// Object is a filesystem like object provided by an Fs
type Object interface {
	ObjectInfo

	// SetModTime sets the metadata on the object to set the modification date
	SetModTime(time.Time) error

	// Open opens the file for read.  Call Close() on the returned io.ReadCloser
	Open(options ...OpenOption) (io.ReadCloser, error)

	// Update in to the object with the modTime given of the given size
	Update(in io.Reader, src ObjectInfo, options ...OpenOption) error

	// Removes this object
	Remove() error
}

// ObjectInfo contains information about an object.
type ObjectInfo interface {
	BasicInfo

	// Fs returns read only access to the Fs that this object is part of
	Fs() Info

	// Hash returns the selected checksum of the file
	// If no checksum is available it returns ""
	Hash(HashType) (string, error)

	// Storable says whether this object can be stored
	Storable() bool
}

// BasicInfo common interface for Dir and Object providing the very
// basic attributes of an object.
type BasicInfo interface {
	// String returns a description of the Object
	String() string

	// Remote returns the remote path
	Remote() string

	// ModTime returns the modification date of the file
	// It should return a best guess if one isn't available
	ModTime() time.Time

	// Size returns the size of the file
	Size() int64
}

// MimeTyper is an optional interface for Object
type MimeTyper interface {
	// MimeType returns the content type of the Object if
	// known, or "" if not
	MimeType() string
}

// ListRCallback defines a callback function for ListR to use
//
// It is called for each tranche of entries read from the listing and
// if it returns an error, the listing stops.
type ListRCallback func(entries DirEntries) error

// ListRFn is defines the call used to recursively list a directory
type ListRFn func(dir string, callback ListRCallback) error

// Features describe the optional features of the Fs
type Features struct {
	// Feature flags
	CaseInsensitive bool
	DuplicateFiles  bool
	ReadMimeType    bool
	WriteMimeType   bool

	// Purge all files in the root and the root directory
	//
	// Implement this if you have a way of deleting all the files
	// quicker than just running Remove() on the result of List()
	//
	// Return an error if it doesn't exist
	Purge func() error

	// Copy src to this remote using server side copy operations.
	//
	// This is stored with the remote path given
	//
	// It returns the destination Object and a possible error
	//
	// Will only be called if src.Fs().Name() == f.Name()
	//
	// If it isn't possible then return fs.ErrorCantCopy
	Copy func(src Object, remote string) (Object, error)

	// Move src to this remote using server side move operations.
	//
	// This is stored with the remote path given
	//
	// It returns the destination Object and a possible error
	//
	// Will only be called if src.Fs().Name() == f.Name()
	//
	// If it isn't possible then return fs.ErrorCantMove
	Move func(src Object, remote string) (Object, error)

	// DirMove moves src, srcRemote to this remote at dstRemote
	// using server side move operations.
	//
	// Will only be called if src.Fs().Name() == f.Name()
	//
	// If it isn't possible then return fs.ErrorCantDirMove
	//
	// If destination exists then return fs.ErrorDirExists
	DirMove func(src Fs, srcRemote, dstRemote string) error

	// DirChangeNotify calls the passed function with a path
	// of a directory that has had changes. If the implementation
	// uses polling, it should adhere to the given interval.
	DirChangeNotify func(func(string), time.Duration) chan bool

	// UnWrap returns the Fs that this Fs is wrapping
	UnWrap func() Fs

	// DirCacheFlush resets the directory cache - used in testing
	// as an optional interface
	DirCacheFlush func()

	// Put in to the remote path with the modTime given of the given size
	//
	// May create the object even if it returns an error - if so
	// will return the object and the error, otherwise will return
	// nil and the error
	//
	// May create duplicates or return errors if src already
	// exists.
	PutUnchecked func(in io.Reader, src ObjectInfo, options ...OpenOption) (Object, error)

	// CleanUp the trash in the Fs
	//
	// Implement this if you have a way of emptying the trash or
	// otherwise cleaning up old versions of files.
	CleanUp func() error

	// ListR lists the objects and directories of the Fs starting
	// from dir recursively into out.
	//
	// dir should be "" to start from the root, and should not
	// have trailing slashes.
	//
	// This should return ErrDirNotFound if the directory isn't
	// found.
	//
	// It should call callback for each tranche of entries read.
	// These need not be returned in any particular order.  If
	// callback returns an error then the listing will stop
	// immediately.
	//
	// Don't implement this unless you have a more efficient way
	// of listing recursively that doing a directory traversal.
	ListR ListRFn
}

// Fill fills in the function pointers in the Features struct from the
// optional interfaces.  It returns the original updated Features
// struct passed in.
func (ft *Features) Fill(f Fs) *Features {
	if do, ok := f.(Purger); ok {
		ft.Purge = do.Purge
	}
	if do, ok := f.(Copier); ok {
		ft.Copy = do.Copy
	}
	if do, ok := f.(Mover); ok {
		ft.Move = do.Move
	}
	if do, ok := f.(DirMover); ok {
		ft.DirMove = do.DirMove
	}
	if do, ok := f.(DirChangeNotifier); ok {
		ft.DirChangeNotify = do.DirChangeNotify
	}
	if do, ok := f.(UnWrapper); ok {
		ft.UnWrap = do.UnWrap
	}
	if do, ok := f.(DirCacheFlusher); ok {
		ft.DirCacheFlush = do.DirCacheFlush
	}
	if do, ok := f.(PutUncheckeder); ok {
		ft.PutUnchecked = do.PutUnchecked
	}
	if do, ok := f.(CleanUpper); ok {
		ft.CleanUp = do.CleanUp
	}
	if do, ok := f.(ListRer); ok {
		ft.ListR = do.ListR
	}
	return ft
}

// Mask the Features with the Fs passed in
//
// Only optional features which are implemented in both the original
// Fs AND the one passed in will be advertised.  Any features which
// aren't in both will be set to false/nil, except for UnWrap which
// will be left untouched.
func (ft *Features) Mask(f Fs) *Features {
	mask := f.Features()
	ft.CaseInsensitive = ft.CaseInsensitive && mask.CaseInsensitive
	ft.DuplicateFiles = ft.DuplicateFiles && mask.DuplicateFiles
	ft.ReadMimeType = ft.ReadMimeType && mask.ReadMimeType
	ft.WriteMimeType = ft.WriteMimeType && mask.WriteMimeType
	if mask.Purge == nil {
		ft.Purge = nil
	}
	if mask.Copy == nil {
		ft.Copy = nil
	}
	if mask.Move == nil {
		ft.Move = nil
	}
	if mask.DirMove == nil {
		ft.DirMove = nil
	}
	if mask.DirChangeNotify == nil {
		ft.DirChangeNotify = nil
	}
	// if mask.UnWrap == nil {
	// 	ft.UnWrap = nil
	// }
	if mask.DirCacheFlush == nil {
		ft.DirCacheFlush = nil
	}
	if mask.PutUnchecked == nil {
		ft.PutUnchecked = nil
	}
	if mask.CleanUp == nil {
		ft.CleanUp = nil
	}
	if mask.ListR == nil {
		ft.ListR = nil
	}
	return ft
}

// Wrap makes a Copy of the features passed in, overriding the UnWrap
// method only if available in f.
func (ft *Features) Wrap(f Fs) *Features {
	copy := new(Features)
	*copy = *ft
	if do, ok := f.(UnWrapper); ok {
		copy.UnWrap = do.UnWrap
	}
	return copy
}

// Purger is an optional interfaces for Fs
type Purger interface {
	// Purge all files in the root and the root directory
	//
	// Implement this if you have a way of deleting all the files
	// quicker than just running Remove() on the result of List()
	//
	// Return an error if it doesn't exist
	Purge() error
}

// Copier is an optional interface for Fs
type Copier interface {
	// Copy src to this remote using server side copy operations.
	//
	// This is stored with the remote path given
	//
	// It returns the destination Object and a possible error
	//
	// Will only be called if src.Fs().Name() == f.Name()
	//
	// If it isn't possible then return fs.ErrorCantCopy
	Copy(src Object, remote string) (Object, error)
}

// Mover is an optional interface for Fs
type Mover interface {
	// Move src to this remote using server side move operations.
	//
	// This is stored with the remote path given
	//
	// It returns the destination Object and a possible error
	//
	// Will only be called if src.Fs().Name() == f.Name()
	//
	// If it isn't possible then return fs.ErrorCantMove
	Move(src Object, remote string) (Object, error)
}

// DirMover is an optional interface for Fs
type DirMover interface {
	// DirMove moves src, srcRemote to this remote at dstRemote
	// using server side move operations.
	//
	// Will only be called if src.Fs().Name() == f.Name()
	//
	// If it isn't possible then return fs.ErrorCantDirMove
	//
	// If destination exists then return fs.ErrorDirExists
	DirMove(src Fs, srcRemote, dstRemote string) error
}

// DirChangeNotifier is an optional interface for Fs
type DirChangeNotifier interface {
	// DirChangeNotify calls the passed function with a path
	// of a directory that has had changes. If the implementation
	// uses polling, it should adhere to the given interval.
	DirChangeNotify(func(string), time.Duration) chan bool
}

// UnWrapper is an optional interfaces for Fs
type UnWrapper interface {
	// UnWrap returns the Fs that this Fs is wrapping
	UnWrap() Fs
}

// DirCacheFlusher is an optional interface for Fs
type DirCacheFlusher interface {
	// DirCacheFlush resets the directory cache - used in testing
	// as an optional interface
	DirCacheFlush()
}

// PutUncheckeder is an optional interface for Fs
type PutUncheckeder interface {
	// Put in to the remote path with the modTime given of the given size
	//
	// May create the object even if it returns an error - if so
	// will return the object and the error, otherwise will return
	// nil and the error
	//
	// May create duplicates or return errors if src already
	// exists.
	PutUnchecked(in io.Reader, src ObjectInfo, options ...OpenOption) (Object, error)
}

// CleanUpper is an optional interfaces for Fs
type CleanUpper interface {
	// CleanUp the trash in the Fs
	//
	// Implement this if you have a way of emptying the trash or
	// otherwise cleaning up old versions of files.
	CleanUp() error
}

// ListRer is an optional interfaces for Fs
type ListRer interface {
	// ListR lists the objects and directories of the Fs starting
	// from dir recursively into out.
	//
	// dir should be "" to start from the root, and should not
	// have trailing slashes.
	//
	// This should return ErrDirNotFound if the directory isn't
	// found.
	//
	// It should call callback for each tranche of entries read.
	// These need not be returned in any particular order.  If
	// callback returns an error then the listing will stop
	// immediately.
	//
	// Don't implement this unless you have a more efficient way
	// of listing recursively that doing a directory traversal.
	ListR(dir string, callback ListRCallback) error
}

// ObjectsChan is a channel of Objects
type ObjectsChan chan Object

// Objects is a slice of Object~s
type Objects []Object

// ObjectPair is a pair of Objects used to describe a potential copy
// operation.
type ObjectPair struct {
	src, dst Object
}

// ObjectPairChan is a channel of ObjectPair
type ObjectPairChan chan ObjectPair

// Dir describes a directory for directory/container/bucket lists
type Dir struct {
	Name  string    // name of the directory
	When  time.Time // modification or creation time - IsZero for unknown
	Bytes int64     // size of directory and contents -1 for unknown
	Count int64     // number of objects -1 for unknown
}

// String returns the name
func (d *Dir) String() string {
	return d.Name
}

// Remote returns the remote path
func (d *Dir) Remote() string {
	return d.Name
}

// ModTime returns the modification date of the file
// It should return a best guess if one isn't available
func (d *Dir) ModTime() time.Time {
	if !d.When.IsZero() {
		return d.When
	}
	return time.Now()
}

// Size returns the size of the file
func (d *Dir) Size() int64 {
	return d.Bytes
}

// Check interface
var _ BasicInfo = (*Dir)(nil)

// DirChan is a channel of Dir objects
type DirChan chan *Dir

// Find looks for an Info object for the name passed in
//
// Services are looked up in the config file
func Find(name string) (*RegInfo, error) {
	for _, item := range fsRegistry {
		if item.Name == name {
			return item, nil
		}
	}
	return nil, errors.Errorf("didn't find filing system for %q", name)
}

// MustFind looks for an Info object for the type name passed in
//
// Services are looked up in the config file
//
// Exits with a fatal error if not found
func MustFind(name string) *RegInfo {
	fs, err := Find(name)
	if err != nil {
		log.Fatalf("Failed to find remote: %v", err)
	}
	return fs
}

// Pattern to match an rclone url
var matcher = regexp.MustCompile(`^([\w_ -]+):(.*)$`)

// ParseRemote deconstructs a path into configName, fsPath, looking up
// the fsName in the config file (returning NotFoundInConfigFile if not found)
func ParseRemote(path string) (fsInfo *RegInfo, configName, fsPath string, err error) {
	parts := matcher.FindStringSubmatch(path)
	var fsName string
	fsName, configName, fsPath = "local", "local", path
	if parts != nil && !isDriveLetter(parts[1]) {
		configName, fsPath = parts[1], parts[2]
		fsName = ConfigFileGet(configName, "type")
		if fsName == "" {
			return nil, "", "", ErrorNotFoundInConfigFile
		}
	}
	// change native directory separators to / if there are any
	fsPath = filepath.ToSlash(fsPath)
	fsInfo, err = Find(fsName)
	return fsInfo, configName, fsPath, err
}

// NewFs makes a new Fs object from the path
//
// The path is of the form remote:path
//
// Remotes are looked up in the config file.  If the remote isn't
// found then NotFoundInConfigFile will be returned.
//
// On Windows avoid single character remote names as they can be mixed
// up with drive letters.
func NewFs(path string) (Fs, error) {
	fsInfo, configName, fsPath, err := ParseRemote(path)
	if err != nil {
		return nil, err
	}
	return fsInfo.NewFs(configName, fsPath)
}

// CheckClose is a utility function used to check the return from
// Close in a defer statement.
func CheckClose(c io.Closer, err *error) {
	cerr := c.Close()
	if *err == nil {
		*err = cerr
	}
}

// NewStaticObjectInfo returns a static ObjectInfo
// If hashes is nil and fs is not nil, the hash map will be replaced with
// empty hashes of the types supported by the fs.
func NewStaticObjectInfo(remote string, modTime time.Time, size int64, storable bool, hashes map[HashType]string, fs Info) ObjectInfo {
	info := &staticObjectInfo{
		remote:   remote,
		modTime:  modTime,
		size:     size,
		storable: storable,
		hashes:   hashes,
		fs:       fs,
	}
	if fs != nil && hashes == nil {
		set := fs.Hashes().Array()
		info.hashes = make(map[HashType]string)
		for _, ht := range set {
			info.hashes[ht] = ""
		}
	}
	return info
}

type staticObjectInfo struct {
	remote   string
	modTime  time.Time
	size     int64
	storable bool
	hashes   map[HashType]string
	fs       Info
}

func (i *staticObjectInfo) Fs() Info           { return i.fs }
func (i *staticObjectInfo) Remote() string     { return i.remote }
func (i *staticObjectInfo) String() string     { return i.remote }
func (i *staticObjectInfo) ModTime() time.Time { return i.modTime }
func (i *staticObjectInfo) Size() int64        { return i.size }
func (i *staticObjectInfo) Storable() bool     { return i.storable }
func (i *staticObjectInfo) Hash(h HashType) (string, error) {
	if len(i.hashes) == 0 {
		return "", ErrHashUnsupported
	}
	if hash, ok := i.hashes[h]; ok {
		return hash, nil
	}
	return "", ErrHashUnsupported
}
