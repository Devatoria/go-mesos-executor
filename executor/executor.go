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
	"github.com/Devatoria/go-mesos-executor/types"

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
	AgentInfo           mesos.AgentInfo                           // AgentInfo contains agent info returned by the agent
	Cli                 *httpcli.Client                           // Cli is the mesos HTTP cli
	ContainerTasks      map[mesos.TaskID]*types.ContainerTaskInfo // Running tasks
	Containerizer       container.Containerizer                   // Containerize to use to manage containers
	ExecutorID          string                                    // Executor ID returned by the agent when running the executor
	ExecutorInfo        mesos.ExecutorInfo                        // Executor info returned by the agent after registration
	FrameworkID         string                                    // Framework ID returned by the agent when running the executor
	FrameworkInfo       mesos.FrameworkInfo                       // Framework info returned by the agent after registration
	Handler             events.Handler                            // Handler to use to handle events
	HealthCheckers      map[mesos.TaskID]*healthcheck.Checker     // Health checkers for Mesos native health checks (one per task)
	HealthCheckersMutex *sync.RWMutex                             // Mutex used to protect health checkers map
	HookManager         *hook.Manager                             // Hooks manager
	Shutdown            bool                                      // Shutdown the executor (used to stop loop event and gently kill the executor)
	StopSignals         chan os.Signal                            // Channel receiving stopping signals (SIGINT, SIGTERM, ...)
	UnackedMutex        *sync.RWMutex                             // Mutex used to protect unacked tasks and updates maps
	UnackedTasks        map[mesos.TaskID]mesos.TaskInfo           // Unacked tasks (waiting for an acknowledgment from the agent)
	UnackedUpdates      map[string]executor.Call_Update           // Unacked updates (waiting for an acknowledgment from the agent)
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
		Cli:                 cli,
		ContainerTasks:      make(map[mesos.TaskID]*types.ContainerTaskInfo),
		Containerizer:       containerizer,
		ExecutorID:          config.ExecutorID,
		FrameworkID:         config.FrameworkID,
		FrameworkInfo:       mesos.FrameworkInfo{},
		HealthCheckers:      make(map[mesos.TaskID]*healthcheck.Checker),
		HealthCheckersMutex: &sync.RWMutex{},
		HookManager:         hookManager,
		Shutdown:            false,
		StopSignals:         make(chan os.Signal),
		UnackedMutex:        &sync.RWMutex{},
		UnackedTasks:        make(map[mesos.TaskID]mesos.TaskInfo),
		UnackedUpdates:      make(map[string]executor.Call_Update),
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
		subscribe := calls.Subscribe(e.getUnackedTasks(), e.getUnackedUpdates()).With(
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
	task := ev.GetLaunch().GetTask()

	// Lock mutex
	e.UnackedMutex.Lock()
	e.UnackedTasks[task.GetTaskID()] = task
	e.UnackedMutex.Unlock()

	logger.GetInstance().Debug("Launching a task",
		zap.Reflect("task", task),
	)

	// Get task resources
	mem, err := getMemoryLimit(task)
	if err != nil {
		e.throwError(task.GetTaskID(), err)

		return err
	}

	cpuShares, err := getCPUSharesLimit(task)
	if err != nil {
		return e.throwError(task.GetTaskID(), err)
	}

	// Create container info
	info := container.Info{
		CPUSharesLimit: cpuShares,
		MemoryLimit:    mem,
		TaskInfo:       task,
	}

	// Prepare container task info struct
	e.ContainerTasks[task.TaskID] = &types.ContainerTaskInfo{
		TaskInfo: task,
	}

	// Run pre-create hooks
	err = e.HookManager.RunPreCreateHooks(e.Containerizer, e.ContainerTasks[task.TaskID])
	if err != nil {
		return e.throwError(task.GetTaskID(), err)
	}

	// Create container
	containerID, err := e.Containerizer.ContainerCreate(info)
	if err != nil {
		return e.throwError(task.GetTaskID(), err)
	}
	e.ContainerTasks[task.TaskID].ContainerID = containerID // Set container ID when created

	// Run pre-run hooks
	err = e.HookManager.RunPreRunHooks(e.Containerizer, e.ContainerTasks[task.TaskID])
	if err != nil {
		return e.throwError(task.GetTaskID(), err)
	}

	// Launch container
	err = e.Containerizer.ContainerRun(containerID)
	if err != nil {
		return e.throwError(task.GetTaskID(), err)
	}

	// Run post-run hooks
	err = e.HookManager.RunPostRunHooks(e.Containerizer, e.ContainerTasks[task.TaskID])
	if err != nil {
		return e.throwError(task.GetTaskID(), err)
	}

	// Initialize health checker for the current task and run checks
	if task.HealthCheck != nil {
		var pid int
		pid, err = e.Containerizer.ContainerGetPID(containerID)
		if err != nil {
			return e.throwError(task.GetTaskID(), err)
		}
		e.HealthCheckersMutex.Lock()
		e.HealthCheckers[task.GetTaskID()] = healthcheck.NewChecker(pid, e.Containerizer, containerID, &task)
		e.HealthCheckersMutex.Unlock()
		go e.healthCheck(task.GetTaskID())
	}

	// Update status to RUNNING
	status := e.newStatus(task.GetTaskID())
	status.State = mesos.TASK_RUNNING.Enum()
	err = e.updateStatus(status)
	if err != nil {
		return e.throwError(task.GetTaskID(), err)
	}

	// Handle the container exit
	go e.waitContainer(e.ContainerTasks[task.TaskID], task.GetTaskID())

	return nil
}

// handleKill kills given task and updates status
func (e *Executor) handleKill(ev *executor.Event) error {
	logger.GetInstance().Info("Handled KILL event")
	taskID := ev.GetKill().GetTaskID()

	// Get container ID associated to the given task
	containerTaskInfo, ok := e.ContainerTasks[taskID]
	if !ok {
		logger.GetInstance().Warn("Error while killing the given task: task not found in running tasks",
			zap.String("taskID", taskID.GetValue()),
		)

		return e.throwError(taskID, fmt.Errorf("%s task not found, unable to kill it", taskID.GetValue()))
	}

	err := e.tearDownTask(taskID, containerTaskInfo)
	if err != nil {
		return e.throwError(taskID, err)
	}

	status := e.newStatus(taskID)
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
	delete(e.UnackedTasks, ev.GetAcknowledged().GetTaskID())
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

	// Kill all tasks
	for taskID, containerTaskInfo := range e.ContainerTasks {
		logger.GetInstance().Info("Killing task",
			zap.String("taskID", taskID.GetValue()),
		)

		err := e.tearDownTask(taskID, containerTaskInfo)
		if err != nil {
			e.throwError(taskID, err)
		}

		status := e.newStatus(taskID)
		status.State = mesos.TASK_KILLED.Enum()
		e.updateStatus(status)
	}

	// Shutdown
	e.Shutdown = true

	return nil
}

// handleError returns an error returned by the agent
func (e *Executor) handleError(ev *executor.Event) error {
	logger.GetInstance().Info("Handled ERROR event")

	return fmt.Errorf("%s", ev.GetError().GetMessage())
}

// getUnackedTasks returns a slice of unacked tasks
func (e *Executor) getUnackedTasks() []mesos.TaskInfo {
	// Lock mutex
	e.UnackedMutex.RLock()
	defer e.UnackedMutex.RUnlock()

	// Loop on unacked tasks
	var tasks []mesos.TaskInfo
	for _, task := range e.UnackedTasks {
		tasks = append(tasks, task)
	}

	return tasks
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
func (e *Executor) newStatus(taskID mesos.TaskID) mesos.TaskStatus {
	return mesos.TaskStatus{
		ExecutorID: &e.ExecutorInfo.ExecutorID,
		Source:     mesos.SOURCE_EXECUTOR.Enum(),
		TaskID:     taskID,
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
func (e *Executor) healthCheck(taskID mesos.TaskID) {
	// Retrieve checker associated to the given task
	e.HealthCheckersMutex.RLock()
	hc := e.HealthCheckers[taskID]
	e.HealthCheckersMutex.RUnlock()

	// Run check
	go hc.Run()
	for {
		select {
		// Healthy state update
		case healthy := <-hc.Healthy:
			logger.GetInstance().Info("Task health has changed",
				zap.Bool("healthy", healthy),
			)

			status := e.newStatus(taskID)
			status.Healthy = &healthy
			status.State = mesos.TASK_RUNNING.Enum()
			e.updateStatus(status)
		// Health checker has ended, we must kill the associated task
		case <-hc.Done:
			logger.GetInstance().Info("Health checker has ended, task is going to be killed")

			e.handleKill(&executor.Event{
				Kill: &executor.Event_Kill{
					TaskID: taskID,
				},
			})

			return
		// Health checker has been stopped by the executor
		case <-hc.Exited:
			logger.GetInstance().Info("Health checker has been removed, freeing...")

			return
		}
	}
}

// throwError updates the task status to TASK_FAILED, sets
// the given error as a message of the update, so the scheduler will be able
// to display it to user, and finally return the error to allow the core
// to handle it and throw it to main command
func (e *Executor) throwError(taskID mesos.TaskID, err error) error {
	// Wrap error message into task status update and send it
	// Do not check the error on update until this throw is done just before exiting
	// executor core (we can't do anything)
	message := fmt.Sprintf("Executor error: %v", err)
	status := e.newStatus(taskID)
	status.State = mesos.TASK_FAILED.Enum()
	status.Message = &message

	e.updateStatus(status)

	return err
}

// tearDownTask kills the given task, running hooks and stopping associated container before
// updating the task status
func (e *Executor) tearDownTask(taskID mesos.TaskID, containerTaskInfo *types.ContainerTaskInfo) error {
	// Quit and remove health checker (if existing)
	e.HealthCheckersMutex.Lock()
	if hc, ok := e.HealthCheckers[taskID]; ok {
		hc.Quit <- struct{}{}
	}

	delete(e.HealthCheckers, taskID)
	e.HealthCheckersMutex.Unlock()

	// Run pre-stop hooks
	err := e.HookManager.RunPreStopHooks(e.Containerizer, containerTaskInfo)
	if err != nil {
		return err
	}

	// Stop container
	err = e.Containerizer.ContainerStop(containerTaskInfo.ContainerID)
	if err != nil {
		return err
	}

	// Run post-stop hooks
	err = e.HookManager.RunPostStopHooks(e.Containerizer, containerTaskInfo)
	if err != nil {
		return err
	}

	// Remove it from tasks
	delete(e.ContainerTasks, taskID)

	return nil
}

// waitContainer waits for the given container to stop,
// making the associated task to teardown
func (e *Executor) waitContainer(containerTaskInfo *types.ContainerTaskInfo, taskID mesos.TaskID) error {
	logger.GetInstance().Info("Waiting for container to finish",
		zap.String("Container", containerTaskInfo.ContainerID),
		zap.String("Task", taskID.GetValue()),
	)

	code, err := e.Containerizer.ContainerWait(containerTaskInfo.ContainerID)
	if err != nil {
		logger.GetInstance().Error("Error while waiting for container to stop",
			zap.String("Container", containerTaskInfo.ContainerID),
			zap.String("Task", taskID.GetValue()),
			zap.Error(err),
		)

		return e.throwError(taskID, err)
	}

	logger.GetInstance().Info("Container exited",
		zap.Int("Code", code),
		zap.String("Container", containerTaskInfo.ContainerID),
		zap.String("Task", taskID.GetValue()),
	)

	err = e.tearDownTask(taskID, containerTaskInfo)
	if err != nil {
		return e.throwError(taskID, err)
	}

	message := fmt.Sprintf("Container exited (code %d)", code)
	status := e.newStatus(taskID)
	status.State = mesos.TASK_FINISHED.Enum()
	status.Message = &message

	return e.updateStatus(status)
}

// handleStopSignals handles stop signals such as SIGINT or SIGTERM
func (e *Executor) handleStopSignals() {
	sig := <-e.StopSignals
	logger.GetInstance().Info("Received stop signals",
		zap.String("signal", sig.String()),
	)
	e.handleShutdown(&executor.Event{})
}
