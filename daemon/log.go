package main

import (
	"fmt"
	"io"
	"log"
	"os"
)

// Logger wraps the standard log.Logger, writing to a file if LogPath is
// set (the normal case for a real daemon, whose stdout usually isn't
// attached to anything once daemonized) or to stdout otherwise (handy for
// `daemon run` in the foreground during development/debugging).
type Logger struct {
	*log.Logger
	file *os.File
}

func NewLogger(path string) (*Logger, error) {
	var w io.Writer = os.Stdout
	var f *os.File
	if path != "" {
		var err error
		f, err = os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return nil, fmt.Errorf("opening log file %s: %w", path, err)
		}
		w = f
	}
	return &Logger{
		Logger: log.New(w, "isolator-daemon: ", log.LstdFlags),
		file:   f,
	}, nil
}

func (l *Logger) Close() {
	if l.file != nil {
		l.file.Close()
	}
}

func stdoutStderr() (io.Writer, io.Writer) {
	return os.Stdout, os.Stderr
}
