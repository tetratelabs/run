// Copyright (c) Tetrate, Inc 2021 All Rights Reserved.

package run

import (
	"encoding/json"
	"io/ioutil"
	"net"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/tetrateio/tetrate/pkg/health"
	tnet "github.com/tetrateio/tetrate/pkg/test/net"
)

func TestHealthServiceStatus(t *testing.T) {
	cases := []struct {
		name       string
		services   []*testChecker
		expected   health.Status
		statusCode int
	}{
		{
			"1-svc-running",
			[]*testChecker{{"HCS", health.Running}},
			health.Status{
				Code: health.AllUp,
				Services: map[string]health.ServiceStatus{
					"HCS": {Code: health.Running},
				},
			},
			200,
		},
		{
			"1-svc-initializing",
			[]*testChecker{{"HCS", health.Initializing}},
			health.Status{
				Code: health.AllDown,
				Services: map[string]health.ServiceStatus{
					"HCS": {Code: health.Initializing},
				},
			},
			503,
		},
		{
			"1-svc-shuttingdown",
			[]*testChecker{{"HCS", health.ShuttingDown}},
			health.Status{
				Code: health.AllDown,
				Services: map[string]health.ServiceStatus{
					"HCS": {Code: health.ShuttingDown},
				},
			},
			503,
		},
		{
			"all-svcs-running",
			[]*testChecker{
				{"HCS-0", health.Running},
				{"HCS-1", health.Running},
				{"HCS-2", health.Running},
			},
			health.Status{
				Code: health.AllUp,
				Services: map[string]health.ServiceStatus{
					"HCS-0": {Code: health.Running},
					"HCS-1": {Code: health.Running},
					"HCS-2": {Code: health.Running},
				},
			},
			200,
		},
		{
			"some-svcs-running",
			[]*testChecker{
				{"HCS-0", health.Initializing},
				{"HCS-1", health.Running},
				{"HCS-2", health.Running}},
			health.Status{
				Code: health.Partial,
				Services: map[string]health.ServiceStatus{
					"HCS-0": {Code: health.Initializing},
					"HCS-1": {Code: health.Running},
					"HCS-2": {Code: health.Running},
				},
			},
			503,
		},
		{
			"all-svcs-not-running",
			[]*testChecker{
				{"HCS-0", health.Initializing},
				{"HCS-1", health.ShuttingDown},
				{"HCS-2", health.ShuttingDown}},
			health.Status{
				Code: health.AllDown,
				Services: map[string]health.ServiceStatus{
					"HCS-0": {Code: health.Initializing},
					"HCS-1": {Code: health.ShuttingDown},
					"HCS-2": {Code: health.ShuttingDown},
				},
			},
			503,
		},
	}

	for _, tc := range cases {
		t.Run(string(tc.expected.Code), func(tt *testing.T) {
			// initialize and start the server
			l := tnet.InMemoryListener()
			h := healthService{
				address:  "localhost:9009",
				endpoint: "/health",
				listen: func() (net.Listener, error) {
					return l, nil
				},
			}
			tt.Cleanup(h.GracefulStop)

			if err := h.PreRun(); err != nil {
				t.Fatalf("could not initialize health check service for test. Error: %v", err)
			}

			// register test case services
			for _, hcs := range tc.services {
				h.register(hcs)
			}

			go func() { _ = h.Serve() }()

			c := l.HTTPClient()

			resp, err := c.Get("http://localhost:9009/health")
			if err != nil {
				tt.Fatalf("Unexpected error performing health request: %v", err)
			}
			if resp.StatusCode != tc.statusCode {
				tt.Errorf("GET /health = %d, want %d", resp.StatusCode, tc.statusCode)
			}

			var body []byte
			if body, err = ioutil.ReadAll(resp.Body); err != nil {
				tt.Fatalf("Unexpected error reading body response: %v", err)
			}

			var got health.Status
			if err := json.Unmarshal(body, &got); err != nil {
				tt.Fatalf("Unexpected error unmarshalling body response: %v", err)
			}

			if diff := cmp.Diff(tc.expected, got); diff != "" {
				tt.Errorf("Health status payload does not match (-want,+got): %s", diff)
			}

		})
	}
}

func TestHealthService_MarshallErrorRespectsModel(t *testing.T) {
	// check hardcoded error matches the status model in order to not break external clients
	got := &health.Status{}
	if err := json.Unmarshal([]byte(marshallErr), got); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	// marshal again in order to compare strings
	var (
		bytes []byte
		err   error
	)
	if bytes, err = json.Marshal(got); err != nil {
		t.Fatalf("Unexpected error when marshalling the unmarshalled error: %v", err)
	}
	// original and unmarshalled and re-marshalled must match
	if diff := cmp.Diff(marshallErr, string(bytes)); diff != "" {
		t.Fatalf("Error payload does not match (-want,+got):  %s", diff)
	}
}

func TestHealthService_Registration(t *testing.T) {
	// set up health checker
	l := tnet.InMemoryListener()
	h := &healthService{
		address:  "localhost:9009",
		endpoint: "/health",
		listen: func() (net.Listener, error) {
			return l, nil
		},
	}

	t.Cleanup(func() {
		h.GracefulStop()
	})

	service := testService{
		testChecker: testChecker{name: "service", serviceStatus: health.Running},
		started:     make(chan struct{}),
		done:        make(chan struct{}),
	}
	// manually register the hs in order to be able to use in memory listener
	g := &Group{h: h, hsRegistered: true}
	g.Register(
		h,
		testPreRun{testChecker: testChecker{name: "prerunner", serviceStatus: health.Running}},
		testPreRun{testChecker: testChecker{name: "prerunner-2", serviceStatus: health.Failing}},
		service,
	)

	// start the run.Group lifecycle
	go func() { _ = g.Run() }()

	// make sure all services are running, they start consecutively
	<-service.started

	c := l.HTTPClient()
	resp, err := c.Get("http://localhost:9009/health")
	if err != nil {
		t.Fatalf("Unexpected error performing health request: %v", err)
	}
	if resp.StatusCode != 503 { // Partial Service Failures
		t.Fatalf("GET /health = %d, want 503", resp.StatusCode)
	}

	var body []byte
	if body, err = ioutil.ReadAll(resp.Body); err != nil {
		t.Fatalf("Unexpected error reading body response: %v", err)
	}

	var got health.Status
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("Unexpected error unmarshalling body response: %v", err)
	}

	expected := health.Status{
		Code: health.Partial,
		Services: map[string]health.ServiceStatus{
			"health":      {Code: health.Running},
			"prerunner":   {Code: health.Running},
			"prerunner-2": {Code: health.Failing},
			"service":     {Code: health.Running},
		},
	}

	if diff := cmp.Diff(expected, got); diff != "" {
		t.Errorf("Health status payload does not match (-want,+got): %s", diff)
	}
}

var _ health.Checker = (*testChecker)(nil)

type testChecker struct {
	name          string
	serviceStatus health.ServiceStatusCode
}

func (t testChecker) Name() string { return t.name }
func (t testChecker) Health() health.ServiceStatus {
	return health.ServiceStatus{Code: t.serviceStatus}
}

var _ PreRunner = (*testPreRun)(nil)

type testPreRun struct {
	testChecker
}

func (t testPreRun) PreRun() error {
	return nil
}

var _ Service = (*testService)(nil)

type testService struct {
	testChecker
	started chan struct{}
	done    chan struct{}
}

func (t testService) Serve() error {
	close(t.started)
	<-t.done
	return nil
}

func (t testService) GracefulStop() {
	close(t.done)
}
