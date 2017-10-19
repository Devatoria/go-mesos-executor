package executor

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/Devatoria/go-mesos-executor/container"
	"github.com/Devatoria/go-mesos-executor/healthcheck"
	"github.com/Devatoria/go-mesos-executor/hook"
	"github.com/Devatoria/go-mesos-executor/logger"

	"github.com/mesos/mesos-go/api/v1/lib"
	"github.com/mesos/mesos-go/api/v1/lib/encoding"
	"github.com/mesos/mesos-go/api/v1/lib/executor"
	"github.com/mesos/mesos-go/api/v1/lib/executor/calls"
	"github.com/mesos/mesos-go/api/v1/lib/executor/events"
	"github.com/mesos/mesos-go/api/v1/lib/httpcli"
	"github.com/pborman/uuid"
	"go.uber.org/zap"
)

const (
	apiEndpoint     = "/api/v1/executor" // Mesos agent endpoint used by the executor to communicate
	cpuSharesPerCPU = 1024               // Default CPU shares per CPU
	timeout         = 10 * time.Second   // Timeout to apply when calling agent API
)

// Executor represents an executor
type Executor struct {
	AgentInfo      mesos.AgentInfo                 // AgentInfo contains agent info returned by the agent
	Cli            *httpcli.Client                 // Cli is the mesos HTTP cli
	ContainerID    string                          // Running container ID
	Containerizer  container.Containerizer         // Containerize to use to manage containers
	ExecutorID     string                          // Executor ID returned by the agent when running the executor
	ExecutorInfo   mesos.ExecutorInfo              // Executor info returned by the agent after registration
	FrameworkID    string                          // Framework ID returned by the agent when running the executor
	FrameworkInfo  mesos.FrameworkInfo             // Framework info returned by the agent after registration
	Handler        events.Handler                  // Handler to use to handle events
	HealthChecker  *healthcheck.Checker            // Health checker for Mesos native health checks
	HookManager    *hook.Manager                   // Hooks manager
	Shutdown       bool                            // Shutdown the executor (used to stop loop event and gently kill the executor)
	StopSignals    chan os.Signal                  // Channel receiving stopping signals (SIGINT, SIGTERM, ...)
	TaskInfo       mesos.TaskInfo                  // Task info sent by the agent on launch event
	UnackedMutex   *sync.RWMutex                   // Mutex used to protect unacked tasks and updates maps
	UnackedUpdates map[string]executor.Call_Update // Unacked updates (waiting for an acknowledgment from the agent)
}

// Config represents an executor config, containing arguments passed by the
// agent and used to initialize a new executor
type Config struct {
	AgentEndpoint string
	ExecutorID    string
	FrameworkID   string
}

// getResource searches the given resource name in the given task
// and returns it, or returns an error if the resource cannot be found
func getResource(task mesos.TaskInfo, name string) (mesos.Resource, error) {
	for _, resource := range task.GetResources() {
		if resource.GetName() == name {
			return resource, nil
		}
	}

	return mesos.Resource{}, fmt.Errorf("Unable to find resource %s", name)
}

// getMemoryLimit returns the set memory limit (in MB) in the given task (or an error if unavailable)
func getMemoryLimit(task mesos.TaskInfo) (uint64, error) {
	resource, err := getResource(task, "mem")
	if err != nil {
		return 0, err
	}

	return uint64(resource.GetScalar().GetValue() * 1024 * 1024), nil
}

// getCPUSharesLimit returns the set CPU limit (in shares) in the given task (or an error if unavailable)
func getCPUSharesLimit(task mesos.TaskInfo) (uint64, error) {
	resource, err := getResource(task, "cpus")
	if err != nil {
		return 0, err
	}

	return uint64(resource.GetScalar().GetValue() * cpuSharesPerCPU), nil
}

// NewExecutor initializes a new executor with the given executor and framework ID
func NewExecutor(config Config, containerizer container.Containerizer, hookManager *hook.Manager) *Executor {
	var e *Executor

	apiURL := url.URL{
		Scheme: "http",
		Host:   config.AgentEndpoint,
		Path:   apiEndpoint,
	}
	cli := httpcli.New(
		httpcli.Endpoint(apiURL.String()),
		httpcli.Codec(&encoding.ProtobufCodec),
		httpcli.Do(httpcli.With(httpcli.Timeout(timeout))),
	)

	// Prepare executor
	e = &Executor{
		Cli:            cli,
		Containerizer:  containerizer,
		ExecutorID:     config.ExecutorID,
		FrameworkID:    config.FrameworkID,
		FrameworkInfo:  mesos.FrameworkInfo{},
		HookManager:    hookManager,
		Shutdown:       false,
		StopSignals:    make(chan os.Signal),
		UnackedMutex:   &sync.RWMutex{},
		UnackedUpdates: make(map[string]executor.Call_Update),
	}

	// Add events handler
	e.Handler = events.NewMux(
		events.Handle(executor.Event_SUBSCRIBED, events.HandlerFunc(e.handleSubscribed)),
		events.Handle(executor.Event_LAUNCH, events.HandlerFunc(e.handleLaunch)),
		events.Handle(executor.Event_KILL, events.HandlerFunc(e.handleKill)),
		events.Handle(executor.Event_ACKNOWLEDGED, events.HandlerFunc(e.handleAcknowledged)),
		events.Handle(executor.Event_MESSAGE, events.HandlerFunc(e.handleMessage)),
		events.Handle(executor.Event_SHUTDOWN, events.HandlerFunc(e.handleShutdown)),
		events.Handle(executor.Event_ERROR, events.HandlerFunc(e.handleError)),
	)

	return e
}

// Execute runs the executor workflow
func (e *Executor) Execute() error {
	// Handles stopping signals to gracefuly shutdown the executor
	signal.Notify(e.StopSignals,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	go e.handleStopSignals()

	for !e.Shutdown {
		var err error
		var resp mesos.Response

		// Prepare for subscribing
		subscribe := calls.Subscribe(nil, e.getUnackedUpdates()).With(
			calls.Executor(e.ExecutorID),
			calls.Framework(e.FrameworkID),
		)
		resp, err = e.Cli.Do(subscribe, httpcli.Close(true))
		if resp != nil {
			defer resp.Close()
		}

		// If there's an error which is not an EOF, we throw
		// Otherwise, it means that we've been disconnected from the agent and we will try to reconnect
		if err != nil {
			if err == io.EOF {
				continue
			}

			return err
		}

		// We are connected, we start to handle events
		for !e.Shutdown {
			var event executor.Event
			decoder := encoding.Decoder(resp)
			err = decoder.Decode(&event)
			if err != nil {
				return err
			}

			err = e.Handler.HandleEvent(&event)
			if err != nil {
				return err
			}
		}
	}

	// Now, executor is shutting down (every tasks have been killed)
	logger.GetInstance().Info("All tasks have been killed. Now exiting, bye bye.")

	return nil
}

// handleSubscribed handles subscribed events
func (e *Executor) handleSubscribed(ev *executor.Event) error {
	logger.GetInstance().Info("Handled SUBSCRIBED event")
	e.AgentInfo = ev.GetSubscribed().GetAgentInfo()
	e.ExecutorInfo = ev.GetSubscribed().GetExecutorInfo()
	e.FrameworkInfo = ev.GetSubscribed().GetFrameworkInfo()

	return nil
}

// handleLaunch puts given task in unacked tasks and launches it
func (e *Executor) handleLaunch(ev *executor.Event) error {
	logger.GetInstance().Info("Handled LAUNCH event")
	e.TaskInfo = ev.GetLaunch().GetTask()

	logger.GetInstance().Debug("Launching a task",
		zap.Reflect("task", e.TaskInfo),
	)

	// Get task resources
	mem, err := getMemoryLimit(e.TaskInfo)
	if err != nil {
		return e.throwError(fmt.Errorf("error while getting memory limit: %v", err))
	}

	cpuShares, err := getCPUSharesLimit(e.TaskInfo)
	if err != nil {
		return e.throwError(fmt.Errorf("error while getting cpu shares limit: %v", err))
	}

	// Create container info
	info := container.Info{
		CPUSharesLimit: cpuShares,
		MemoryLimit:    mem,
		TaskInfo:       e.TaskInfo,
	}

	err = e.HookManager.RunPreCreateHooks(e.Containerizer, &e.TaskInfo)
	if err != nil {
		return e.throwError(fmt.Errorf("error while running pre-create hooks: %v", err))
	}

	// Create container
	containerID, err := e.Containerizer.ContainerCreate(info)
	if err != nil {
		return e.throwError(fmt.Errorf("error while creating the container: %v", err))
	}
	e.ContainerID = containerID

	// Run pre-run hooks
	err = e.HookManager.RunPreRunHooks(e.Containerizer, &e.TaskInfo, e.ContainerID)
	if err != nil {
		return e.throwError(fmt.Errorf("error while running pre-run hooks: %v", err))
	}

	// Launch container
	err = e.Containerizer.ContainerRun(containerID)
	if err != nil {
		return e.throwError(fmt.Errorf("error while starting the container: %v", err))
	}

	// Run post-run hooks
	err = e.HookManager.RunPostRunHooks(e.Containerizer, &e.TaskInfo, e.ContainerID)
	if err != nil {
		return e.throwError(fmt.Errorf("error while running post-run hooks: %v", err))
	}

	// Initialize health checker for the current task and run checks
	if e.TaskInfo.HealthCheck != nil {
		var pid int
		pid, err = e.Containerizer.ContainerGetPID(containerID)
		if err != nil {
			return e.throwError(fmt.Errorf("error while getting container pid: %v", err))
		}
		e.HealthChecker = healthcheck.NewChecker(pid, e.Containerizer, containerID, &e.TaskInfo)
		go e.healthCheck()
	}

	// Update status to RUNNING
	status := e.newStatus()
	status.State = mesos.TASK_RUNNING.Enum()
	err = e.updateStatus(status)
	if err != nil {
		return e.throwError(fmt.Errorf("error while updating task status: %v", err))
	}

	// Handle the container exit
	go e.waitContainer()

	return nil
}

// handleKill kills given task and updates status
func (e *Executor) handleKill(ev *executor.Event) error {
	logger.GetInstance().Info("Handled KILL event")

	e.Shutdown = true
	e.tearDown()
	status := e.newStatus()
	status.State = mesos.TASK_KILLED.Enum()

	return e.updateStatus(status)
}

// handleAcknowledged removes the given task/update from unacked
func (e *Executor) handleAcknowledged(ev *executor.Event) error {
	logger.GetInstance().Info("Handled ACKNOWLEDGED event")

	// Lock mutex
	e.UnackedMutex.Lock()
	defer e.UnackedMutex.Unlock()

	// Remove task/update from unacked
	delete(e.UnackedUpdates, string(ev.GetAcknowledged().GetUUID()))

	return nil
}

// handleMessage receives a message from the scheduler and logs it
func (e *Executor) handleMessage(ev *executor.Event) error {
	logger.GetInstance().Info("Handled MESSAGE event",
		zap.String("eventMessage", string(ev.GetMessage().GetData())),
	)

	return nil
}

// handleShutdown kills all tasks before shuting down the executor
func (e *Executor) handleShutdown(ev *executor.Event) error {
	logger.GetInstance().Info("Handled SHUTDOWN event")

	return e.handleKill(nil)
}

// handleError returns an error returned by the agent
func (e *Executor) handleError(ev *executor.Event) error {
	logger.GetInstance().Info("Handled ERROR event")

	return fmt.Errorf("%s", ev.GetError().GetMessage())
}

// getUnackedUpdates returns a slice of unacked updates
func (e *Executor) getUnackedUpdates() []executor.Call_Update {
	// Lock mutex
	e.UnackedMutex.RLock()
	defer e.UnackedMutex.RUnlock()

	// Loop on unacked updates
	var updates []executor.Call_Update
	for _, update := range e.UnackedUpdates {
		updates = append(updates, update)
	}

	return updates
}

// newStatus returns a mesos task status generated for the given executor and task ID
func (e *Executor) newStatus() mesos.TaskStatus {
	return mesos.TaskStatus{
		ExecutorID: &e.ExecutorInfo.ExecutorID,
		Source:     mesos.SOURCE_EXECUTOR.Enum(),
		TaskID:     e.TaskInfo.GetTaskID(),
		UUID:       []byte(uuid.NewRandom()),
	}
}

// updateStatus updates the current status of the given executor and adds the update to the
// unacked updates
func (e *Executor) updateStatus(status mesos.TaskStatus) error {
	// Prepare and do the call
	u := calls.Update(status).With(
		calls.Executor(e.ExecutorID),
		calls.Framework(e.FrameworkID),
	)
	resp, err := e.Cli.Do(u)
	if resp != nil {
		defer resp.Close()
	}

	if err != nil {
		return err
	}

	// Lock mutex
	e.UnackedMutex.Lock()
	defer e.UnackedMutex.Unlock()

	// Add current update to unacked updates until we handle the acknowledgment in events
	e.UnackedUpdates[string(status.UUID)] = *u.Update

	return nil
}

// healthCheck handles task health changes and update task status
func (e *Executor) healthCheck() {
	// Run check
	go e.HealthChecker.Run()
	for {
		select {
		// Healthy state update
		case healthy := <-e.HealthChecker.Healthy:
			logger.GetInstance().Info("Task health has changed",
				zap.Bool("healthy", healthy),
			)

			status := e.newStatus()
			status.Healthy = &healthy
			status.State = mesos.TASK_RUNNING.Enum()
			e.updateStatus(status)
		// Health checker has ended, we must kill the associated task
		case <-e.HealthChecker.Done:
			logger.GetInstance().Info("Health checker has ended, task is going to be killed")

			e.handleKill(nil)

			return
		// Health checker has been stopped by the executor
		case <-e.HealthChecker.Exited:
			logger.GetInstance().Info("Health checker has been removed, freeing...")

			return
		}
	}
}

// throwError updates the task status to TASK_FAILED, sets
// the given error as a message of the update, so the scheduler will be able
// to display it to user, and finally return the error to allow the core
// to handle it and throw it to main command
func (e *Executor) throwError(err error) error {
	e.tearDown()

	// Wrap error message into task status update and send it
	// Do not check the error on update until this throw is done just before exiting
	// executor core (we can't do anything)
	message := err.Error()
	status := e.newStatus()
	status.State = mesos.TASK_FAILED.Enum()
	status.Message = &message

	e.updateStatus(status)

	return err
}

// tearDown kills the executor task, running hooks and stopping associated container
func (e *Executor) tearDown() {
	// Quit and remove health checker (if existing)
	if e.HealthChecker != nil {
		e.HealthChecker.Quit <- struct{}{}
	}

	e.HookManager.RunPreStopHooks(e.Containerizer, &e.TaskInfo, e.ContainerID)
	e.Containerizer.ContainerStop(e.ContainerID)
	e.HookManager.RunPostStopHooks(e.Containerizer, &e.TaskInfo, e.ContainerID)
}

// waitContainer waits for the executor container to stop,
// making the associated task to teardown
func (e *Executor) waitContainer() error {
	logger.GetInstance().Info("Waiting for container to finish",
		zap.String("Container", e.ContainerID),
		zap.String("Task", e.TaskInfo.TaskID.GetValue()),
	)

	code, err := e.Containerizer.ContainerWait(e.ContainerID)
	if err != nil {
		logger.GetInstance().Error("Error while waiting for container to stop",
			zap.String("Container", e.ContainerID),
			zap.String("Task", e.TaskInfo.TaskID.GetValue()),
			zap.Error(err),
		)

		return e.throwError(fmt.Errorf("error while waiting for container to stop: %v", err))
	}

	logger.GetInstance().Info("Container exited",
		zap.Int("Code", code),
		zap.String("Container", e.ContainerID),
		zap.String("Task", e.TaskInfo.TaskID.GetValue()),
	)

	if code != 0 {
		return e.throwError(fmt.Errorf("container exited (code %d)", code))
	}

	e.tearDown()
	status := e.newStatus()
	status.State = mesos.TASK_FINISHED.Enum()

	return e.updateStatus(status)
}

// handleStopSignals handles stop signals such as SIGINT or SIGTERM
func (e *Executor) handleStopSignals() {
	sig := <-e.StopSignals
	logger.GetInstance().Info("Received stop signals",
		zap.String("signal", sig.String()),
	)

	e.handleKill(nil)
}
