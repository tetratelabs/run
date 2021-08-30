// Copyright (c) Tetrate, Inc 2021 All Rights Reserved.

package run

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"

	l "github.com/tetratelabs/log"
	"github.com/tetratelabs/multierror"

	"github.com/tetrateio/tetrate/pkg"
	"github.com/tetrateio/tetrate/pkg/health"
)

const marshallErr = `{"code":"No Service Operational","services":{"health":{"code":"Error marshalling status"}}}`

var (
	_ Service        = (*healthService)(nil)
	_ Config         = (*healthService)(nil)
	_ PreRunner      = (*healthService)(nil)
	_ health.Checker = (*healthService)(nil)
	_ http.Handler   = (*healthService)(nil)

	log = l.RegisterScope("health", "Messages from health check service", 0)
)

type (
	// healthService implements run.Service in order to start a service handling health check requests.
	//
	// It inspects all the services in the same group it's registered and holds all those that implements health.Checker
	// to retrieve their health status.
	// Also implements health.Checker itself to provide its own status.
	healthService struct {
		checkers map[string]health.Checker
		server   *http.Server
		// abstracts net.Listen(protocol, address) for testing
		listen func() (net.Listener, error)

		// config
		address  string
		endpoint string
		status   atomic.Value
	}
)

// Name implements run.Unit.
func (*healthService) Name() string {
	return "health"
}

// PreRun implements run.PreRunner.
func (s *healthService) PreRun() error {
	s.status.Store(health.Initializing)
	s.checkers = make(map[string]health.Checker)
	if s.listen == nil {
		s.listen = func() (net.Listener, error) {
			return net.Listen("tcp", s.address)
		}
	}
	return nil
}

const (
	addressFlag     = "health-address"
	endpointFlag    = "health-endpoint"
	defaultAddress  = ":9082"
	defaultEndpoint = "/health"
)

// FlagSet implements run.Config.
func (s *healthService) FlagSet() *FlagSet {
	f := NewFlagSet("Health check service")

	f.StringVar(&s.address, addressFlag, defaultAddress, `Address to host health check service; just a port, e.g. ":8080", works`)
	f.StringVar(&s.endpoint, endpointFlag, defaultEndpoint, `HTTP endpoint to host health check service: string path, e.g. "/health"`)

	return f
}

// Validate implements run.Config.
func (s healthService) Validate() error {
	var err error
	if s.address == "" {
		err = multierror.Append(err, fmt.Errorf(pkg.FlagErr, addressFlag, pkg.ErrRequired))
	}
	if s.endpoint == "" {
		err = multierror.Append(err, fmt.Errorf(pkg.FlagErr, endpointFlag, pkg.ErrRequired))
	}
	return err
}

// Register takes a unit and if it implements health.Checker then saves it to track its health status
func (s *healthService) register(u Unit) {
	if c, ok := u.(health.Checker); ok {
		s.checkers[u.Name()] = c
		log.Debugf("Health checker %q (%T) registered", u.Name(), c)
	}
}

// Serve implements run.Service.
//
// Starts a server exposing the `/health` path to get access to the health status of the service.
func (s *healthService) Serve() error {
	log.Debugf("%d health checkers registered", len(s.checkers))

	m := http.NewServeMux()
	m.Handle(s.endpoint, s)
	s.server = &http.Server{Handler: m}

	listener, err := s.listen()
	if err != nil {
		return fmt.Errorf("unable to start health check service on %s%s: %w",
			s.address, s.endpoint, err)
	}

	log.Infof("Starting Health Check Service at %s%s", s.address, s.endpoint)
	s.status.Store(health.Running)
	return s.server.Serve(listener)
}

// GracefulStop implements run.Service.
func (s *healthService) GracefulStop() {
	log.Debugf("Shutting down Health Check Service from %s%s", s.address, s.endpoint)
	s.status.Store(health.ShuttingDown)
	if s.server != nil {
		_ = s.server.Shutdown(context.Background())
	}
}

// Health implements health.Checker.
func (s healthService) Health() health.ServiceStatus {
	return health.ServiceStatus{Code: s.status.Load().(health.ServiceStatusCode)}
}

// ServeHTTP implements http.Handler
func (s healthService) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	var (
		httpStatusCode int
		bytes          []byte
		err            error
	)

	status := s.checkServices()

	switch status.Code {
	case health.AllDown, health.Partial:
		httpStatusCode = http.StatusServiceUnavailable
	default:
		httpStatusCode = http.StatusOK
	}

	if bytes, err = json.Marshal(status); err != nil {
		log.Errorf("Error marshalling status: %v", err)
		httpStatusCode = http.StatusInternalServerError
		bytes = []byte(marshallErr)
	}

	w.WriteHeader(httpStatusCode)
	if _, err := w.Write(bytes); err != nil {
		log.Errorf("Error writing response: %v", err)
	}
}

// checkServices invokes all the health.Checker instances and returns the result.
// This is expected to be called always after the `healthService.PreRun()` method.
func (s healthService) checkServices() health.Status {
	serviceStatuses := make(map[string]health.ServiceStatus, len(s.checkers))

	var healthyServices int
	for name, checker := range s.checkers {
		st := checker.Health()
		if st.Code == health.Running {
			healthyServices++
		}
		serviceStatuses[name] = st
	}

	var code health.StatusCode
	switch {
	case healthyServices == len(s.checkers):
		code = health.AllUp
	case healthyServices == 0:
		code = health.AllDown
	default:
		code = health.Partial
	}

	return health.Status{
		Code:     code,
		Services: serviceStatuses,
	}
}
