// Copyright (c) Tetrate, Inc 2019 All Rights Reserved.

// Package run implements an actor-runner with deterministic teardown.
// It uses the https://github.com/oklog/run/ package as its basis and enhances
// it with configuration registration and validation as well as pre-run phase
// logic.
package run

import (
	"fmt"
	"os"

	"github.com/oklog/run"
	"github.com/spf13/pflag"
	l "github.com/tetratelabs/log"
	"github.com/tetratelabs/multierror"

	"github.com/tetrateio/tetrate/pkg/version"
)

var log = l.RegisterScope("rungroup", "Messages from the RunGroup handler", 0)

// GroupService implementations can be added to our Group logic.
// The Serve method must be blocking and return an error on unexpected shutdown.
// Recoverable errors need to be handled inside the service itself.
// GracefulStop must gracefully stop the service and make the Serve call return.
type GroupService interface {
	// Serve starts the GroupService and blocks
	Serve() error
	// GracefulStop shuts down and cleans up the GroupService
	GracefulStop()
}

// PreRunner is an extension interface for a GroupService implementation.
// A PreRunner implementation will have its PreRun function be added to the
// Group pre-run start logic. A PreRun returning an error will stop the Group
// immediately.
type PreRunner interface {
	PreRun() error
}

// PreRunFunc is a stand alone function executed before the program's primary
// services are started. If a PreMainFunc returns an error the entire program is
// aborted and we exit in error.
type PreRunFunc func() error

// Config interface should be implemented by Config objects and GroupService
// objects that manage their own configuration.
type Config interface {
	// FlagSet returns an object's FlagSet
	FlagSet() *pflag.FlagSet
	// Validate checks an object's stored values
	Validate() error
}

// Group builds on https://github.com/oklog/run to provide a deterministic way
// to manage service lifecycles. It allows for easy composition of elegant
// monoliths as well as adding signal handlers, metrics services, etc.
type Group struct {
	// Name of the Group managed service. If omitted, the binaryname will be
	// used as found at runtime.
	Name string
	// HelpText is optional and allows to provide some additional help context
	// when --help is requested.
	HelpText string

	f *pflag.FlagSet
	r run.Group
	c []Config
	p []PreRunFunc
}

// AddConfig adds Config objects to the configuration stage of Group.
// It will register the Config's flags to the internal Group flagSet as well as
// run Config validation before proceeding to the PreRun stage.
func (g *Group) AddConfig(cfg ...Config) {
	g.c = append(g.c, cfg...)
}

// AddPreRun allows stand alone PreRunFunc methods to register themselves to the
// Group's bootstrap cycle.
func (g *Group) AddPreRun(fn ...PreRunFunc) {
	g.p = append(g.p, fn...)
}

// AddService registers a GroupService compatible service to the Group.
// If the service implements the Config interface, it is added to the Group's
// configuration and flag handling phase. If the service also implements the
// PreRunner interface, it is added to the application's pre-run phase.
func (g *Group) AddService(svcs ...GroupService) {
	for _, svc := range svcs {
		// if svc implements Config, add to our Config list
		if c, ok := svc.(Config); ok {
			g.AddConfig(c)
		}

		// if svc implements the PreRunner interface, add to our pre-run list
		if pr, ok := svc.(PreRunner); ok {
			g.AddPreRun(pr.PreRun)
		}

		// add the GroupService to our Group
		g.r.Add(func() error {
			return svc.Serve()
		}, func(_ error) {
			svc.GracefulStop()
		})
	}
}

// Add allows custom Group execute and interrupt functions to be used instead
// of the default GroupService method ones. This is convenient for custom
// startup / shutdown logic of a service or when needing to manage a service
// that doesn't satisfy the GroupService interface.
func (g *Group) Add(execute func() error, interrupt func(error)) {
	g.r.Add(execute, interrupt)
}

// Run will execute all registered GroupServices.
// All registered Config objects and GroupService objects implementing the
// Config interface will be requested to provide their configuration flags.
// These are then added to the internal Group flagSet. After successful flag
// parsing the Config Validate methods will be run to identify value validation
// errors. If none are found the registered PreRun methods will be called and if
// all are successful the registered GroupService's Serve methods will be
// executed. When done Run will block until at least one GroupService's Serve
// method returns (with or without error). Once a GroupService Serve method has
//  returned, all registered GroupServices will receive a call to their
// GracefulStop methods and Run will return the originating error to the caller
// (or nil if it was a proper shutdown).
func (g *Group) Run(args ...string) (err error) {
	defer func() {
		if err != nil {
			log.Errorf("unexpected exit: %v", err)
			err = multierror.SetFormatter(err, multierror.ListFormatFunc)
		}
	}()
	// use the binary name if custom name has not been provided
	if g.Name == "" {
		g.Name = os.Args[0]
	}

	// run configuration stage
	g.f = pflag.NewFlagSet(g.Name, pflag.ContinueOnError)
	g.f.SortFlags = false // keep order of flag registration
	g.f.Usage = func() {
		fmt.Printf("Usage of %s:\n", g.Name)
		if g.HelpText != "" {
			fmt.Printf("%s\n", g.HelpText)
		}
		fmt.Printf("Flags:\n")
		g.f.PrintDefaults()
	}

	// register flags from attached Config objects
	for idx := range g.c {
		fs := g.c[idx].FlagSet()
		if fs == nil {
			// no FlagSet returned
			log.Debugf("configuration object did not return a flagset [%d]", idx)
			continue
		}
		fs.VisitAll(func(f *pflag.Flag) {
			if g.f.Lookup(f.Name) != nil {
				// log duplicate flag
				log.Warnf("ignoring duplicate flag: %s [%d]", f.Name, idx)
				return
			}
			g.f.AddFlag(f)
		})
	}

	// register default help and version flags
	var showHelp, showVersion bool
	g.f.BoolVarP(&showHelp, "help", "h", false,
		"show this help information and exit.")
	g.f.BoolVarP(&showVersion, "version", "v", false,
		"show version information and exit.")

	// default to os.Args if args parameter was omitted
	if len(args) == 0 {
		args = os.Args[1:]
	}

	// parse FlagSet and exit on error
	if err = g.f.Parse(args); err != nil {
		return err
	}

	// bail early on help or version requests
	switch {
	case showHelp:
		g.f.Usage()
		return nil
	case showVersion:
		version.Show(g.Name)
		return nil
	}

	// Validate Config inputs
	for _, c := range g.c {
		if vErr := c.Validate(); vErr != nil {
			err = multierror.Append(err, vErr)
		}
	}

	// exit on at least one Validate error
	if err != nil {
		return err
	}

	// execute pre run stage and exit on error
	for _, p := range g.p {
		if err := p(); err != nil {
			return err
		}
	}
	// start registered services and block
	return g.r.Run()
}
