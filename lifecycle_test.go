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

package run_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/tetratelabs/run"
)

func TestLifecycle(t *testing.T) {
	l := run.NewLifecycle()

	if want, have := "lifecycle-tracker", l.Name(); want != have {
		t.Errorf("unexpected unit name: want %q, have %q", want, have)
	}

	errCh := make(chan error, 1)
	go func() {
		defer close(errCh)

		errCh <- (l.(run.Service)).Serve()
	}()

	waitFor := time.Now().Add(25 * time.Millisecond)
	for {
		err := l.Context().Err()
		if err != nil {
			t.Fatalf("unexpected context error: %+v", err)
		}
		if time.Now().After(waitFor) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	(l.(run.Service)).GracefulStop()

	ctx := l.Context()

	if want, have := context.Canceled, ctx.Err(); want != have {
		t.Errorf("unexpected error: want %v, have %v", want, have)
	}

	if !isChannelClosed(ctx.Done()) {
		t.Errorf("expected context.Done() to be closed")
	}
}

func isChannelClosed(val interface{}) bool {
	channelValue := reflect.ValueOf(val)
	winnerIndex, _, open := reflect.Select([]reflect.SelectCase{
		{Dir: reflect.SelectRecv, Chan: channelValue},
		{Dir: reflect.SelectDefault},
	})
	var closed bool
	if winnerIndex == 0 {
		closed = !open
	} else if winnerIndex == 1 {
		closed = false
	}
	return closed
}
