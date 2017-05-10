package executor

import (
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/Devatoria/go-mesos-executor/container"

	"github.com/Sirupsen/logrus"
	"github.com/mesos/mesos-go/api/v1/lib"
	"github.com/mesos/mesos-go/api/v1/lib/encoding"
	"github.com/mesos/mesos-go/api/v1/lib/executor"
	"github.com/mesos/mesos-go/api/v1/lib/executor/calls"
	"github.com/mesos/mesos-go/api/v1/lib/executor/events"
	"github.com/mesos/mesos-go/api/v1/lib/httpcli"
	"github.com/pborman/uuid"
)

const (
	apiEndpoint     = "/api/v1/executor" // Mesos agent endpoint used by the executor to communicate
	cpuSharesPerCPU = 1024               // Default CPU shares per CPU
	timeout         = 10 * time.Second   // Timeout to apply when calling agent API
)

// Executor represents an executor
type Executor struct {
	AgentInfo      mesos.AgentInfo                    // AgentInfo contains agent info returned by the agent
	Cli            *httpcli.Client                    // Cli is the mesos HTTP cli
	ContainerTasks map[mesos.TaskID]ContainerTaskInfo // Running tasks
	Containerizer  container.Containerizer            // Containerize to use to manage containers
	ExecutorID     string                             // Executor ID returned by the agent when running the executor
	ExecutorInfo   mesos.ExecutorInfo                 // Executor info returned by the agent after registration
	FrameworkID    string                             // Framework ID returned by the agent when running the executor
	FrameworkInfo  mesos.FrameworkInfo                // Framework info returned by the agent after registration
	Handler        events.Handler                     // Handler to use to handle events
	Shutdown       bool                               // Shutdown the executor (used to stop loop event and gently kill the executor)
	UnackedTasks   map[mesos.TaskID]mesos.TaskInfo    // Unacked tasks (waiting for an acknowledgment from the agent)
	UnackedUpdates map[string]executor.Call_Update    // Unacked updates (waiting for an acknowledgment from the agent)
}

// Config represents an executor config, containing arguments passed by the
// agent and used to initialize a new executor
type Config struct {
	AgentEndpoint string
	ExecutorID    string
	FrameworkID   string
}

// ContainerTaskInfo represents a container linked to a task
// This struct is used to store executor tasks with associated containers
type ContainerTaskInfo struct {
	ContainerID string
	TaskInfo    mesos.TaskInfo
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
func NewExecutor(config Config, containerizer container.Containerizer) *Executor {
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
		ContainerTasks: make(map[mesos.TaskID]ContainerTaskInfo),
		Containerizer:  containerizer,
		ExecutorID:     config.ExecutorID,
		FrameworkID:    config.FrameworkID,
		FrameworkInfo:  mesos.FrameworkInfo{},
		Shutdown:       false,
		UnackedTasks:   make(map[mesos.TaskID]mesos.TaskInfo),
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
	for e.Shutdown == false {
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
		for e.Shutdown == false {
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
	logrus.Info("All tasks have been killed. Now exiting, bye bye.")

	return nil
}

// handleSubscribed handles subscribed events
func (e *Executor) handleSubscribed(ev *executor.Event) error {
	logrus.Info("Handled SUBSCRIBED event")
	e.AgentInfo = ev.GetSubscribed().GetAgentInfo()
	e.ExecutorInfo = ev.GetSubscribed().GetExecutorInfo()
	e.FrameworkInfo = ev.GetSubscribed().GetFrameworkInfo()

	return nil
}

// handleLaunch puts given task in unacked tasks and launches it
func (e *Executor) handleLaunch(ev *executor.Event) error {
	logrus.Info("Handled LAUNCH event")
	task := ev.GetLaunch().GetTask()
	e.UnackedTasks[task.GetTaskID()] = task

	// Get task resources
	mem, err := getMemoryLimit(task)
	if err != nil {
		return err
	}

	cpuShares, err := getCPUSharesLimit(task)
	if err != nil {
		return err
	}

	// Create container info
	info := container.Info{
		CPUSharesLimit: cpuShares,
		Image:          task.GetContainer().GetDocker().GetImage(),
		MemoryLimit:    mem,
	}

	// Launch container
	containerID, err := e.Containerizer.ContainerRun(info)
	if err != nil {
		return err
	}

	// Store new container ID and task
	e.ContainerTasks[task.TaskID] = ContainerTaskInfo{
		ContainerID: containerID,
		TaskInfo:    task,
	}

	// Update status to RUNNING
	status := e.newStatus(task.GetTaskID())
	status.State = mesos.TASK_RUNNING.Enum()

	return e.updateStatus(status)
}

// handleKill kills given task and updates status
func (e *Executor) handleKill(ev *executor.Event) error {
	logrus.Info("Handled KILL event")
	taskID := ev.GetKill().GetTaskID()

	// Get container ID associated to the given task
	containerTaskInfo, ok := e.ContainerTasks[taskID]
	if !ok {
		logrus.WithField("TaskID", taskID.GetValue()).Warn("Unable to kill the given task (not found in running tasks)")

		return fmt.Errorf("%s task not found, unable to kill it", taskID.GetValue())
	}

	// Stop container
	err := e.Containerizer.ContainerStop(containerTaskInfo.ContainerID)
	if err != nil {
		return err
	}

	// Remove it from tasks
	delete(e.ContainerTasks, taskID)

	// Update status to TASK_KILLED
	status := e.newStatus(taskID)
	status.State = mesos.TASK_KILLED.Enum()

	return e.updateStatus(status)
}

// handleAcknowledged removes the given task/update from unacked
func (e *Executor) handleAcknowledged(ev *executor.Event) error {
	logrus.Info("Handled ACKNOWLEDEGED event")

	// Remove task/update from unacked
	delete(e.UnackedTasks, ev.GetAcknowledged().GetTaskID())
	delete(e.UnackedUpdates, string(ev.GetAcknowledged().GetUUID()))

	return nil
}

// handleMessage receives a message from the scheduler and logs it
func (e *Executor) handleMessage(ev *executor.Event) error {
	logrus.WithField("message", string(ev.GetMessage().GetData())).Info("Handled MESSAGE event")

	return nil
}

// handleShutdown kills all tasks before shuting down the executor
func (e *Executor) handleShutdown(ev *executor.Event) error {
	logrus.Info("Handled SHUTDOWN event")

	// Kill all tasks
	for taskID, containerTaskInfo := range e.ContainerTasks {
		logrus.WithField("TaskID", taskID.GetValue()).Info("Killing task")

		// Stop container
		err := e.Containerizer.ContainerStop(containerTaskInfo.ContainerID)
		if err != nil {
			return err
		}

		// Update status
		status := e.newStatus(taskID)
		status.State = mesos.TASK_KILLED.Enum()
		err = e.updateStatus(status)
		if err != nil {
			return err
		}
	}

	// Shutdown
	e.Shutdown = true

	return nil
}

// handleError returns an error returned by the agent
func (e *Executor) handleError(ev *executor.Event) error {
	logrus.Info("Handled ERROR event")

	return fmt.Errorf("%s", ev.GetError().GetMessage())
}

// getUnackedTasks returns a slice of unacked tasks
func (e *Executor) getUnackedTasks() []mesos.TaskInfo {
	var tasks []mesos.TaskInfo
	for _, task := range e.UnackedTasks {
		tasks = append(tasks, task)
	}

	return tasks
}

// getUnackedUpdates returns a slice of unacked updates
func (e *Executor) getUnackedUpdates() []executor.Call_Update {
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

	// Add current update to unacked updates until we handle the acknowledgment in events
	e.UnackedUpdates[string(status.UUID)] = *u.Update

	return nil
}
