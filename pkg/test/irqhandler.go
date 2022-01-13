// Copyright (c) Tetrate, Inc 2021.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package test adds helper utilities for testing run.Group enabled services.
package test

import (
	"errors"
	"io"
	"sync"

	"github.com/tetratelabs/run"
)

// IRQService is a run.Service and io.Closer implementation. It can be
// registered with run.Group and calling Close will initiate shutdown of the
// run.Group.
// This is primarily used for unit tests of run.Group enabled services and
// handlers.
type IRQService interface {
	run.Service
	io.Closer
}

// NewIRQService returns a IRQService for usage in run.Group tests.
// Use the Close() method to shutdown a run.Group.
// The GracefulStop() method will be called automatically when run.Group is
// shutting down. Both Serve() and GracefulStop() should not be called outside
// of the internal run.Group logic as run.Service is to be managed by run.Group
func NewIRQService(cleanup func()) IRQService {
	return &irqSvc{
		irq: make(chan error),
		cfn: cleanup,
	}
}

type irqSvc struct {
	irq chan error
	cfn func()
	mu  sync.Mutex
}

func (i *irqSvc) Name() string {
	return "irqsvc"
}

func (i *irqSvc) Serve() error {
	return <-i.irq
}

// GracefulStop is managed by run.Group. Do not call directly.
func (i *irqSvc) GracefulStop() {
	i.cfn()

	i.mu.Lock()
	defer i.mu.Unlock()

	if i.irq != nil {
		close(i.irq)
		i.irq = nil
	}
}

// Close signals the IRQService to shutdown and run.Group is responsible for
// cleaning up and calling GracefulStop() on all registered units.
func (i *irqSvc) Close() error {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.irq != nil {
		i.irq <- run.ErrRequestedShutdown
	}
	return nil
}

// TestSvc allows one to quickly bootstrap a run.GroupService from simple
// functions. This is especially useful for unit tests.
type TestSvc struct {
	SvcName   string
	Execute   func() error
	Interrupt func()
}

// Name implements run.Unit.
func (t TestSvc) Name() string {
	return t.SvcName
}

// Serve implements run.Service
func (t TestSvc) Serve() error {
	if t.Execute == nil {
		return errors.New("missing execute function")
	}
	return t.Execute()
}

// GracefulStop implements run.Service
func (t TestSvc) GracefulStop() {
	if t.Interrupt == nil {
		return
	}
	t.Interrupt()
}
