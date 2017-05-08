package executor

import (
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
	apiEndpoint = "/api/v1/executor"
	timeout     = 10 * time.Second
)

// Executor represents an executor
type Executor struct {
	AgentInfo      mesos.AgentInfo
	Cli            *httpcli.Client
	Containerizer  container.Containerizer
	ExecutorID     string
	ExecutorInfo   mesos.ExecutorInfo
	FrameworkID    string
	FrameworkInfo  mesos.FrameworkInfo
	Handler        events.Handler
	UnackedTasks   map[mesos.TaskID]mesos.TaskInfo
	UnackedUpdates map[string]executor.Call_Update
}

// NewExecutor initializes a new executor with the given executor and framework ID
func NewExecutor(executorID, frameworkID string, containerizer container.Containerizer) *Executor {
	var e *Executor

	apiURL := url.URL{
		Scheme: "http",
		Host:   "localhost:5051", //TODO: get it from env (MESOS_AGENT_ENDPOINT)
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
		ExecutorID:     executorID,
		FrameworkID:    frameworkID,
		FrameworkInfo:  mesos.FrameworkInfo{},
		UnackedTasks:   make(map[mesos.TaskID]mesos.TaskInfo),
		UnackedUpdates: make(map[string]executor.Call_Update),
	}

	// Add events handler
	e.Handler = events.NewMux(
		events.Handle(executor.Event_SUBSCRIBED, events.HandlerFunc(e.handleSubscribed)),
		events.Handle(executor.Event_LAUNCH, events.HandlerFunc(e.handleLaunch)),
	)

	return e
}

// Execute runs the executor workflow
func (e *Executor) Execute() error {
	for {
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

			panic(err)
		}

		// We are connected, we start to handle events
		for {
			var event executor.Event
			decoder := encoding.Decoder(resp)
			err = decoder.Decode(&event)
			if err != nil {
				panic(err)
			}

			err = e.Handler.HandleEvent(&event)
			if err != nil {
				panic(err)
			}
		}
	}
}

// handleSubscribed handles subscribed events
func (e *Executor) handleSubscribed(ev *executor.Event) error {
	logrus.Info("Handled SUBSCRIBED event")
	e.AgentInfo = ev.GetSubscribed().GetAgentInfo()
	e.ExecutorInfo = ev.GetSubscribed().GetExecutorInfo()
	e.FrameworkInfo = ev.GetSubscribed().GetFrameworkInfo()

	return nil
}

// handleLaunch put given task in unacked tasks and launches it
func (e *Executor) handleLaunch(ev *executor.Event) error {
	logrus.Info("Handled LAUNCH event")
	task := ev.GetLaunch().GetTask()
	e.UnackedTasks[task.GetTaskID()] = task

	// Launch container
	err := e.Containerizer.ContainerRun("mesostest", task.GetContainer().GetDocker().GetImage())
	if err != nil {
		return err
	}

	// Update status to RUNNING
	status := e.newStatus(task.GetTaskID())
	status.State = mesos.TASK_RUNNING.Enum()

	return e.updateStatus(status)
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
