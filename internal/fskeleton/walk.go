// Copyright (c) Elliot Nunn
// Licensed under the MIT license

package fskeleton

import (
	"cmp"
	"iter"
	"slices"
	"sync"

	"github.com/elliotnunn/BeHierarchic/internal/internpath"
)

type walkstuff struct {
	sync.Mutex
	list       []keyval
	sorted     bool
	full       bool
	stragglers []chan<- string
}

type keyval struct {
	key int64
	val internpath.Path
}

func (w *walkstuff) init() {}

func (w *walkstuff) put(name string, order int64) {
	w.Lock()
	defer w.Unlock()

	w.list = append(w.list, keyval{order, internpath.New(name)})
	w.sorted = false
	for _, ch := range w.stragglers {
		ch <- name
	}
}

func (w *walkstuff) done() {
	w.Lock()
	defer w.Unlock()

	w.full = true
	for _, ch := range w.stragglers {
		close(ch)
	}
	w.stragglers = nil
}

func (w *walkstuff) WalkFiles() iter.Seq[string] {
	return func(yield func(string) bool) {
		w.Lock()

		if w.full {
			slices.SortStableFunc(w.list, func(a, b keyval) int { return cmp.Compare(a.key, b.key) })
			w.sorted = true
		}
		for _, kv := range w.list {
			if !yield(kv.val.String()) {
				w.Unlock()
				return
			}
		}
		if w.full {
			w.Unlock()
			return
		}

		ch := make(chan string)
		w.stragglers = append(w.stragglers, ch)
		w.Unlock()

		for s := range ch {
			if !yield(s) {
				for range ch {
				} // waste the remainder
				return
			}
		}
	}
}
