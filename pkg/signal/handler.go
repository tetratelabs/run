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

// Package signal implements a run.GroupService handling incoming unix signals.
package signal

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// Error allows for creating constant errors instead of sentinel ones.
type Error string

// Error implements error.
func (e Error) Error() string { return string(e) }

// ErrSignal is returned when a termination signal is received.
const ErrSignal = Error("signal received")

// Handler implements a unix signal handler as run.GroupService.
type Handler struct {
	// RefreshCallback is called when a syscall.SIGHUP is received.
	// If the callback returns an error, the signal handler is stopped. In a
	// run.Group environment this means the entire run.Group is requested to
	// stop.
	RefreshCallback func() error

	signal chan os.Signal
	cancel chan struct{}
}

// Name implements run.Unit.
func (h Handler) Name() string {
	return "signal"
}

// PreRun implements run.PreRunner to initialize the handler.
func (h *Handler) PreRun() error {
	h.cancel = make(chan struct{})
	// Notify uses a non-blocking channel send. If handling a HUP and receiving
	// an INT shortly after, it might get lost if we don't use a buffered
	// channel here.
	// E.g. https://gist.github.com/basvanbeek/c0e2ef60b73c8a5d5028ee0cf1afb576
	h.signal = make(chan os.Signal, 2)
	signal.Notify(h.signal,
		syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	return nil
}

// Serve implements run.GroupService and listens for incoming unix signals.
// If a callback handler was registered it will be executed if a "SIGHUP" is
// received. If the callback handler returns an error it will exit in error and
// initiate Group shutdown if used in a run.Group environment.
func (h *Handler) Serve() error {
	for {
		select {
		case sig := <-h.signal:
			switch sig {
			case syscall.SIGHUP:
				if h.RefreshCallback != nil {
					if err := h.RefreshCallback(); err != nil {
						return fmt.Errorf("error on signal %s: %w", sig, err)
					}
				}
			case syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM:
				return fmt.Errorf("%s %w", sig, ErrSignal)
			}
		case <-h.cancel:
			signal.Stop(h.signal)
			close(h.signal)
			return nil
		}
	}
}

// GracefulStop implements run.GroupService and will close the signal handler.
func (h *Handler) GracefulStop() {
	close(h.cancel)
}

// sendHUP is for test purposes
func (h *Handler) sendHUP() {
	h.signal <- syscall.SIGHUP
}

// sendQUIT is for test purposes
func (h *Handler) sendQUIT() {
	h.signal <- syscall.SIGQUIT
}
