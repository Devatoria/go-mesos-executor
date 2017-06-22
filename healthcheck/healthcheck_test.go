package healthcheck

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Devatoria/go-mesos-executor/namespace"
	"github.com/Devatoria/go-mesos-executor/types"

	"github.com/bouk/monkey"
	"github.com/mesos/mesos-go/api/v1/lib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type HealthcheckTestSuite struct {
	suite.Suite
	checker  *Checker
	result   chan bool
	taskInfo *mesos.TaskInfo
}

func (s *HealthcheckTestSuite) SetupTest() {
	// Unpatch
	monkey.UnpatchAll()

	// Temporaries
	delay := 1.0
	timeout := 1.0
	consecutiveFailures := uint32(1)
	grace := 2.0
	interval := 1.0

	s.taskInfo = &mesos.TaskInfo{
		HealthCheck: &mesos.HealthCheck{
			DelaySeconds:        &delay,
			TimeoutSeconds:      &timeout,
			ConsecutiveFailures: &consecutiveFailures,
			GracePeriodSeconds:  &grace,
			IntervalSeconds:     &interval,
		},
	}
	s.result = make(chan bool)
	s.checker = NewChecker(0, types.NewFakeContainerizer(), "fakeID", s.taskInfo)
}

// Check that:
// - consecutive failures is set to 0
func (s *HealthcheckTestSuite) TestNewChecker() {
	assert.Equal(s.T(), uint32(0), s.checker.ConsecutiveFailures)
}

// Check that:
// - Is sleeping for "delay" seconds
// - Unhealthy if unsupported check type
// - Unhealthy if check is unhealthy
//   - Consecutive failures count is not increase during grace period
//   - Done is set if we reach the consecutive failures threshold
// - Healthy if check is healthy
// - Graceful exit
func (s *HealthcheckTestSuite) TestRun() {
	// Sleeping
	expectedDelay := time.Duration(s.taskInfo.GetHealthCheck().GetDelaySeconds()) * time.Second
	go func() { <-s.checker.Healthy }()
	start := time.Now()
	s.checker.Run()
	end := time.Now()
	assert.WithinDuration(s.T(), start.Add(expectedDelay), end, 100*time.Millisecond) // Should sleep during the defined delay (with a delta of 100ms)

	// Unsupported check type case
	go s.checker.Run()
	assert.Equal(s.T(), false, <-s.checker.Healthy) // Should be unhealthy

	// Unhealthy check case
	s.taskInfo.HealthCheck.Type = mesos.HealthCheck_TCP.Enum()
	s.taskInfo.HealthCheck.TCP = &mesos.HealthCheck_TCPCheckInfo{}
	go s.checker.Run()
	assert.Equal(s.T(), false, <-s.checker.Healthy)                                                          // Should be unhealthy
	assert.Equal(s.T(), uint32(0), s.checker.ConsecutiveFailures)                                            // Should not be increased because of grace period
	time.Sleep(time.Duration(s.taskInfo.GetHealthCheck().GetGracePeriodSeconds()) * time.Second)             // Wait for grace period to end
	<-s.checker.Healthy                                                                                      // Pass next check to increase consecutive failures count
	assert.Equal(s.T(), struct{}{}, <-s.checker.Done)                                                        // Should exit because consecutive failures threshold is reached
	assert.Equal(s.T(), s.taskInfo.GetHealthCheck().GetConsecutiveFailures(), s.checker.ConsecutiveFailures) // Should be equal to fixed threshold

	// Healthy check case
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	sURL, _ := url.Parse(server.URL)
	hostPort := strings.Split(sURL.Host, ":")
	port, _ := strconv.Atoi(hostPort[1])
	s.taskInfo.HealthCheck.TCP = &mesos.HealthCheck_TCPCheckInfo{
		Port: uint32(port),
	}
	go s.checker.Run()
	assert.Equal(s.T(), true, <-s.checker.Healthy)      // Should be healthy
	go func() { <-s.checker.Healthy }()                 // Read current check in order to allow quit chan to be read
	s.checker.Quit <- struct{}{}                        // Force checker to quit
	assert.Equal(s.T(), struct{}{}, <-s.checker.Exited) // Should exit gracefuly
}

// Check that:
// - Do not enter in network namespace if not in bridge network mode
// - Result is false if server is not accessible
// - Result is false if HTTP code is not between 200 and 399
// - Result is true otherwise
func (s *HealthcheckTestSuite) TestCheckHTTP() {
	// Prepare struct
	path := "/"
	s.taskInfo.HealthCheck.HTTP = &mesos.HealthCheck_HTTPCheckInfo{
		Path: &path,
	}

	// Patch namespace enter/exit function to simulate it
	nsenter := false
	nsexit := false
	monkey.Patch(namespace.EnterNetworkNamespace, func(_ int) error {
		nsenter = true

		return nil
	})
	monkey.Patch(namespace.ExitNetworkNamespace, func() error {
		nsexit = true

		return nil
	})

	// Bridge mode (namespace management)
	s.taskInfo.Container = &mesos.ContainerInfo{
		Docker: &mesos.ContainerInfo_DockerInfo{
			Network: mesos.ContainerInfo_DockerInfo_BRIDGE.Enum(),
		},
	}
	go s.checker.checkHTTP(s.result)
	<-s.result
	assert.Equal(s.T(), true, nsenter) // Should enter in the namespace
	assert.Equal(s.T(), true, nsexit)  // Should exit the namespace

	// Host mode (no namespace management)
	nsenter = false
	nsexit = false
	s.taskInfo.Container.Docker.Network = mesos.ContainerInfo_DockerInfo_HOST.Enum()
	go s.checker.checkHTTP(s.result)
	<-s.result
	assert.Equal(s.T(), false, nsenter) // Should not enter in the namespace
	assert.Equal(s.T(), false, nsexit)  // Should not exit the namespace

	// Server is not accessible
	go s.checker.checkHTTP(s.result)
	assert.Equal(s.T(), false, <-s.result) // Should be unhealthy

	// Server is accessible but returns a wrong HTTP code
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound) // 404
	}))
	serverURL, _ := url.Parse(server.URL)
	portString := strings.Split(serverURL.Host, ":")
	port, _ := strconv.Atoi(portString[1])
	s.taskInfo.HealthCheck.HTTP.Port = uint32(port)
	go s.checker.checkHTTP(s.result)
	assert.Equal(s.T(), false, <-s.result) // Should be unhealthy
	server.Close()

	// Nominal case
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	serverURL, _ = url.Parse(server.URL)
	portString = strings.Split(serverURL.Host, ":")
	port, _ = strconv.Atoi(portString[1])
	s.taskInfo.HealthCheck.HTTP.Port = uint32(port)
	go s.checker.checkHTTP(s.result)
	assert.Equal(s.T(), true, <-s.result) // Should be unhealthy
	server.Close()
}

// Check that:
// - Do not enter in network namespace if not in bridge mode
// - Result is false if server is not accessible
// - Result is true otherwise
func (s *HealthcheckTestSuite) TestCheckTCP() {
	// Prepare struct
	s.taskInfo.HealthCheck.TCP = &mesos.HealthCheck_TCPCheckInfo{}

	// Patch namespace enter/exit function to simulate it
	nsenter, nsexit := false, false
	monkey.Patch(namespace.EnterNetworkNamespace, func(_ int) error {
		nsenter = true

		return nil
	})
	monkey.Patch(namespace.ExitNetworkNamespace, func() error {
		nsexit = true

		return nil
	})

	// Bridge mode (namespace management)
	s.taskInfo.Container = &mesos.ContainerInfo{
		Docker: &mesos.ContainerInfo_DockerInfo{
			Network: mesos.ContainerInfo_DockerInfo_BRIDGE.Enum(),
		},
	}
	go s.checker.checkTCP(s.result)
	<-s.result
	assert.Equal(s.T(), true, nsenter) // Should enter network namespace
	assert.Equal(s.T(), true, nsexit)  // Should exit network namespace

	// Host mode (no namespace management)
	nsenter, nsexit = false, false
	s.taskInfo.Container.Docker.Network = mesos.ContainerInfo_DockerInfo_HOST.Enum()
	go s.checker.checkTCP(s.result)
	<-s.result
	assert.Equal(s.T(), false, nsenter) // Should not enter network namespace
	assert.Equal(s.T(), false, nsexit)  // Should not exit network namespace

	// Server is not accessible
	go s.checker.checkTCP(s.result)
	assert.Equal(s.T(), false, <-s.result)

	// Nominal case
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	serverURL, _ := url.Parse(server.URL)
	portString := strings.Split(serverURL.Host, ":")
	port, _ := strconv.Atoi(portString[1])
	s.taskInfo.HealthCheck.TCP.Port = uint32(port)
	go s.checker.checkTCP(s.result)
	assert.Equal(s.T(), true, <-s.result)
	server.Close()
}

// Check that:
// - Command is wrapped (or not) into shell when calling containerizer exec
// - Result is false if timeout
// - Result is false if error on execution
// - Result is true otherwise
func (s *HealthcheckTestSuite) TestCheckCommand() {
	// Prepare struct
	shell := true
	value := "sleep 1"
	s.taskInfo.HealthCheck.Command = &mesos.CommandInfo{
		Value: &value,
		Shell: &shell,
	}

	// We patch containerizer exec function to get formated command
	// and send a nil result (OK)
	var command []string
	monkey.PatchInstanceMethod(reflect.TypeOf(s.checker.Containerizer), "ContainerExec", func(_ *types.FakeContainerizer, _ context.Context, _ string, cmd []string) chan error {
		command = cmd
		ch := make(chan error)
		go func() {
			ch <- nil
		}()

		return ch
	})

	// With wrapping
	go s.checker.checkCommand(s.result)
	<-s.result
	assert.Equal(s.T(), []string{"/bin/sh", "-c", s.taskInfo.GetHealthCheck().GetCommand().GetValue()}, command) // Should be wrapped into a shell

	// Without wrapping
	shell = false
	go s.checker.checkCommand(s.result)
	<-s.result
	assert.Equal(s.T(), []string{s.taskInfo.GetHealthCheck().GetCommand().GetValue()}, command) // Should not be wrapped into a shell

	// Nominal case
	go s.checker.checkCommand(s.result)
	assert.Equal(s.T(), true, <-s.result) // Should be healthy

	// Timeout case
	// We patch function to time out
	monkey.PatchInstanceMethod(reflect.TypeOf(s.checker.Containerizer), "ContainerExec", func(_ *types.FakeContainerizer, _ context.Context, _ string, _ []string) chan error {
		ch := make(chan error)
		go func() {
			time.Sleep(2 * time.Second)
			ch <- nil
		}()

		return ch
	})
	go s.checker.checkCommand(s.result)
	assert.Equal(s.T(), false, <-s.result) // Should be false

	// Error on execution
	// We patch function to return an error
	monkey.PatchInstanceMethod(reflect.TypeOf(s.checker.Containerizer), "ContainerExec", func(_ *types.FakeContainerizer, _ context.Context, _ string, _ []string) chan error {
		ch := make(chan error)
		go func() {
			ch <- errors.New("fake")
		}()

		return ch
	})
	go s.checker.checkCommand(s.result)
	assert.Equal(s.T(), false, <-s.result) // Should be false
}

func TestHealthcheckSuite(t *testing.T) {
	suite.Run(t, new(HealthcheckTestSuite))
}
