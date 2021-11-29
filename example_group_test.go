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
	"errors"
	"fmt"
	"os"

	"github.com/spf13/pflag"
	"github.com/tetratelabs/multierror"

	"github.com/tetratelabs/run"
	"github.com/tetratelabs/run/pkg/signal"
)

func Example() {
	var (
		g run.Group
		p PersonService
		s signal.Handler
	)

	// add our PersonService
	g.Register(&p)

	// add a SignalHandler service
	g.Register(&s)

	// Start our services and block until error or exit request.
	// If sending a SIGINT to the process, a graceful shutdown of the
	// application will occur.
	err := g.Run()
	fmt.Printf("Service Exit: %v\n", err)
	if !errors.Is(err, signal.ErrSignal) {
		// we had an actual fatal error
		os.Exit(-1)
	}
}

// PersonService implements run.Config, run.PreRunner and run.GroupService to
// show a fully managed service lifecycle.
type PersonService struct {
	name string
	age  int

	closer chan error
}

func (p PersonService) Name() string {
	return "person"
}

// FlagSet implements run.Config and thus its configuration and flag handling is
// automatically registered when adding the service to Group.
func (p *PersonService) FlagSet() *pflag.FlagSet {
	flags := pflag.NewFlagSet("PersonService's flags", pflag.ContinueOnError)

	flags.StringVarP(&p.name, "name", "-n", "john doe", "name of person")
	flags.IntVarP(&p.age, "age", "a", 42, "age of person")

	return flags
}

// Validate implements run.Config and thus its configuration and flag handling
// is automatically registered when adding the service to Group.
func (p PersonService) Validate() error {
	var err error
	if p.name == "" {
		err = multierror.Append(err, errors.New("invalid name provided"))
	}
	if p.age < 18 {
		err = multierror.Append(err, errors.New("invalid age provided, we don't serve minors"))
	}
	if p.age > 110 {
		err = multierror.Append(err, errors.New("faking it? or life expectancy assumptions surpassed by future healthcare system"))
	}

	return err
}

// PreRun implements run.PreRunner and thus this method is run at the pre-run
// stage of Group before starting any of the services.
func (p *PersonService) PreRun() error {
	p.closer = make(chan error)
	return nil
}

// Serve implements run.GroupService and is executed at the service run phase of
// Group in order of registration. All Serve methods must block until requested
// to Stop or needing to fatally error.
func (p PersonService) Serve() error {
	<-p.closer
	return nil
}

// GracefulStop implements run.GroupService and is executed at the shutdown
// phase of Group.
func (p PersonService) GracefulStop() {
	close(p.closer)
}
