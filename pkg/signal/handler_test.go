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

package signal

import (
	"errors"
	"testing"
	"time"

	"github.com/tetratelabs/run"
	"github.com/tetratelabs/run/pkg/test"
)

var (
	errClose = errors.New("requested close")
	errIRQ   = errors.New("interrupt")
)

func TestSignalHandlerStop(t *testing.T) {
	var (
		g = run.Group{}
		s Handler
	)

	// add our signal handler to Group
	g.Register(&s)

	// add our interrupter
	g.Register(&test.TestSvc{
		SvcName: "irqsvc",
		Execute: func() error { return errClose },
	})

	// start group
	res := make(chan error)
	go func() { res <- g.Run() }()

	select {
	case err := <-res:
		if !errors.Is(err, errClose) {
			t.Errorf("expected clean shutdown, got %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Errorf("timeout")
	}
}

func TestSignalHandlerSignals(t *testing.T) {
	var (
		s      Handler
		errHUP = errors.New("sigHUP called")
	)

	tests := []struct {
		action func()
		err    error
	}{
		{action: s.sendHUP, err: errHUP},
		{action: s.sendQUIT, err: ErrSignal},
	}
	for idx, tt := range tests {
		var (
			g   = run.Group{}
			irq = make(chan error)
		)

		s.RefreshCallback = func() error {
			return errHUP
		}

		// add our signal handler to Group
		g.Register(&s)

		// add our interrupter
		g.Register(&test.TestSvc{
			SvcName: "irqsvc",
			Execute: func() error {
				tt.action()
				return <-irq
			},
			Interrupt: func() { irq <- errIRQ },
		})

		// start group
		res := make(chan error)
		go func() { res <- g.Run() }()

		select {
		case err := <-res:
			if !errors.Is(err, tt.err) {
				t.Errorf("[%d] expected %v, got %v", idx, tt.err, err)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("[%d] timeout", idx)
		}

	}
}
