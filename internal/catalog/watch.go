package catalog

import (
	"context"
	"io/fs"
	"path/filepath"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Watcher tells whether a collection's directories changed since they
// were last looked at, without looking at them: the kernel says
// (inotify). A server that runs long asks it where it would walk a
// collection on every call to see that its index is current
// (docs/architecture.md, §17.7). One watcher serves all collections,
// with one inotify instance: a user has few.
type Watcher struct {
	mu      sync.Mutex
	fd      int
	byWatch map[int32]string   // watch descriptor → collection
	watches map[string][]int32 // collection → its watch descriptors
	dirty   map[string]bool
	all     bool // events were lost: every collection may have changed
}

// NewWatcher returns a watcher, or nil where the kernel gives none (no
// inotify, or the user's instances are used up): the caller then looks
// for itself.
func NewWatcher() *Watcher {
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		return nil
	}
	return &Watcher{fd: fd, byWatch: map[int32]string{}, watches: map[string][]int32{}, dirty: map[string]bool{}}
}

// what changes a collection: a document written, added, removed or
// renamed, a directory added or removed, and the same of the watched
// directory itself.
const watchMask = unix.IN_CREATE | unix.IN_DELETE | unix.IN_MODIFY | unix.IN_CLOSE_WRITE | unix.IN_MOVED_FROM |
	unix.IN_MOVED_TO | unix.IN_ATTRIB | unix.IN_DELETE_SELF | unix.IN_MOVE_SELF | unix.IN_ONLYDIR | unix.IN_DONT_FOLLOW

// Watch starts watching a collection's directories as they are now,
// in place of those watched before, and reports whether it does: false
// for a collection with documents elsewhere (also), which have no
// directory of the collection's, or one whose directories cannot all
// be watched. Call it before looking at the collection: what changes
// from then on is told by Changed.
func (w *Watcher) Watch(ctx context.Context, c *Catalog, k *Collection) bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.drain()
	for _, wd := range w.watches[k.Name] {
		_, _ = unix.InotifyRmWatch(w.fd, uint32(wd))
		delete(w.byWatch, wd)
	}
	delete(w.watches, k.Name)
	delete(w.dirty, k.Name)
	if len(k.Also) > 0 {
		return false
	}
	dirs := []string{k.Path}
	err := c.Walk(ctx, Doc{Coll: k, Rel: "."}, nil, func(d Doc, e fs.DirEntry) bool {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(k.Path, filepath.FromSlash(d.Rel)))
		}
		return true
	})
	ok := err == nil
	for _, dir := range dirs {
		wd, err := unix.InotifyAddWatch(w.fd, dir, watchMask)
		if err != nil {
			ok = false
			break
		}
		w.byWatch[int32(wd)] = k.Name
		w.watches[k.Name] = append(w.watches[k.Name], int32(wd))
	}
	if !ok {
		for _, wd := range w.watches[k.Name] {
			_, _ = unix.InotifyRmWatch(w.fd, uint32(wd))
			delete(w.byWatch, wd)
		}
		delete(w.watches, k.Name)
	}
	return ok
}

// Changed reports whether a watched collection may have changed since
// Watch; a collection that is not watched has.
func (w *Watcher) Changed(k *Collection) bool {
	if w == nil {
		return true
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.drain()
	return w.all || w.dirty[k.Name] || len(w.watches[k.Name]) == 0
}

// drain reads the events there are and marks their collections.
func (w *Watcher) drain() {
	var buf [16 << 10]byte
	for {
		n, err := unix.Read(w.fd, buf[:])
		if err == unix.EINTR {
			continue
		}
		if err == unix.EAGAIN {
			return
		}
		if err != nil || n < unix.SizeofInotifyEvent {
			w.all = true // not to be trusted any more
			return
		}
		for off := 0; off+unix.SizeofInotifyEvent <= n; {
			ev := (*unix.InotifyEvent)(unsafe.Pointer(&buf[off]))
			if ev.Mask&unix.IN_Q_OVERFLOW != 0 {
				w.all = true
			} else if name, ok := w.byWatch[ev.Wd]; ok && ev.Mask&unix.IN_IGNORED == 0 {
				w.dirty[name] = true
			}
			off += unix.SizeofInotifyEvent + int(ev.Len)
		}
	}
}
