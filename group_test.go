// Copyright (c) Tetrate, Inc 2019 All Rights Reserved.

package run

import (
	"errors"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/tetratelabs/multierror"

	"github.com/tetrateio/tetrate/pkg"
)

const (
	errFlags = pkg.Error("flagset error")
	errClose = pkg.Error("requested close")
	errIRQ   = pkg.Error("interrupt")
)

func TestRunGroupSvcLifeCycle(t *testing.T) {
	var (
		g   Group
		s   service
		irq = make(chan error)
	)

	// add our service to Group
	g.AddService(&s)

	// add our interruptor
	g.Add(func() error { return errIRQ }, func(error) {})

	// start Group
	go func() { irq <- g.Run("./myService", "-f", "1") }()

	select {
	case err := <-irq:
		if err != errIRQ {
			t.Errorf("Expected proper close, got %v", err)
		}
		if !s.flagSet {
			t.Error("Expected flagSet logic to run")
		}
		if !s.validated {
			t.Error("Expected validation logic to run")
		}
		if s.configItem != 1 {
			t.Errorf("Expected flag value to be %d, got %d", 1, s.configItem)
		}
		if !s.preRun {
			t.Errorf("Expected preRun logic to run")
		}
		if !s.serve {
			t.Errorf("Expected serve logic to run")
		}
		if !s.gracefulStop {
			t.Errorf("Expected graceful stop logic to run")
		}
	case <-time.After(100 * time.Millisecond):
		t.Errorf("timeout")
	}
}

func TestRunGroupMultiErrorHandling(t *testing.T) {
	var (
		g = Group{Name: "MyService"}

		err1 = errors.New("cfg1 failed")
		err2 = errors.New("cfg2 failed")
		err3 = errors.New("cfg3 failed")

		mErr = multierror.SetFormatter(
			multierror.Append(nil, err1, err2, err3),
			multierror.ListFormatFunc,
		)

		cfg1 = failingConfig{e: err1}
		cfg2 = failingConfig{e: err2}
		cfg3 = failingConfig{e: err3}
	)

	g.AddConfig(cfg1, cfg2, cfg3)

	if want, have := mErr.Error(), g.Run().Error(); want != have {
		t.Errorf("invalid error payload returned:\nwant:\n%+v\nhave:\n%+v\n", want, have)
	}
}

func TestRunGroupEarlyBailFlags(t *testing.T) {
	var (
		g   = Group{HelpText: "placeholder"}
		irq = make(chan error)
	)

	type test struct {
		flag   string
		hasErr bool
	}

	for idx, tt := range []test{
		{flag: "-v"},
		{flag: "-h"},
		{flag: "--version"},
		{flag: "--help"},
		{flag: "--non-existent", hasErr: true},
	} {

		// start Group
		go func() { irq <- g.Run("./myService", tt.flag) }()

		select {
		case err := <-irq:
			if !tt.hasErr && err != nil {
				t.Errorf("[%d] Expected proper close, got %v", idx, err)
			}
			if tt.hasErr && err == nil {
				t.Errorf("[%d] Expected early bail with error, got nil", idx)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("timeout")
		}
	}
}

func TestDuplicateFlag(t *testing.T) {
	var (
		g     Group
		flag1 flagTestConfig
		flag2 flagTestConfig
		irq   = make(chan error)
	)

	// add our flags
	g.AddConfig(&flag1, &flag2)

	// add our interruptor
	g.Add(func() error { return errIRQ }, func(error) {})

	// start Group
	go func() { irq <- g.Run("./myService", "-f", "3") }()

	select {
	case err := <-irq:
		if err != errIRQ {
			t.Errorf("Expected proper close, got %v", err)
		}
		if flag1.value != 3 {
			t.Errorf("Expected flag1 = %d, got %d", 3, flag1.value)
		}
		if flag2.value != 10 {
			t.Errorf("Expected flag2 = %d, got %d", 10, flag2.value)
		}
	case <-time.After(100 * time.Millisecond):
		t.Errorf("timeout")
	}
}

type flagTestConfig struct {
	value int
}

func (f *flagTestConfig) FlagSet() *pflag.FlagSet {
	flags := pflag.NewFlagSet("flag test config", pflag.ContinueOnError)
	flags.IntVarP(&f.value, "flagtest", "f", 10, "flagtester")
	return flags
}

func (f flagTestConfig) Validate() error { return nil }

type failingConfig struct {
	e error
}

func (f failingConfig) FlagSet() *pflag.FlagSet { return nil }

func (f failingConfig) Validate() error { return f.e }

type service struct {
	configItem   int
	flagSet      bool
	validated    bool
	preRun       bool
	serve        bool
	gracefulStop bool
	closer       chan error
}

func (s *service) FlagSet() *pflag.FlagSet {
	s.flagSet = true
	flags := pflag.NewFlagSet("dummy flagset", pflag.ContinueOnError)
	flags.IntVarP(&s.configItem, "flagtest", "f", 5, "rungroup flagset test")
	return flags
}

func (s *service) Validate() error {
	s.validated = true
	if s.configItem != 1 {
		return errFlags
	}
	return nil
}

func (s *service) PreRun() error {
	s.preRun = true
	s.closer = make(chan error, 5)
	return nil
}

func (s *service) Serve() error {
	s.serve = true
	err := <-s.closer
	if err == errClose {
		s.gracefulStop = true
	}
	close(s.closer)
	return err
}

func (s *service) GracefulStop() {
	s.closer <- errClose
}
