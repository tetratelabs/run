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

package run

import (
	"context"
)

// Lifecycle tracks application lifecycle.
// And allows anyone to attach to it by exposing a `context.Context` that will
// end at the shutdown phase.
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
