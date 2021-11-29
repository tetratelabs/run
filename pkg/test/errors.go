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

package test

import "errors"

// Error allows for creating constant errors instead of sentinel ones.
type Error string

func (e Error) Error() string { return string(e) }

const (
	// FlagErr can be used as formatting string for flag related validation
	// errors where the first variable lists the flag name and the second
	// variable is the actual error.
	FlagErr = "--%s error: %w"

	// ErrRequired is returned when required config options are not provided.
	ErrRequired Error = "required"

	// ErrInvalidPath is returned when a path config option is invalid.
	ErrInvalidPath Error = "invalid path"
)

// HasError checks if the given error is the same as, or wraps the given error.
// Supports errors wrapped using Go's "%w", using `errors.Wrap` and tetratelabs
// multierror.
func HasError(in, expected error) bool {
	if in == expected {
		return true
	}

	// check if the error is the expected one, or is errWrapped using Go's "%w"
	if errors.Is(in, expected) {
		return true
	}

	checkNoStandardError := func(err error) bool {
		// check if the error has been errWrapped using `errors.Wrap` and
		// iterate to check the cause
		if c, ok := err.(causer); ok {
			return HasError(c.Cause(), expected)
		}

		// Otherwise, check if is a tetratelabs multierror
		if wrapper, ok := err.(errWrapper); ok {
			for _, e := range wrapper.WrappedErrors() {
				if HasError(e, expected) {
					return true
				}
			}
		}

		return false
	}

	if checkNoStandardError(in) {
		return true
	}

	// Since `errors.Is` will not handle multierror or errors.Wrap from non
	// first level, find and check them
	unwrapped := errors.Unwrap(in)
	for unwrapped != nil {
		if checkNoStandardError(unwrapped) {
			return true
		}
		unwrapped = errors.Unwrap(unwrapped)
	}

	return false
}

// see `github.com/pkg/errors.Cause`
// The Cause() method there recursively goes to the root error, but we want to
// check all the errors in the stacktrace, so we can't use it and have to do it
// manually.
type causer interface {
	Cause() error
}

// errWrapper defines the methods implemented by the multierror packages.
// The codebase uses the one in tetratelabs but also the Hashi one, so we use
// this to avoid importing the deprecated Hashi package.
type errWrapper interface {
	WrappedErrors() []error
}
