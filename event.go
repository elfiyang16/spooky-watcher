package main

import (
	"bytes"
	"os"
)

type Op uint32

const (
	Create Op = 1 << iota
	Remove
	Modify
	Rename
	Chmod
	Move
)

type Event struct {
	FileInfo os.FileInfo
	Path     string
	Op       Op
}

func (op Op) String() string {
	var buffer bytes.Buffer
	if op&Create == Create {
		buffer.WriteString("|CREATE")
	}
	if op&Remove == Remove {
		buffer.WriteString("|REMOVE")
	}
	if op&Modify == Modify {
		buffer.WriteString("|MODIFY")
	}
	if op&Rename == Rename {
		buffer.WriteString("|RENAME")
	}
	if op&Chmod == Chmod {
		buffer.WriteString("|CHMOD")
	}
	if op&Move == Move {
		buffer.WriteString("|MOVE")
	}
	if buffer.Len() == 0 {
		return ""
	}
	return buffer.String()[1:]
}

func (e *Event) IsDirEvent() bool {
	if e == nil {
		return false
	}
	return e.FileInfo.IsDir()
}

func (e *Event) HasOps(ops ...Op) bool {
	if e == nil {
		return false
	}
	for _, op := range ops {
		if e.Op&op != 0 {
			return true
		}
	}
	return false
}
