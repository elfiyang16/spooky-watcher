package main

import (
	"errors"
	"fmt"
	"go.uber.org/atomic"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	ErrWatcherStarted = errors.New("watcher already started")
	ErrWatcherClosed  = errors.New("watcher already closed")
)

type Watcher struct {
	Events  chan Event
	Errors  chan error
	closed  chan struct{}
	names   map[string]struct{}    // list of names to watch
	files   map[string]os.FileInfo // all files to watch up to date
	wg      sync.WaitGroup
	running atomic.Int32 // default to 0
	mu      sync.Mutex
}

func NewWatcher() *Watcher {
	return &Watcher{
		Events: make(chan Event),
		Errors: make(chan error),
		closed: make(chan struct{}),
		names:  make(map[string]struct{}),
		files:  make(map[string]os.FileInfo),
	}
}

func (w *Watcher) Start(d time.Duration) error {
	if !w.running.CompareAndSwap(0, 1) {
		return ErrWatcherStarted
	}

	select {
	case <-w.closed:
		return ErrWatcherClosed
	default:
	}

	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.doWatch(d)
	}()
	return nil
}

func (w *Watcher) Close() {
	// already closed
	if !w.running.CompareAndSwap(0, 1) {
		return
	}

	close(w.closed)
	w.wg.Wait()

	close(w.Events)
	close(w.Errors)

	w.mu.Lock()
	w.names = make(map[string]struct{})
	w.files = make(map[string]os.FileInfo)
	w.mu.Unlock()
}

func (w *Watcher) Add(name string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	select {
	case <-w.closed:
		return ErrWatcherClosed
	default:
	}

	fileList, err := listForName(name)
	if err != nil {
		return err
	}

	w.names[name] = struct{}{}
	for fp, fi := range fileList {
		w.files[fp] = fi
	}
	return nil
}

func (w *Watcher) Remove(name string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	select {
	case <-w.closed:
		return ErrWatcherClosed
	default:
	}

	w.doRemove(name)
	return nil
}

func (w *Watcher) doWatch(d time.Duration) {
	ticker := time.NewTicker(d)
	defer ticker.Stop()
	for {
		select {
		case <-w.closed:
			return
		case <-ticker.C:
			currFileList := w.listForAll()
			w.pollEvents(currFileList)
			w.mu.Lock()
			w.files = currFileList
			w.mu.Unlock()
		}
	}
}

func (w *Watcher) pollEvents(currFileList map[string]os.FileInfo) {
	w.mu.Lock()
	defer w.mu.Unlock()

	created := make(map[string]os.FileInfo)
	removed := make(map[string]os.FileInfo)

	for latestFp, latestFi := range w.files {
		// 1. if not found in files -> removed
		if _, ok := currFileList[latestFp]; !ok {
			removed[latestFp] = latestFi
		}
	}

	for fp, currFi := range currFileList {
		latestFi, ok := w.files[fp]
		if !ok {
			// 2. if not found in currFileList -> created
			created[fp] = currFi
			continue
		}
		// 3. if ModTime + Size changes -> modify
		if !latestFi.ModTime().Equal(currFi.ModTime()) || latestFi.Size() != currFi.Size() {
			select {
			case <-w.closed:
				return
			case w.Events <- Event{
				Path:     fp,
				Op:       Modify,
				FileInfo: currFi,
			}:
			}
		}
	}

	for removeFp, removeFi := range removed {
		for createFp, createFi := range created {
			// 4. if removed file becomes created file -> move
			if os.SameFile(removeFi, createFi) {
				ev := Event{
					Path:     removeFp,
					Op:       Move,
					FileInfo: removeFi,
				}
				if filepath.Dir(removeFp) == filepath.Dir(createFp) {
					ev.Op = Rename
				}
				delete(removed, removeFp)
				delete(created, createFp)
				select {
				case <-w.closed:
					return
				case w.Events <- ev:
				}
			}

		}
	}

	for fp, fi := range created {
		select {
		case <-w.closed:
			return
		case w.Events <- Event{Path: fp, Op: Create, FileInfo: fi}:
		}
	}
	for fp, fi := range removed {
		select {
		case <-w.closed:
			return
		case w.Events <- Event{Path: fp, Op: Remove, FileInfo: fi}:
		}
	}
}

func (w *Watcher) doRemove(name string) {
	delete(w.names, name)

	fi, ok := w.files[name]
	if !ok {
		return // check if it's still exist
	}

	delete(w.files, name)

	if !fi.IsDir() {
		return
	}

	for fp := range w.files {
		if filepath.Dir(fp) == name {
			delete(w.files, fp)
		}
	}
}

func (w *Watcher) listForAll() map[string]os.FileInfo {
	w.mu.Lock()
	defer w.mu.Unlock()

	fileList := make(map[string]os.FileInfo)
	for name := range w.names {
		fl, err := listForName(name)
		if err != nil {
			if os.IsNotExist(err) {
				w.doRemove(name)
			}
			select {
			case <-w.closed:
				return nil
			case w.Errors <- err: // report on error if not exist
			}
		}
		for fp, fi := range fl {
			fileList[fp] = fi
		}
	}
	return fileList
}

func listForName(name string) (map[string]os.FileInfo, error) {
	stat, err := os.Stat(name)
	if err != nil {
		return nil, fmt.Errorf("name %s with error %v", name, err)
	}

	list := make(map[string]os.FileInfo)
	list[name] = stat

	if !stat.IsDir() {
		// not a directory, return
		return list, nil
	}

	dirEntries, err := os.ReadDir(name)
	if err != nil {
		return nil, fmt.Errorf("directory %s with error %v", name, err)
	}

	for _, dirEntry := range dirEntries {
		fp := filepath.Join(name, dirEntry.Name())
		list[fp], _ = dirEntry.Info()
	}

	return list, nil
}
