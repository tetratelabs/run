// Copyright (c) Tetrate, Inc 2021 All Rights Reserved.

package run_test

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"

	"github.com/tetrateio/tetrate/pkg/run"
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
