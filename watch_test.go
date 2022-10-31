package main

import (
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestWatcher(t *testing.T) {
	var (
		oldFilePath, newFilePath string
		oldFileName              = "xxx"
		newFileName              = "yyy"
		wg                       sync.WaitGroup
	)

	dir, _ := os.MkdirTemp("", "tes")
	defer os.RemoveAll(dir)

	oldFilePath = filepath.Join(dir, oldFileName)
	newFilePath = filepath.Join(dir, newFileName)

	w := NewWatcher()
	defer w.Close()

	err := w.Add(dir)
	require.NoError(t, err)

	f, err := os.Create(oldFilePath)
	require.NoError(t, err)
	_ := f.Close()

	// assert and wait for Rename
	wg.Add(1)
	go func() {
		defer wg.Done()
		assertEvent(t, w, oldFilePath, Rename)
	}()
	err = os.Rename(oldFilePath, newFilePath)
	require.NoError(t, err)
	wg.Wait()

	wg.Add(1)
	go func() {
		defer wg.Done()
		assertEvent(t, w, newFilePath, Remove)
	}()

	err = os.Remove(newFilePath)
	require.NoError(t, err)
	wg.Wait()

	wg.Add(1)
	go func() {
		defer wg.Done()
		assertEvent(t, w, oldFilePath, Create)
	}()

	f, err = os.Create(oldFilePath)
	require.NoError(t, err)
	f.Close()
	wg.Wait()

	// create another dir
	dir2, err := os.MkdirTemp("", "test2")
	require.NoError(t, err)
	defer os.RemoveAll(dir2)
	oldFilePath2 := filepath.Join(dir2, oldFileName)

	err = w.Add(dir2)
	require.NoError(t, err)

	wg.Add(1)
	go func() {
		defer wg.Done()
		assertEvent(w, oldFilePath, Move, t)
	}()

	err = os.Rename(oldFilePath, oldFilePath2)
	require.NoError(t, err)
	wg.Wait()
}

func assertEvent(t *testing.T, w *Watcher, path string, op Op) {
	t.Helper()
	for {
		select {
		case ev := <-w.Events:
			if ev.IsDirEvent() {
				continue
			}
			require.True(t, ev.HasOps(op))
			require.Equal(t, path, ev.Path)
			return
		case err := <-w.Errors:
			t.Fatal(err)
			return
		}
	}
}
