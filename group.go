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

// Package run implements an actor-runner with deterministic teardown.
// It uses the https://github.com/oklog/run/ package as its basis and enhances
// it with configuration registration and validation as well as pre-run phase
// logic.
package run

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	color "github.com/logrusorgru/aurora"
	"github.com/oklog/run"
	"github.com/spf13/pflag"
	"github.com/tetratelabs/multierror"
	"github.com/tetratelabs/telemetry"

	"github.com/tetratelabs/run/pkg/log"
	"github.com/tetratelabs/run/pkg/version"
)

// BinaryName holds the template variable that will be replaced by the Group
// name in HelpText strings.
const BinaryName = "{{.Name}}"

// Error allows for creating constant errors instead of sentinel ones.
type Error string

// Error implements error.
func (e Error) Error() string { return string(e) }

// ErrBailEarlyRequest is returned when a call to RunConfig was successful but
// signals that the application should exit in success immediately.
// It is typically returned on --version and --help requests that have been
// served. It can and should be used for custom config phase situations where
// the job of the application is done.
const ErrBailEarlyRequest Error = "exit request from flag handler"

// ErrRequestedShutdown can be used by Service implementations to gracefully
// request a shutdown of the application. Group will then exit without errors.
const ErrRequestedShutdown Error = "shutdown requested"

// FlagSet holds a pflag.FlagSet as well as an exported Name variable for
// allowing improved help usage information.
type FlagSet struct {
	*pflag.FlagSet
	Name string
}

// NewFlagSet returns a new FlagSet for usage in Config objects.
func NewFlagSet(name string) *FlagSet {
	return &FlagSet{
		FlagSet: pflag.NewFlagSet(name, pflag.ContinueOnError),
		Name:    name,
	}
}

// Unit is the default interface an object needs to implement for it to be able
// to register with a Group.
// Name should return a short but good identifier of the Unit.
type Unit interface {
	Name() string
}

// Initializer is an extension interface that Units can implement if they need
// to have certain properties initialized after creation but before any of the
// other lifecycle phases such as Config, PreRunner and/or Serve are run.
// Note, since an Initializer is a public function, make sure it is safe to be
// called multiple times.
type Initializer interface {
	// Unit is embedded for Group registration and identification
	Unit
	Initialize()
}

// Namer is an extension interface that Units can implement if they need to know
// or want to use the Group.Name. Since Group's name can be updated at runtime
// by the -n flag, Group first parses its own FlagSet the know if its Name needs
// to be updated and then runs the Name method on all Units implementing the
// Namer interface before handling the Units that implement Config. This allows
// these units to have the Name method be used to adjust the default values for
// flags or any other logic that uses the Group name to make decisions.
type Namer interface {
	GroupName(string)
}

// Config interface should be implemented by Group Unit objects that manage
// their own configuration through the use of flags.
// If a Unit's Validate returns an error it will stop the Group immediately.
type Config interface {
	// Unit is embedded for Group registration and identification
	Unit
	// FlagSet returns an object's FlagSet
	FlagSet() *FlagSet
	// Validate checks an object's stored values
	Validate() error
}

// PreRunner interface should be implemented by Group Unit objects that need
// a pre run stage before starting the Group Services.
// If a Unit's PreRun returns an error it will stop the Group immediately.
type PreRunner interface {
	// Unit is embedded for Group registration and identification
	Unit
	PreRun() error
}

// NewPreRunner takes a name and a standalone pre runner compatible function
// and turns them into a Group compatible PreRunner, ready for registration.
func NewPreRunner(name string, fn func() error) PreRunner {
	return preRunner{name: name, fn: fn}
}

type preRunner struct {
	name string
	fn   func() error
}

func (p preRunner) Name() string {
	return p.name
}

func (p preRunner) PreRun() error {
	return p.fn()
}

// Service interface should be implemented by Group Unit objects that need
// to run a blocking service until an error occurs or a shutdown request is
// made.
// The Serve method must be blocking and return an error on unexpected shutdown.
// Recoverable errors need to be handled inside the service itself.
// GracefulStop must gracefully stop the service and make the Serve call return.
//
// Since Service is managed by Group, it is considered a design flaw to call any
// of the Service methods directly in application code.
type Service interface {
	// Unit is embedded for Group registration and identification
	Unit
	// Serve starts the GroupService and blocks.
	Serve() error
	// GracefulStop shuts down and cleans up the GroupService.
	GracefulStop()
}

// Group builds on https://github.com/oklog/run to provide a deterministic way
// to manage service lifecycles. It allows for easy composition of elegant
// monoliths as well as adding signal handlers, metrics services, etc.
type Group struct {
	// Name of the Group managed service. If omitted, the binary name will be
	// used as found at runtime.
	Name string
	// HelpText is optional and allows to provide some additional help context
	// when --help is requested.
	HelpText string
	Logger   telemetry.Logger

	f *FlagSet
	r run.Group
	i []Initializer
	n []Namer
	c []Config
	p []PreRunner
	s []Service

	configured   bool
	hsRegistered bool
}

// Register will inspect the provided objects implementing the Unit interface to
// see if it needs to register the objects for any of the Group bootstrap
// phases. If a Unit doesn't satisfy any of the bootstrap phases it is ignored
// by Group.
// The returned array of booleans is of the same size as the amount of provided
// Units, signalling for each provided Unit if it successfully registered with
// Group for at least one of the bootstrap phases or if it was ignored.
func (g *Group) Register(units ...Unit) []bool {
	hasRegistered := make([]bool, len(units))
	for idx := range units {
		if i, ok := units[idx].(Initializer); ok {
			g.i = append(g.i, i)
			hasRegistered[idx] = true
		}
		if !g.configured {
			// if RunConfig has been called we can no longer register Config
			// phases of Units
			if n, ok := units[idx].(Namer); ok {
				g.n = append(g.n, n)
				hasRegistered[idx] = true
			}
			if c, ok := units[idx].(Config); ok {
				g.c = append(g.c, c)
				hasRegistered[idx] = true
			}
		}
		if p, ok := units[idx].(PreRunner); ok {
			g.p = append(g.p, p)
			hasRegistered[idx] = true
		}
		if s, ok := units[idx].(Service); ok {
			g.s = append(g.s, s)
			hasRegistered[idx] = true
		}
	}
	return hasRegistered
}

// Deregister will inspect the provided objects implementing the Unit interface
// to see if it needs to de-register the objects for any of the Group bootstrap
// phases.
// The returned array of booleans is of the same size as the amount of provided
// Units, signalling for each provided Unit if it successfully de-registered
// with Group for at least one of the bootstrap phases or if it was ignored.
// It is generally safe to use Deregister at any bootstrap phase except at Serve
// time (when it will have no effect).
// WARNING: Dependencies between Units can cause a crash as a dependent Unit
// might expect the other Unit to gone through all the needed bootstrapping
// phases.
func (g *Group) Deregister(units ...Unit) []bool {
	hasDeregistered := make([]bool, len(units))
	for idx := range units {
		for i := range g.i {
			if g.i[i] != nil && g.i[i].(Unit) == units[idx] {
				g.i[i] = nil // can't resize slice during Run, so nil
				hasDeregistered[idx] = true
			}
		}
		for i := range g.n {
			if g.n[i] != nil && g.n[i].(Unit) == units[idx] {
				g.n[i] = nil // can't resize slice during Run, so nil
				hasDeregistered[idx] = true
			}
		}
		for i := range g.c {
			if g.c[i] != nil && g.c[i].(Unit) == units[idx] {
				g.c[i] = nil // can't resize slice during Run, so nil
				hasDeregistered[idx] = true
			}
		}
		for i := range g.p {
			if g.p[i] != nil && g.p[i].(Unit) == units[idx] {
				g.p[i] = nil // can't resize slice during Run, so nil
				hasDeregistered[idx] = true
			}
		}
		for i := range g.s {
			if g.s[i] != nil && g.s[i].(Unit) == units[idx] {
				g.s[i] = nil // can't resize slice during Run, so nil
				hasDeregistered[idx] = true
			}
		}
	}
	return hasDeregistered
}

// RunConfig runs the Config phase of all registered Config aware Units.
// Only use this function if needing to add additional wiring between config
// and (pre)run phases and a separate PreRunner phase is not an option.
// In most cases it is best to use the Run method directly as it will run the
// Config phase prior to executing the PreRunner and Service phases.
// If an error is returned the application must shut down as it is considered
// fatal. In case the error is an ErrBailEarlyRequest the application
// should clean up and exit without an error code as an ErrBailEarlyRequest
// is not an actual error but a request for Help, Version or other task that has
// been finished and there is no more work left to handle.
func (g *Group) RunConfig(args ...string) (err error) {
	g.configured = true
	if g.Logger == nil {
		g.Logger = &log.Logger{}
	}

	if g.Name == "" {
		// use the binary name if custom name has not been provided
		g.Name = path.Base(os.Args[0])
	}

	g.HelpText = strings.ReplaceAll(g.HelpText, BinaryName, os.Args[0])

	defer func() {
		if err != nil && err != ErrBailEarlyRequest {
			g.Logger.Error("unexpected exit", err)
			err = multierror.SetFormatter(err, multierror.ListFormatFunc)
		}
	}()

	// run configuration stage
	g.f = NewFlagSet(g.Name)
	g.f.SortFlags = false // keep order of flag registration
	g.f.Usage = func() {
		fmt.Printf("Usage of %s:\n", g.Name)
		if g.HelpText != "" {
			fmt.Printf("%s\n", g.HelpText)
		}
		fmt.Printf("Flags:\n")
		g.f.PrintDefaults()
	}

	// register default rungroup flags
	var (
		name         string
		showHelp     bool
		showVersion  bool
		showRunGroup bool
	)

	gFS := NewFlagSet("Common Service options")
	gFS.SortFlags = false
	gFS.StringVarP(&name, "name", "n", g.Name, `name of this service`)
	gFS.BoolVarP(&showVersion, "version", "v", false,
		"show version information and exit.")
	gFS.BoolVarP(&showHelp, "help", "h", false,
		"show this help information and exit.")
	gFS.BoolVar(&showRunGroup, "show-rungroup-units", false, "show run group units")
	_ = gFS.MarkHidden("show-rungroup-units")
	g.f.AddFlagSet(gFS.FlagSet)

	// default to os.Args if args parameter was omitted
	if len(args) == 0 {
		args = os.Args[1:]
	}

	// parse our run group flags only (not the plugin ones)
	_ = gFS.Parse(args)
	if name != "" {
		g.Name = name
	}

	// initialize all Units implementing Initializer
	for idx, i := range g.i {
		// an Initializer might have been de-registered
		if i != nil {
			i.Initialize()
			// don't call in Run phase again
			g.i[idx] = nil
		}
	}

	// inform all Units implementing Namer of the parsed Group name
	for _, n := range g.n {
		// a Namer might have been de-registered
		if n != nil {
			n.GroupName(g.Name)
		}
	}

	// register flags from attached Config objects
	fs := make([]*FlagSet, len(g.c))
	for idx := range g.c {
		// a Config might have been de-registered
		if g.c[idx] == nil {
			continue
		}
		g.Logger.Debug("registering flags",
			"name", g.c[idx].Name(),
			"index", fmt.Sprintf("(%d/%d)", idx+1, len(g.c)),
		)
		fs[idx] = g.c[idx].FlagSet()
		if fs[idx] == nil {
			// no FlagSet returned
			g.Logger.Debug("config object did not return a flagset", "index", idx)
			continue
		}
		fs[idx].VisitAll(func(f *pflag.Flag) {
			if g.f.Lookup(f.Name) != nil {
				g.Logger.Debug("ignoring duplicate flag", "name", f.Name, "index", idx)
				return
			}
			g.f.AddFlag(f)
		})
	}

	// parse FlagSet and exit on error
	if err = g.f.Parse(args); err != nil {
		return err
	}

	// bail early on help or version requests
	switch {
	case showHelp:
		fmt.Println(color.Cyan(color.Bold(fmt.Sprintf("Usage of %s:", g.Name))))
		if g.HelpText != "" {
			fmt.Printf("%s\n", g.HelpText)
		}
		fmt.Printf("%s\n\n", color.Cyan(color.Bold("Flags:")))
		fmt.Printf("%s\n%s\n", color.Cyan("* "+gFS.Name), gFS.FlagUsages())
		for _, f := range fs {
			if f != nil {
				fmt.Printf("%s\n%s\n", color.Cyan("* "+f.Name), f.FlagUsages())
			}
		}
		return ErrBailEarlyRequest
	case showVersion:
		version.Show(g.Name)
		return ErrBailEarlyRequest
	case showRunGroup:
		fmt.Println(g.ListUnits())
		return ErrBailEarlyRequest
	}

	// Validate Config inputs
	for idx := range g.c {
		// a Config might have been de-registered during Run
		if g.c[idx] == nil {
			g.Logger.Debug("skipping validate", "index", idx)
			continue
		}
		g.Logger.Debug("validate config",
			"name", g.c[idx].Name(),
			fmt.Sprintf("(%d/%d)", idx+1, len(g.c)),
		)
		if vErr := g.c[idx].Validate(); vErr != nil {
			err = multierror.Append(err, vErr)
		}
	}

	// exit on at least one Validate error
	if err != nil {
		return err
	}

	// log binary name and version
	g.Logger.Info(g.Name + " " + version.Parse() + " started")

	return nil
}

// Run will execute all phases of all registered Units and block until an error
// occurs.
// If RunConfig has been called prior to Run, the Group's Config phase will be
// skipped and Run continues with the PreRunner and Service phases.
//
// The following phases are executed in the following sequence:
//
//   Initialization phase (serially, in order of Unit registration)
//     - Initialize()     Initialize Unit's supporting this interface.
//
//   Config phase (serially, in order of Unit registration)
//     - FlagSet()        Get & register all FlagSets from Config Units.
//     - Flag Parsing     Using the provided args (os.Args if empty)
//     - Validate()       Validate Config Units. Exit on first error.
//
//   PreRunner phase (serially, in order of Unit registration)
//     - PreRun()         Execute PreRunner Units. Exit on first error.
//
//   Service phase (concurrently)
//     - Serve()          Execute all Service Units in separate Go routines.
//     - Wait             Block until one of the Serve() methods returns
//     - GracefulStop()   Call interrupt handlers of all Service Units.
//
//   Run will return with the originating error on:
//   - first Config.Validate()  returning an error
//   - first PreRunner.PreRun() returning an error
//   - first Service.Serve()    returning (error or nil)
//
// Note: it is perfectly acceptable to use Group without Service units. In this
// case Run will just return immediately after having handled the Config and
// PreRunner phases of the registered Units. This is particularly convenient if
// using the common pkg middlewares in a CLI / ephemeral environment.
func (g *Group) Run(args ...string) (err error) {
	if !g.configured {
		// run config registration and flag parsing stages
		if err = g.RunConfig(args...); err != nil {
			if err == ErrBailEarlyRequest {
				return nil
			}
			return err
		}
	}

	var hasServices bool

	defer func() {
		if err == nil {
			// Registered services should never initiate an exit without an
			// error. Services allowing intended shutdowns must use the
			// ErrRequestShutdown error (or wrap it) to signal intent.
			// If Group is used without services (e.g. PreRunner scripts) this
			// is fine.
			if hasServices {
				err = errors.New("run terminated without explicit error condition")
				g.Logger.Error("unexpected exit", err)
				return
			}
			g.Logger.Info("done")
			return
		}
		// test if this is a requested / expected shutdown...
		if errors.Is(err, ErrRequestedShutdown) {
			g.Logger.Info("received shutdown request")
			err = nil
			return
		}
		// actual fatal error
		g.Logger.Error("unexpected exit", err)
		err = multierror.SetFormatter(err, multierror.ListFormatFunc)
	}()

	// call our Initializer (again)
	// In case a Unit was registered for PreRun and/or Serve phase after Config
	// phase was completed, we still want to run the Initializer if existent.
	for _, i := range g.i {
		// an Initializer might have been de-registered
		if i != nil {
			i.Initialize()
		}
	}

	// execute pre run stage and exit on error
	for idx := range g.p {
		// a PreRunner might have been de-registered during Run
		if g.p[idx] == nil {
			continue
		}
		g.Logger.Debug("pre-run",
			"name", g.p[idx].Name(),
			fmt.Sprintf("(%d/%d)", idx+1, len(g.p)),
		)
		if err := g.p[idx].PreRun(); err != nil {
			return fmt.Errorf("pre-run %s: %w", g.p[idx].Name(), err)
		}
	}

	// feed our registered services to our internal run.Group
	for idx := range g.s {
		// a Service might have been de-registered during Run
		s := g.s[idx]
		if s == nil {
			continue
		}
		hasServices = true
		g.Logger.Debug("serve",
			"name", s.Name(),
			fmt.Sprintf("(%d/%d)", idx+1, len(g.s)),
		)
		g.r.Add(func() error {
			return s.Serve()
		}, func(_ error) {
			g.Logger.Debug("stop",
				"name", s.Name(),
				fmt.Sprintf("(%d/%d)", idx+1, len(g.s)),
			)
			s.GracefulStop()
		})
	}

	// start registered services and block
	return g.r.Run()
}

// ListUnits returns a list of all Group phases and the Units registered to each
// of them.
func (g Group) ListUnits() string {
	var (
		s string
		t = "cli"
	)

	if len(g.i) > 0 {
		s += "\n - initialize: "
		for _, u := range g.i {
			if u != nil {
				s += u.Name() + " "
			}
		}
	}
	if len(g.c) > 0 {
		s += "\n- config: "
		for _, u := range g.c {
			if u != nil {
				s += u.Name() + " "
			}
		}
	}
	if len(g.p) > 0 {
		s += "\n- pre-run: "
		for _, u := range g.p {
			if u != nil {
				s += u.Name() + " "
			}
		}
	}
	if len(g.s) > 0 {
		s += "\n- serve : "
		for _, u := range g.s {
			if u != nil {
				t = "svc"
				s += u.Name() + " "
			}
		}
	}

	return fmt.Sprintf("Group: %s [%s]%s", g.Name, t, s)
}
