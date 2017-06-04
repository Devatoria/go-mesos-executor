package healthcheck

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/Devatoria/go-mesos-executor/logger"
	"github.com/Devatoria/go-mesos-executor/namespace"
	"github.com/spf13/viper"

	"github.com/Devatoria/go-nsenter"
	"github.com/mesos/mesos-go/api/v1/lib"
	"go.uber.org/zap"
)

// Checker is a health checker in charge to run check and say
// it task is healthy or unhealthy
type Checker struct {
	ConsecutiveFailures uint32
	Done                chan struct{}
	Exited              chan struct{}
	GracePeriodExpired  *uint32
	Healthy             chan bool
	Pid                 int
	TaskInfo            *mesos.TaskInfo
	Quit                chan struct{}
}

// NewChecker instanciate a health checker with given health check
func NewChecker(pid int, taskInfo *mesos.TaskInfo) *Checker {
	var gracePeriodExpired uint32
	gracePeriodExpired = 0

	return &Checker{
		ConsecutiveFailures: 0,
		Done:                make(chan struct{}),
		Exited:              make(chan struct{}),
		GracePeriodExpired:  &gracePeriodExpired,
		Healthy:             make(chan bool),
		Pid:                 pid,
		TaskInfo:            taskInfo,
		Quit:                make(chan struct{}),
	}
}

// Run starts to run health check
func (c *Checker) Run() {
	// Prepare interval ticker
	ticker := time.NewTicker(time.Duration(c.TaskInfo.GetHealthCheck().GetIntervalSeconds()) * time.Second)
	defer ticker.Stop()

	// Sleep during the defined delay before starting to check
	time.Sleep(time.Duration(c.TaskInfo.GetHealthCheck().GetDelaySeconds()) * time.Second)

	// Grace period timer
	gracePeriodTimer := time.NewTimer(time.Duration(c.TaskInfo.GetHealthCheck().GetGracePeriodSeconds()) * time.Second)
	defer gracePeriodTimer.Stop()

	// Wait for grace period to end (in parallel with checks)
	go func() {
		<-gracePeriodTimer.C
		atomic.StoreUint32(c.GracePeriodExpired, 1)

		logger.GetInstance().Development.Debug("Grace period expired")
	}()

	// Define check function to use (HTTP/TCP/COMMAND)
	var checkFunc func(chan bool)
	switch c.TaskInfo.GetHealthCheck().GetType() {
	case mesos.HealthCheck_HTTP:
		checkFunc = c.checkHTTP
	case mesos.HealthCheck_TCP:
		checkFunc = c.checkTCP
	case mesos.HealthCheck_COMMAND:
		checkFunc = c.checkCommand
	default: // Unsupported health check type
		logger.GetInstance().Production.Error("Unknown or unsupported health check type... Mark task as unhealthy",
			zap.String("type", c.TaskInfo.GetHealthCheck().GetType().String()),
		)
		c.Healthy <- false

		return
	}

	// Start ticking
	var healthy bool
	for {
		select {
		// Ticket has ticked (we must launch the check)
		case <-ticker.C:
			// Prepare channel to receive result or timeout
			result := make(chan bool)
			go checkFunc(result)
			healthy = <-result

			logger.GetInstance().Development.Debug("Health check ticker has ticked",
				zap.Bool("healthy", healthy),
			)

			// Manage consecutive failures count
			if healthy {
				c.ConsecutiveFailures = 0

				// Manually expire grace period if check is healthy
				atomic.StoreUint32(c.GracePeriodExpired, 1)
			} else {
				// Do not take care about consecutive failures if we are in the grace period
				// We'll just change the task status but do not increase count
				if atomic.LoadUint32(c.GracePeriodExpired) == 1 {
					c.ConsecutiveFailures++

					// If we reached the max consecutive failures, we stop the checker,
					// signal the executor and wait for it to tell us to free resources (using quit chan)
					if c.ConsecutiveFailures == c.TaskInfo.GetHealthCheck().GetConsecutiveFailures() {
						ticker.Stop()
						c.Done <- struct{}{}

						break
					}
				}
			}

			c.Healthy <- healthy
		// Executor asked the checker to quit
		case <-c.Quit:
			logger.GetInstance().Development.Debug("Shutting down checker")
			c.Exited <- struct{}{}

			return
		}
	}
}

// checkHTTP enters the container network namespace and
// calls the check path, and returns true if the HTTP status
// is between 200 and 399
func (c *Checker) checkHTTP(result chan bool) {
	// Enter network namespace only if we are running in bridge mode
	if c.TaskInfo.GetContainer().GetDocker().GetNetwork() == mesos.ContainerInfo_DockerInfo_BRIDGE {
		defer namespace.ExitNetworkNamespace() // No matter what happen, we must return into executor namespace and unlock thread

		// Enter container network namespace
		err := namespace.EnterNetworkNamespace(c.Pid)
		if err != nil {
			logger.GetInstance().Development.Debug("Error while getting container namespace",
				zap.Error(err),
			)

			result <- false

			return
		}
	}

	// Prepare raw TCP request
	// We can't use net/http package here because transport RoundTripper
	// would make the call on another goroutine, making the network namespace
	// to be switched to another one that we don't care.
	port := c.TaskInfo.GetHealthCheck().GetHTTP().GetPort()
	path := c.TaskInfo.GetHealthCheck().GetHTTP().GetPath()
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", port), time.Duration(c.TaskInfo.GetHealthCheck().GetTimeoutSeconds())*time.Second)
	defer conn.Close()
	if err != nil {
		logger.GetInstance().Production.Error("Error while connecting to health check socket",
			zap.Error(err),
		)

		result <- false

		return
	}

	// Do raw HTTP request and get status
	// Then, parse it into an HTTP response in order
	// to be able to easily manipulate it
	fmt.Fprintf(conn, fmt.Sprintf("GET %s HTTP/1.0\r\n\r\n", path))
	status := bufio.NewReader(conn)
	response, err := http.ReadResponse(status, nil)
	if err != nil {
		logger.GetInstance().Production.Error("Error while reading HTTP response",
			zap.Error(err),
		)

		result <- false

		return
	}

	// Check status code: should be between 200 and 399
	if response.StatusCode < 200 || response.StatusCode > 399 {
		logger.GetInstance().Production.Error("Unexpected status code",
			zap.Int("code", response.StatusCode),
		)

		result <- false

		return
	}

	result <- true
}

// checkTCP enters the container network namespace and
// check that given port is accessible
func (c *Checker) checkTCP(result chan bool) {
	// Enter network namespace only if we are running in bridge mode
	if c.TaskInfo.GetContainer().GetDocker().GetNetwork() == mesos.ContainerInfo_DockerInfo_BRIDGE {
		defer namespace.ExitNetworkNamespace()

		// Enter container network namespace
		err := namespace.EnterNetworkNamespace(c.Pid)
		if err != nil {
			logger.GetInstance().Development.Debug("Error while getting container namespace",
				zap.Error(err),
			)

			result <- false

			return
		}
	}

	// Try to dial on health check port
	port := c.TaskInfo.GetHealthCheck().GetTCP().GetPort()
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", port), time.Duration(c.TaskInfo.GetHealthCheck().GetTimeoutSeconds())*time.Second)
	defer conn.Close()
	if err != nil {
		logger.GetInstance().Production.Error("Error while connecting to health check socket",
			zap.Error(err),
		)

		result <- false

		return
	}

	result <- true
}

// checkCommand enters the container mount namespace and
// executes the given command using nsenter
func (c *Checker) checkCommand(result chan bool) {
	var err error
	var args []string
	var program string
	command := c.TaskInfo.GetHealthCheck().GetCommand()
	// Must be wrapped into a shell process
	if command.GetShell() {
		// Wrap command into quotes
		// (otherwise, params would not be passed to the wrapped command)
		args = []string{"-c", fmt.Sprintf("\"%s\"", command.GetValue())}
		program = "/bin/sh"
	} else {
		program = command.GetValue()
	}

	// Prepare config to enter required namespaces
	// TODO: should we enter into container user namespace if it is actived in containerizer?
	nsPath := fmt.Sprintf("%s/%d/ns", viper.GetString("proc_path"), c.Pid)
	config := &nsenter.Config{
		Mount:     true,
		MountFile: fmt.Sprintf("%s/mnt", nsPath),
		Target:    c.Pid,
	}

	// Enter network namespace only if we are running in bridge mode
	if c.TaskInfo.GetContainer().GetDocker().GetNetwork() == mesos.ContainerInfo_DockerInfo_BRIDGE {
		config.Net = true
		config.NetFile = fmt.Sprintf("%s/net", nsPath)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(c.TaskInfo.GetHealthCheck().GetTimeoutSeconds())*time.Second)
	stdout, stderr, err := config.ExecuteContext(ctx, program, args...)
	cancel()

	if err != nil {
		logger.GetInstance().Production.Error("Error while executing health check command",
			zap.String("stderr", stderr),
			zap.Error(err),
		)

		result <- false

		return
	}

	logger.GetInstance().Development.Debug("Health check has been done",
		zap.String("stdout", stdout),
	)

	result <- true
}
