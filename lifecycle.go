// Copyright (c) Tetrate, Inc 2021 All Rights Reserved.

package run

import (
	"context"
)

// Lifecycle tracks application lifecycle.
// And allows anyone to attach to it by exposing a `context.Context` that will end at the shutdown phase.
type Lifecycle interface {
	Unit

	// Context returns a context that gets cancelled when application
	// is stopped.
	Context() context.Context
}

// NewLifecycle returns a new application lifecycle tracker.
func NewLifecycle() Lifecycle {
	ctx, cancel := context.WithCancel(context.Background())
	return &lifecycle{
		ctx:    ctx,
		cancel: cancel,
	}
}

type lifecycle struct {
	ctx    context.Context
	cancel context.CancelFunc
}

var _ Service = (*lifecycle)(nil)

// Name implements Unit.
func (l *lifecycle) Name() string {
	return "lifecycle-tracker"
}

// Serve implements Server.
func (l *lifecycle) Serve() error {
	<-l.ctx.Done()
	return nil
}

// GracefulStop implements Server.
func (l *lifecycle) GracefulStop() {
	l.cancel()
}

func (l *lifecycle) Context() context.Context {
	return l.ctx
}
