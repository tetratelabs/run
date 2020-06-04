// Copyright (c) Tetrate, Inc 2020 All Rights Reserved.

package run_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tetratelabs/multierror"

	"github.com/tetrateio/tetrate/pkg"
	"github.com/tetrateio/tetrate/pkg/run"
	"github.com/tetrateio/tetrate/pkg/test/group"
)

const (
	errFlags = pkg.Error("flagset error")
	errClose = pkg.Error("requested close")
	errIRQ   = pkg.Error("interrupt")
)

func TestRunGroupSvcLifeCycle(t *testing.T) {
	var (
		g       run.Group
		s       service
		irq     = make(chan error)
		hasName bool
	)

	// add our service to Group
	g.Register(&s)

	// add our interruptor
	g.Register(&group.TestSvc{
		SvcName: "testsvc",
		Execute: func() error {
			hasName = len(g.Name) > 0
			return errIRQ
		},
	})

	// start Group
	go func() { irq <- g.Run("./myService", "-f", "1") }()

	select {
	case err := <-irq:
		if err != errIRQ {
			t.Errorf("Expected proper close, got %v", err)
		}
		if s.groupName != g.Name {
			t.Error("Expected namer logic to run")
		}
		if s.initializer < 1 {
			t.Error("Expected initializer logic to run")
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
		if !hasName {
			t.Errorf("Expected valid name from env")
		}
	case <-time.After(100 * time.Millisecond):
		t.Errorf("timeout")
	}
}

func TestRunGroupMultiErrorHandling(t *testing.T) {
	var (
		g = run.Group{Name: "MyService"}

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

	g.Register(cfg1, cfg2, cfg3)

	if want, have := mErr.Error(), g.Run().Error(); want != have {
		t.Errorf("invalid error payload returned:\nwant:\n%+v\nhave:\n%+v\n", want, have)
	}
}

func TestRunGroupEarlyBailFlags(t *testing.T) {
	var (
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
		g := run.Group{HelpText: "placeholder"}

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
		g     run.Group
		flag1 flagTestConfig
		flag2 flagTestConfig
		irq   = make(chan error)
	)

	// add our flags
	g.Register(&flag1, &flag2)

	// add our interruptor
	g.Register(&group.TestSvc{
		SvcName: "irqsvc",
		Execute: func() error { return errIRQ },
	})

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

func TestRuntimeDeregister(t *testing.T) {
	for _, svcs := range [][]string{
		[]string{"--s1-disable"},
		[]string{"--s2-disable"},
		[]string{"--s1-disable", "--s2-disable"},
	} {
		for _, phase := range []string{"config", "prerunner", "service"} {
			var (
				g          run.Group
				s1, s2, s3 service
				d1, d2     bool
				disabler   disablerService
				irq        = make(chan error)
				idx        = fmt.Sprintf("%s(%s)", phase, strings.Join(svcs, ","))
			)

			s1.customFlags = run.NewFlagSet("s1-disabler")
			s1.customFlags.BoolVar(&d1, "s1-disable", false, "disable service 1")
			s1.configItem = 1
			s2.customFlags = run.NewFlagSet("s2-disabler")
			s2.customFlags.BoolVar(&d2, "s2-disable", false, "disable service 2")
			s2.configItem = 1

			g.Register(&disabler, &s1, &s2, &s3)
			g.Deregister(&s3) // make sure we also handle deregister before calling Run

			switch phase {
			case "config":
				disabler.config = func() {
					if d1 {
						if dereg := g.Deregister(&s1); dereg[0] == false {
							t.Errorf("%s: deregister want: true, have: %t", idx, dereg[0])
						}
						s1.disabled.config = true
						s1.disabled.preRun = true
						s1.disabled.serve = true
					}
					if d2 {
						if dereg := g.Deregister(&s2); dereg[0] == false {
							t.Errorf("%s: deregister want: true, have: %t", idx, dereg[0])
						}
						s2.disabled.config = true
						s2.disabled.preRun = true
						s2.disabled.serve = true
					}
				}
			case "prerunner":
				disabler.prerunner = func() {
					if d1 {
						if dereg := g.Deregister(&s1); dereg[0] == false {
							t.Errorf("%s: deregister want: true, have: %t", idx, dereg[0])
						}
						s1.disabled.preRun = true
						s1.disabled.serve = true
					}
					if d2 {
						if dereg := g.Deregister(&s2); dereg[0] == false {
							t.Errorf("%s: deregister want: true, have: %t", idx, dereg[0])
						}
						s2.disabled.preRun = true
						s2.disabled.serve = true
					}
				}
			case "service":
				g.Register(run.NewPreRunner("service-disabler", func() error {
					if d1 {
						if dereg := g.Deregister(&s1); dereg[0] == false {
							t.Errorf("%s: deregister want: true, have: %t", idx, dereg[0])
						}
						s1.disabled.serve = true
					}
					if d2 {
						if dereg := g.Deregister(&s2); dereg[0] == false {
							t.Errorf("%s: deregister want: true, have: %t", idx, dereg[0])
						}
						s2.disabled.serve = true
					}
					return nil
				}))
			}

			g.Register(&group.TestSvc{
				SvcName: "testsvc",
				Execute: func() error { return errIRQ },
			})

			// start Group
			go func() { irq <- g.Run(append([]string{"./myService"}, svcs...)...) }()

			select {
			case err := <-irq:
				if err != errIRQ {
					t.Errorf("Expected proper close, got %v", err)
				}

				if want, have := !s1.disabled.config, s1.validated; want != have {
					t.Errorf("%s: s1 config want: %t, have: %t", idx, want, have)
				}
				if want, have := !s1.disabled.preRun, s1.preRun; want != have {
					t.Errorf("%s: s1 prerun want: %t, have: %t", idx, want, have)
				}
				if want, have := !s1.disabled.serve, s1.serve && s1.gracefulStop; want != have {
					t.Errorf("%s: s1 serve want: %t, have: %t", idx, want, have)
				}
				if want, have := !s2.disabled.config, s2.validated; want != have {
					t.Errorf("%s: s2 config want: %t, have: %t", idx, want, have)
				}
				if want, have := !s2.disabled.preRun, s2.preRun; want != have {
					t.Errorf("%s: s2 prerun want: %t, have: %t", idx, want, have)
				}
				if want, have := !s2.disabled.serve, s2.serve && s2.gracefulStop; want != have {
					t.Errorf("%s: s2 serve want: %t, have: %t", idx, want, have)
				}

			case <-time.After(100 * time.Millisecond):
				t.Errorf("timeout")
			}

		}
	}
}

type flagTestConfig struct {
	value int
}

func (f flagTestConfig) Name() string {
	return fmt.Sprintf("flagtest%d", f.value)
}

func (f *flagTestConfig) FlagSet() *run.FlagSet {
	flags := run.NewFlagSet("flag test config")
	flags.IntVarP(&f.value, "flagtest", "f", 10, "flagtester")
	return flags
}

func (f flagTestConfig) Validate() error { return nil }

type failingConfig struct {
	e error
}

func (f failingConfig) Name() string {
	return f.e.Error()
}

func (f failingConfig) FlagSet() *run.FlagSet { return nil }

func (f failingConfig) Validate() error { return f.e }

var (
	_ run.Unit        = (*service)(nil)
	_ run.Initializer = (*service)(nil)
	_ run.Namer       = (*service)(nil)
	_ run.Config      = (*service)(nil)
	_ run.PreRunner   = (*service)(nil)
	_ run.Service     = (*service)(nil)
)

type service struct {
	configItem   int
	groupName    string
	initializer  int
	flagSet      bool
	validated    bool
	preRun       bool
	serve        bool
	gracefulStop bool
	disabled     struct {
		config bool
		preRun bool
		serve  bool
	}
	closer      chan error
	customFlags *run.FlagSet
}

func (s service) Name() string {
	return "testsvc"
}

func (s *service) GroupName(name string) {
	s.groupName = name
}

func (s *service) Initialize() {
	s.initializer++
}

func (s *service) FlagSet() *run.FlagSet {
	s.flagSet = true
	if s.customFlags != nil {
		return s.customFlags
	}
	flags := run.NewFlagSet("dummy flagset")
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

var (
	_ run.Unit      = (*disablerService)(nil)
	_ run.Config    = (*disablerService)(nil)
	_ run.PreRunner = (*disablerService)(nil)
)

type disablerService struct {
	q         chan error
	config    func()
	prerunner func()
}

func (d disablerService) Name() string {
	return "disablerService"
}

func (d disablerService) FlagSet() *run.FlagSet {
	return run.NewFlagSet("dummy flagset")
}

func (d *disablerService) Validate() error {
	d.q = make(chan error)
	if d.config != nil {
		d.config()
	}
	return nil
}

func (d *disablerService) PreRun() error {
	if d.prerunner != nil {
		d.prerunner()
	}
	return nil
}
