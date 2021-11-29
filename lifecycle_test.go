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
	"testing"

	. "github.com/onsi/gomega"

	"github.com/tetratelabs/run"
)

func TestLifecycle(t *testing.T) {
	g := NewWithT(t)

	// when

	l := run.NewLifecycle()

	// then

	g.Expect(l.Name()).To(Equal("lifecycle-tracker"))

	// when

	errCh := make(chan error, 1)
	go func() {
		defer close(errCh)

		errCh <- (l.(run.Service)).Serve()
	}()

	// then

	g.Consistently(l.Context().Err, "25ms").Should(BeNil())

	// when

	(l.(run.Service)).GracefulStop()

	// then

	g.Expect(l.Context().Err()).To(MatchError(context.Canceled))
	g.Expect(l.Context().Done()).To(BeClosed())
}