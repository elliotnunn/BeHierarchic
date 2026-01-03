package main

import (
	"fmt"
	"io"
	"io/fs"
	"iter"
	gopath "path"
	"runtime"
	"slices"
	"strings"
	"sync"
	"unsafe"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/elliotnunn/BeHierarchic/internal/internpath"
)

// A generalisation of a "file path"
// - specifies the public containing *FS, the hidden sub-FS, and the path within that FS
// - suitable for use as a map key
// - common operations are fast (Open, Stat, ReadDir)
// - rarer operations are possible (specifically String to get full path)
type path struct {
	container *FS
	fsys      fs.FS
	name      internpath.Path
}

// Save memory when the container pointer is redundant
type thinPath struct {
	fsys fs.FS
	name internpath.Path
}

func (tp thinPath) Thick(container *FS) path {
	return path{container: container, fsys: tp.fsys, name: tp.name}
}
func (o path) Thin() thinPath {
	return thinPath{o.fsys, o.name}
}

// ShallowJoin returns a path with some elements added. Caution! It is only a lexical operation,
// and will return an unusable path if passed a Special character
func (o path) ShallowJoin(p string) path { o.name = o.name.Join(p); return o }

// Open opens the raw file (no archive-browsing decorations) for the benefit of reader2readerat
func (o path) Open() (fs.File, error) { return o.fsys.Open(o.name.String()) }

// pathRenderer converts a [path] to a textual path.
//
// When similar paths are encountered successively, the cost of allocations and locking is amortized.
type pathRenderer struct {
	buf         []byte
	left, right int
	paths       []path
	nupaths     []path
}

func (r *pathRenderer) dump() string {
	return fmt.Sprintf("left=%q right=%q npaths=%d", r.buf[:r.left], r.buf[len(r.buf)-r.right:], len(r.paths))
}

// Render converts a path struct to a textual path.
//
// The returned buffer is only valid until the next call to Render.
// Capacity is guaranteed for it to grow by at least one byte.
func (r *pathRenderer) Render(o path) []byte {
	nkeep := 0

outerloop:
	for o != o.container.rootPath() {
		for i, existingp := range slices.Backward(r.paths) {
			if o == existingp {
				nkeep = i + 1
				break outerloop
			}
		}

		r.nupaths = append(r.nupaths, o)
		if o.name == (internpath.Path{}) {
			o.container.rMu.RLock()
			archive := o.container.reverse[o.fsys]
			o.container.rMu.RUnlock()
			r.put("/", archive.name.PutBase, Special)
			o = archive.Thick(o.container)
			o.name = o.name.Dir()
		} else {
			r.put("/", o.name.PutBase, "")
			o.name = o.name.Dir()
		}
	}

	for range len(r.paths) - nkeep {
		for { // delete one slash-prefixed path component
			r.left--
			if r.buf[r.left] == '/' {
				break
			}
		}
	}
	r.paths = r.paths[:nkeep]
	for _, nu := range slices.Backward(r.nupaths) {
		r.paths = append(r.paths, nu)
	}
	r.nupaths = r.nupaths[:0]
	r.allToLeft()
	for len(r.buf) < r.left+2 {
		r.grow() // guarantee capacity for appends
	}
	if r.left == 0 {
		r.buf[r.left] = '.' // root
		return r.buf[r.left:][:1]
	}
	return r.buf[1:r.left]
}

func (r *pathRenderer) grow() {
	newsize := max(16, 2*len(r.buf))
	buf2 := make([]byte, newsize)
	copy(buf2, r.buf[:r.left])
	copy(buf2[len(buf2)-r.right:], r.buf[len(r.buf)-r.right:])
	r.buf = buf2
}

// put inserts text on the leftmost side of the "right" field.
// fn is function that copies text to a buffer, returning the length,
// or 0 to request a retry with a larger buffer.
func (r *pathRenderer) put(prefix string, fn func([]byte) int, suffix string) {
	for {
		if len(r.buf)-r.left-r.right > len(prefix)+len(suffix) {
			n := fn(r.buf[r.left+len(prefix) : len(r.buf)-r.right-len(suffix)])
			if n > 0 {
				total := len(prefix) + n + len(suffix)
				copy(r.buf[r.left:], prefix)
				copy(r.buf[r.left+len(prefix)+n:], suffix)
				copy(r.buf[len(r.buf)-r.right-total:][:total], r.buf[r.left:])
				r.right += total
				return
			}
		}
		r.grow()
	}
}

func (r *pathRenderer) allToLeft() {
	copy(r.buf[r.left:], r.buf[len(r.buf)-r.right:])
	r.left += r.right
	r.right = 0
}

// String converts a path struct to a textual path.
//
// There is quite a bit of allocation involved.
// Consider using a pathRenderer instead.
func (o path) String() string {
	o.container.rMu.RLock()
	defer o.container.rMu.RUnlock()
	warps := []string{o.name.String()}
	thin := o.Thin()
	for thin.fsys != o.container.root {
		thin = o.container.reverse[thin.fsys]
		warps = append(warps, thin.name.String()+Special)
	}
	slices.Reverse(warps)
	return gopath.Join(warps...)
}

func (o path) deepWalk() iter.Seq2[path, fs.FileMode] {
	return func(yield func(path, fs.FileMode) bool) {
		type it struct {
			next func() (path, fs.FileMode, bool)
			stop func()
		}
		var stack = make([]it, 0, 32)
		f1, f2 := iter.Pull2(o.flatWalk())
		stack = append(stack, it{f1, f2})

		defer func() {
			for _, it := range stack {
				it.stop()
			}
		}()

		for len(stack) != 0 {
			path, kind, ok := stack[len(stack)-1].next()
			if !ok {
				stack = stack[:len(stack)-1]
				continue
			}
			if !yield(path, kind) {
				return
			}
			if kind.IsRegular() {
				ok, mnt := path.getArchive(false, false)
				if ok {
					f1, f2 := iter.Pull2(mnt.flatWalk())
					stack = append(stack, it{f1, f2})
				}
			}
		}
	}
}

type selfWalking interface {
	Walk(waitFull bool) iter.Seq2[fmt.Stringer, fs.FileMode]
}

func (o path) flatWalk() iter.Seq2[path, fs.FileMode] {
	return func(yield func(path, fs.FileMode) bool) {
		if selfWalking, ok := o.fsys.(selfWalking); ok {
			for stringer, kind := range selfWalking.Walk(false /*not exhaustive*/) {
				pathname := stringer.(internpath.Path)
				if pathname.IsWithin(o.name) {
					ok := yield(path{o.container, o.fsys, pathname}, kind)
					if !ok {
						return
					}
				}
			}
		} else {
			fs.WalkDir(o.fsys, o.name.String(), func(pathname string, d fs.DirEntry, err error) error {
				ok := yield(path{o.container, o.fsys, internpath.Make(pathname)}, d.Type())
				if !ok {
					return io.EOF // any error is fine
				}
				return nil
			})
		}
	}
}

// path turns a string into our internal path representation
//
// Nonexistent paths might, but won't always, return fs.ErrNotExist
func (fsys *FS) path(name string) (path, error) {
	warps := strings.Split(name, Special+"/")
	if strings.HasSuffix(name, Special) {
		warps[len(warps)-1] = strings.TrimSuffix(warps[len(warps)-1], Special)
		warps = append(warps, ".")
	}

	p := fsys.rootPath()
	for _, el := range warps[:len(warps)-1] {
		var isar bool
		isar, p = p.ShallowJoin(el).getArchive(true, true)
		if !isar {
			return path{}, fs.ErrNotExist
		}
	}
	return p.ShallowJoin(warps[len(warps)-1]), nil
}

func (fsys *FS) rootPath() path { return path{fsys, fsys.root, internpath.Path{}} }

// glob searches for paths matching a doublestar glob pattern.
// Patterns ending with `/` match only directories, other patterns match files and directories.
//
// Returned buffers are only valid until the next iteration.
// Effort is made to return results in a deterministic order.
// Effort is made to be fast, although with questionable success.
func (o path) glob(pattern string) iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		ignorePrefix := 0
		if str := o.String(); str != "." {
			ignorePrefix = len(str) + 1
		}
		pattern, dironly := strings.CutSuffix(pattern, "/")

		// Set up channels
		const batch = 128 // tuned
		nworker := runtime.GOMAXPROCS(-1)
		cancel := make(chan struct{}) // channel sends should also wait on this channel

		// The first goroutine sends batches of paths on these channels in a round-robin
		pathChan := make([]chan [batch]path, nworker)
		for i := range pathChan {
			pathChan[i] = make(chan [batch]path, nworker)
		}

		// The second goroutine shares the yield() function in a round-robin
		robinChan := make([]chan func([]byte) bool, nworker)
		for i := range robinChan {
			robinChan[i] = make(chan func([]byte) bool, 1)
		}
		robinChan[0] <- yield // start it out

		// Acquire paths and batch them into generous work units
		go func() {
			pull, _ := iter.Pull2(o.deepWalk())

			defer func() {
				for wkr := range nworker {
					close(pathChan[wkr])
				}
			}()

			for {
				for wkr := range nworker {
					var batch [batch]path
					for i := range len(batch) {
						o, mode, ok := pull()
						for ok && !mode.IsDir() && dironly {
							o, mode, ok = pull()
						}
						if !ok {
							select {
							case <-cancel:
								return
							case pathChan[wkr] <- batch:
								return
							}
						}
						batch[i] = o
					}
					select {
					case <-cancel:
						return
					case pathChan[wkr] <- batch:
					}
				}
			}
		}()

		// Then make a buncha workers to... do the work
		var wg sync.WaitGroup
		for wkr := range nworker {
			wg.Go(func() {
				robinRecv, robinSend := robinChan[wkr], robinChan[(wkr+1)%nworker]
				var renderers [batch]pathRenderer
				for paths := range pathChan[wkr] {
					var bufs [batch][]byte
					for i, o := range paths {
						if o.container == nil {
							break
						}

						buf := renderers[i].Render(o)
						if len(buf) <= ignorePrefix {
							continue
						}

						relpath := buf[ignorePrefix:]
						unsafeString := unsafe.String(&relpath[0], len(relpath))
						if doublestar.MatchUnvalidated(pattern, unsafeString) {
							bufs[i] = buf
						}
					}

					yfunc, ok := <-robinRecv
					if !ok { // one of our sibling workers has met an end condition
						close(robinSend)
						return
					}
					for _, buf := range bufs {
						if buf == nil {
							continue
						}
						if !yfunc(buf) {
							close(cancel)
							close(robinSend)
							return
						}
					}
					robinSend <- yfunc
				}
			})
		}
		wg.Wait()
	}
}
