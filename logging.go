// Copyright (c) Tetrate, Inc 2020 All Rights Reserved.

package run

import (
	l "github.com/tetratelabs/log"
)

// logOptions provides a Unit compatible bridge to the tetratelabs log package.
type logOptions struct {
	*l.Options
}

var (
	_ Unit   = (*logOptions)(nil)
	_ Config = (*logOptions)(nil)
)

func (logOptions) Name() string {
	return "log"
}

func (l *logOptions) FlagSet() *FlagSet {
	flags := NewFlagSet("Logging options")
	l.AttachToFlagSet(flags.FlagSet)
	return flags
}

func (l *logOptions) Validate() error {
	return nil
}
