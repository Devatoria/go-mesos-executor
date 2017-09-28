package executor

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"github.com/Devatoria/go-mesos-executor/container"
	"github.com/Devatoria/go-mesos-executor/healthcheck"
	"github.com/Devatoria/go-mesos-executor/hook"
	"github.com/Devatoria/go-mesos-executor/types"
	"github.com/bouk/monkey"

	"github.com/mesos/mesos-go/api/v1/lib"
	"github.com/mesos/mesos-go/api/v1/lib/executor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

// ExecutorTestSuite is a struct with all what we need to run the test suite
type ExecutorTestSuite struct {
	suite.Suite
	agentInfo     mesos.AgentInfo
	callUpdate    executor.Call_Update
	config        Config
	containerizer *types.FakeContainerizer
	cpusResource  mesos.Resource
	errorHook     *hook.Hook
	executor      *Executor
	executorInfo  mesos.ExecutorInfo
	frameworkInfo mesos.FrameworkInfo
	hookManager   *hook.Manager
	memResource   mesos.Resource
	server        *httptest.Server
	taskInfo      mesos.TaskInfo
}

// SetupTest prepares each test (in order to start each test with the same state)
func (s *ExecutorTestSuite) SetupTest() {
	var agentPort int32 = 5051

	// Fake server used to mock HTTP mesos agent API
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	sURL, err := url.Parse(s.server.URL)
	if err != nil {
		panic(err)
	}

	// Executor configuration
	s.config = Config{
		AgentEndpoint: sURL.Host,
		ExecutorID:    "fakeExecutorID",
		FrameworkID:   "fakeFrameworkID",
	}

	// Containerizer
	s.containerizer = &types.FakeContainerizer{}

	// Error hook
	s.errorHook = &hook.Hook{
		Name:     "error",
		Priority: 0,
		Execute: func(c container.Containerizer, info *types.ContainerTaskInfo) error {
			return fmt.Errorf("An error")
		},
	}

	// Hooks manager
	s.hookManager = hook.NewManager([]string{"error"})

	// Executor
	s.executor = NewExecutor(s.config, s.containerizer, s.hookManager)

	// Agent information
	s.agentInfo = mesos.AgentInfo{
		ID:       &mesos.AgentID{Value: "fakeAgentID"},
		Hostname: "fakeAgentHostname",
		Port:     &agentPort,
	}

	// Executor information
	s.executorInfo = mesos.ExecutorInfo{
		ExecutorID: mesos.ExecutorID{Value: s.config.ExecutorID},
	}

	// Framework information
	s.frameworkInfo = mesos.FrameworkInfo{
		ID: &mesos.FrameworkID{Value: s.config.FrameworkID},
	}

	// Resources
	s.cpusResource = mesos.Resource{
		Name:   "cpus",
		Type:   mesos.SCALAR.Enum(),
		Scalar: &mesos.Value_Scalar{Value: 2},
	}
	s.memResource = mesos.Resource{
		Name:   "mem",
		Type:   mesos.SCALAR.Enum(),
		Scalar: &mesos.Value_Scalar{Value: 512},
	}

	// Task information
	s.taskInfo = mesos.TaskInfo{
		TaskID: mesos.TaskID{Value: "fakeTaskID"},
		Resources: []mesos.Resource{
			s.cpusResource,
			s.memResource,
		},
	}

	// Call update information
	s.callUpdate = executor.Call_Update{
		Status: mesos.TaskStatus{
			UUID: []byte("fakeUUID"),
		},
	}
}

// Just test the newly created executor values
func (s *ExecutorTestSuite) TestNewExecutor() {
	assert.Equal(s.T(), fmt.Sprintf("http://%s/api/v1/executor", s.config.AgentEndpoint), s.executor.Cli.Endpoint())
	assert.Equal(s.T(), s.executor.ExecutorID, s.config.ExecutorID)
	assert.Equal(s.T(), s.executor.FrameworkID, s.config.FrameworkID)
}

// Test that agent, executor and framework informations are updated on subscribe
func (s *ExecutorTestSuite) TestHandleSubscribed() {
	// Prepare data
	evSub := executor.Event_Subscribed{
		AgentInfo:     s.agentInfo,
		ExecutorInfo:  s.executorInfo,
		FrameworkInfo: s.frameworkInfo,
	}
	ev := executor.Event{
		Subscribed: &evSub,
	}

	// Check that actually everything is empty in executor data
	assert.Equal(s.T(), mesos.AgentInfo{}, s.executor.AgentInfo)
	assert.Equal(s.T(), mesos.ExecutorInfo{}, s.executor.ExecutorInfo)
	assert.Equal(s.T(), mesos.FrameworkInfo{}, s.executor.FrameworkInfo)

	// Handle fake event
	assert.Nil(s.T(), s.executor.handleSubscribed(&ev))

	// Check that data are now up-to-date
	assert.Equal(s.T(), s.agentInfo, s.executor.AgentInfo)
	assert.Equal(s.T(), s.executorInfo, s.executor.ExecutorInfo)
	assert.Equal(s.T(), s.frameworkInfo, s.executor.FrameworkInfo)
}

// Check that we're not throwing an error on message receive
func (s *ExecutorTestSuite) TestHandleMessage() {
	// Prepare event
	evMsg := executor.Event_Message{
		Data: []byte("fake message"),
	}
	ev := executor.Event{
		Message: &evMsg,
	}

	// Handle fake event
	assert.Nil(s.T(), s.executor.handleMessage(&ev))
}

// Check that we're throwing an error on error handling
func (s *ExecutorTestSuite) TestHandleError() {
	// Prepare event
	evErr := executor.Event_Error{
		Message: "fake message",
	}
	ev := executor.Event{
		Error: &evErr,
	}

	// Handle fake event
	assert.Error(s.T(), s.executor.handleError(&ev))
}

// Check that given task/update is removed from unacked
func (s *ExecutorTestSuite) TestHandleAcknowledged() {
	// Unacked should be empty
	assert.Empty(s.T(), s.executor.UnackedTasks)
	assert.Empty(s.T(), s.executor.UnackedUpdates)

	// Add fake task/update
	s.executor.UnackedTasks[s.taskInfo.TaskID] = s.taskInfo
	s.executor.UnackedUpdates["fakeUUID"] = s.callUpdate

	// Unacked should not be empty
	assert.Contains(s.T(), s.executor.getUnackedTasks(), s.taskInfo)
	assert.Contains(s.T(), s.executor.getUnackedUpdates(), s.callUpdate)

	// Generate fake event
	evAckTask := executor.Event_Acknowledged{
		TaskID: s.taskInfo.TaskID,
	}
	ev1 := executor.Event{
		Acknowledged: &evAckTask,
	}
	assert.Nil(s.T(), s.executor.handleAcknowledged(&ev1))
	assert.Empty(s.T(), s.executor.getUnackedTasks())

	status := s.callUpdate.GetStatus()
	evAckUpdate := executor.Event_Acknowledged{
		UUID: status.GetUUID(),
	}
	ev2 := executor.Event{
		Acknowledged: &evAckUpdate,
	}
	assert.Nil(s.T(), s.executor.handleAcknowledged(&ev2))
	assert.Empty(s.T(), s.executor.getUnackedUpdates())
}

// Check that:
// - a task is pushed in unacks
// - a task is pushed in container tasks
// - a status update (RUNNING) is pushed in unacks
// - returns an error if a pre-create/pre-run/post-run hook fails
func (s *ExecutorTestSuite) TestHandleLaunch() {
	// Patch waitContainer in order to fake a long-running container (never stopped)
	done := make(chan struct{})
	monkey.PatchInstanceMethod(reflect.TypeOf(s.executor.Containerizer), "ContainerWait", func(_ *types.FakeContainerizer, _ string) (int, error) {
		select {
		case <-done:
			break
		}

		return 0, nil
	})
	defer monkey.UnpatchAll()

	// Unacked tasks/updates should be empty
	assert.Empty(s.T(), s.executor.UnackedTasks)
	assert.Empty(s.T(), s.executor.UnackedUpdates)

	// Generating fake event
	evLaunch := executor.Event_Launch{
		Task: s.taskInfo,
	}
	ev := executor.Event{
		Launch: &evLaunch,
	}

	// Should return an error if a hook fails during launch
	// Pre-create hook case
	s.executor.HookManager.RegisterHooks("pre-create", s.errorHook)
	assert.Error(s.T(), s.executor.handleLaunch(&ev))
	assert.Equal(s.T(), *mesos.TASK_FAILED.Enum(), *pullFirstUpdate(s.executor.UnackedUpdates).Status.State) // Should be a TASK_FAILED update

	// Pre-run hook case
	s.executor.HookManager.PreCreateHooks = []*hook.Hook{}
	s.executor.HookManager.RegisterHooks("pre-run", s.errorHook)
	assert.Error(s.T(), s.executor.handleLaunch(&ev))
	assert.Equal(s.T(), *mesos.TASK_FAILED.Enum(), *pullFirstUpdate(s.executor.UnackedUpdates).Status.State) // Should be a TASK_FAILED update

	// Post-run hook case
	s.executor.HookManager.PreRunHooks = []*hook.Hook{}
	s.executor.HookManager.RegisterHooks("post-run", s.errorHook)
	assert.Error(s.T(), s.executor.handleLaunch(&ev))
	assert.Equal(s.T(), *mesos.TASK_FAILED.Enum(), *pullFirstUpdate(s.executor.UnackedUpdates).Status.State) // Should be a TASK_FAILED update

	// Nominal case (long-running container)
	s.executor.HookManager.PostRunHooks = []*hook.Hook{}                                                      // Remove previously added failing hooks
	assert.Nil(s.T(), s.executor.handleLaunch(&ev))                                                           // Should return nil (launch successful)
	assert.NotEmpty(s.T(), s.executor.UnackedTasks)                                                           // Should not be empty (task waiting for acknowledgment)
	assert.NotEmpty(s.T(), s.executor.ContainerTasks)                                                         // Should not be empty (new running task for this container)
	assert.NotEmpty(s.T(), s.executor.UnackedUpdates)                                                         // Should not be empty (TASK_RUNNING update)
	assert.Equal(s.T(), *mesos.TASK_RUNNING.Enum(), *pullFirstUpdate(s.executor.UnackedUpdates).Status.State) // Should be a TASK_RUNNING update
	containerTask := s.executor.ContainerTasks[s.taskInfo.GetTaskID()]                                        // Get running task
	assert.Equal(s.T(), "fakeContainerID", containerTask.ContainerID)                                         // Should be equal to the container ID
	assert.Empty(s.T(), s.executor.UnackedUpdates)                                                            // Should be empty
	close(done)                                                                                               // Simulate container exit
	for len(s.executor.UnackedUpdates) == 0 {                                                                 // Wait for TASK_FINISHED update to be sent (async)
	}
	assert.Equal(s.T(), *mesos.TASK_FINISHED.Enum(), *pullFirstUpdate(s.executor.UnackedUpdates).Status.State) // Should be a TASK_FINISHED update
}

// Check that:
// - container tasks is emptied
// - a TASK_KILLED update is added to unacked
// - returns an error if a pre-stop/post-stop hook fail
// - returns nil
func (s *ExecutorTestSuite) TestHandleKill() {
	// Unacked should be empty
	assert.Empty(s.T(), s.executor.UnackedTasks)
	assert.Empty(s.T(), s.executor.UnackedUpdates)

	// Add a fake running container task
	s.executor.ContainerTasks[s.taskInfo.GetTaskID()] = &types.ContainerTaskInfo{
		ContainerID: "fakeContainerID",
		TaskInfo:    s.taskInfo,
	}

	// Generating fake event
	evKill := executor.Event_Kill{
		TaskID: s.taskInfo.GetTaskID(),
	}
	ev := executor.Event{
		Kill: &evKill,
	}

	// Should throw an error on hook fail
	// Pre-stop hook case
	s.executor.HookManager.RegisterHooks("pre-stop", s.errorHook)
	assert.Error(s.T(), s.executor.handleKill(&ev))
	assert.Equal(s.T(), *mesos.TASK_FAILED.Enum(), *pullFirstUpdate(s.executor.UnackedUpdates).Status.State) // Should be a TASK_FAILED update

	// Post-stop hook case
	s.executor.HookManager.PreStopHooks = []*hook.Hook{}
	s.executor.HookManager.RegisterHooks("post-stop", s.errorHook)
	assert.Error(s.T(), s.executor.handleKill(&ev))
	assert.Equal(s.T(), *mesos.TASK_FAILED.Enum(), *pullFirstUpdate(s.executor.UnackedUpdates).Status.State) // Should be a TASK_FAILED update

	// Nominal case
	s.executor.HookManager.PostStopHooks = []*hook.Hook{}
	assert.Nil(s.T(), s.executor.handleKill(&ev))  // Should return nil (kill successful)
	assert.Empty(s.T(), s.executor.ContainerTasks) // Should be empty (task removed from container tasks)

	assert.NotEmpty(s.T(), s.executor.UnackedUpdates)                                                        // Should not be empty (TASK_KILLED update)
	assert.Equal(s.T(), *mesos.TASK_KILLED.Enum(), *pullFirstUpdate(s.executor.UnackedUpdates).Status.State) // Should be a TASK_KILLED update
}

// Check that:
// - all container tasks are killed (but not removed from container tasks, only TASK_KILLED update)
// - shutdown is set to true
// - returns nil
func (s *ExecutorTestSuite) TestHandleShutdown() {
	// Should be set to false (default value)
	assert.False(s.T(), s.executor.Shutdown)

	// Unacked should be empty
	assert.Empty(s.T(), s.executor.UnackedTasks)
	assert.Empty(s.T(), s.executor.UnackedUpdates)

	// Add a fake running container task
	s.executor.ContainerTasks[s.taskInfo.GetTaskID()] = &types.ContainerTaskInfo{
		ContainerID: "fakeContainerID",
		TaskInfo:    s.taskInfo,
	}

	// Generating fake event
	ev := executor.Event{}

	// Should throw an error on hook fail
	// Pre-stop hook case
	s.executor.HookManager.RegisterHooks("pre-stop", s.errorHook)
	assert.Nil(s.T(), s.executor.handleShutdown(&ev))
	assert.Equal(s.T(), *mesos.TASK_FAILED.Enum(), *pullFirstUpdate(s.executor.UnackedUpdates).Status.State) // Should be a TASK_FAILED update

	// Post-stop hook case
	s.executor.HookManager.PreStopHooks = []*hook.Hook{}
	s.executor.HookManager.RegisterHooks("post-stop", s.errorHook)
	assert.Nil(s.T(), s.executor.handleShutdown(&ev))
	assert.Equal(s.T(), *mesos.TASK_FAILED.Enum(), *pullFirstUpdate(s.executor.UnackedUpdates).Status.State) // Should be a TASK_FAILED update

	// Nominal case
	s.executor.HookManager.PostStopHooks = []*hook.Hook{}
	assert.Nil(s.T(), s.executor.handleShutdown(&ev)) // Should return nil (kill successful)
	assert.Empty(s.T(), s.executor.ContainerTasks)    // Should be empty (tasks are removed one by one by teardown function)

	assert.NotEmpty(s.T(), s.executor.UnackedUpdates)                                                        // Should not be empty (TASK_KILLED update)
	assert.Equal(s.T(), *mesos.TASK_KILLED.Enum(), *pullFirstUpdate(s.executor.UnackedUpdates).Status.State) // Should be a TASK_KILLED update
	assert.True(s.T(), s.executor.Shutdown)                                                                  // Should be set to true in order to stop main loops
}

// Check that we are receiving everything that we should
func (s *ExecutorTestSuite) TestGetUnackedTasks() {
	// Should be nil on initialize
	assert.Nil(s.T(), s.executor.getUnackedTasks())

	// Add some tasks
	s.executor.UnackedTasks[s.taskInfo.TaskID] = s.taskInfo
	assert.Contains(s.T(), s.executor.getUnackedTasks(), s.taskInfo)
}

// Check that we are receiving everything that we should
func (s *ExecutorTestSuite) TestGetUnackedUpdates() {
	// Should be nil on initialize
	assert.Nil(s.T(), s.executor.getUnackedUpdates())

	// Add some tasks
	status := s.callUpdate.GetStatus()
	s.executor.UnackedUpdates[string(status.GetUUID())] = s.callUpdate
	assert.Contains(s.T(), s.executor.getUnackedUpdates(), s.callUpdate)
}

// Check that generated status is as it should be
func (s *ExecutorTestSuite) TestNewStatus() {
	taskStatus := s.executor.newStatus(s.taskInfo.TaskID)
	executorID := s.executor.ExecutorInfo.GetExecutorID()
	expected := mesos.TaskStatus{
		ExecutorID: &executorID,
		Source:     mesos.SOURCE_EXECUTOR.Enum(),
		TaskID:     s.taskInfo.TaskID,
		UUID:       taskStatus.UUID, // Because it is randomly generated, we must ensure we're using the same UUID
	}

	assert.Equal(s.T(), expected, taskStatus)
}

// Check that updating a status adds an update to the unacked updates
func (s *ExecutorTestSuite) TestUpdateStatus() {
	// Should be empty
	assert.Empty(s.T(), s.executor.UnackedUpdates)

	// Update status
	taskStatus := s.executor.newStatus(s.taskInfo.TaskID)
	taskStatus.State = mesos.TASK_RUNNING.Enum()

	assert.Empty(s.T(), s.executor.UnackedUpdates)         // Should be empty before updating status
	assert.Nil(s.T(), s.executor.updateStatus(taskStatus)) // Should be nil (update OK)
	assert.NotEmpty(s.T(), s.executor.UnackedUpdates)      // Should contain status update (waiting for an acknowledgment)
}

// Check that we retrieve the asked resource, or an error if missing
func (s *ExecutorTestSuite) TestGetResource() {
	// Should fail on missing resource
	_, err := getResource(s.taskInfo, "missingResource")
	assert.NotNil(s.T(), err)

	// Should return the requested resource
	res, err := getResource(s.taskInfo, "mem")
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), s.memResource, res)
}

// Check that we retrieve the memory resource, or an error when not existing
func (s *ExecutorTestSuite) TestGetMemoryLimit() {
	// Should return the memory resource value
	value, err := getMemoryLimit(s.taskInfo)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), uint64(s.memResource.GetScalar().GetValue()*1024*1024), value)

	// Should return an error when resource is missing
	s.taskInfo.Resources = []mesos.Resource{}
	_, err = getMemoryLimit(s.taskInfo)
	assert.NotNil(s.T(), err)
}

// Check that we retrieve the cpu shares resource, or an error when not existing
func (s *ExecutorTestSuite) TestGetCPUSharesLimit() {
	// Should return the CPU shares resource value
	value, err := getCPUSharesLimit(s.taskInfo)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), uint64(s.cpusResource.GetScalar().GetValue()*cpuSharesPerCPU), value)

	// Should return an error when resource is missing
	s.taskInfo.Resources = []mesos.Resource{}
	_, err = getCPUSharesLimit(s.taskInfo)
	assert.NotNil(s.T(), err)
}

// Check that:
// - receive healthy states throws a status update
// - receive done from checker kills the associated task
func (s *ExecutorTestSuite) TestHealthCheck() {
	defer monkey.UnpatchAll()

	// Create fake checker for task
	checker := healthcheck.NewChecker(0, nil, "", nil)
	taskID := mesos.TaskID{
		Value: "fakeTaskID",
	}
	s.executor.HealthCheckers[taskID] = checker

	// Health state update should update task status
	monkey.PatchInstanceMethod(reflect.TypeOf(checker), "Run", func(c *healthcheck.Checker) {
		c.Healthy <- true      // Simulate health state update from checker
		c.Exited <- struct{}{} // Simulate checker exit (in order to stop loop)
	})
	s.executor.healthCheck(taskID)
	update := pullFirstUpdate(s.executor.UnackedUpdates)
	assert.Equal(s.T(), true, *update.Status.Healthy)
	assert.Equal(s.T(), *mesos.TASK_RUNNING.Enum(), *update.Status.State)

	// A done from the checker should kill the task
	monkey.PatchInstanceMethod(reflect.TypeOf(checker), "Run", func(c *healthcheck.Checker) {
		c.Done <- struct{}{} // Simulate checker stop signal
	})
	s.executor.healthCheck(taskID)
	assert.Len(s.T(), s.executor.UnackedUpdates, 1) // Should contain an update (KILLED or FAILED)
}

func (s *ExecutorTestSuite) TestTearDownTask() {
	// Create fake checker for task
	checker := healthcheck.NewChecker(0, nil, "", nil)
	taskID := mesos.TaskID{
		Value: s.taskInfo.TaskID.Value,
	}
	s.executor.HealthCheckers[taskID] = checker

	// Handle the fake checker exit (avoid deadlock)
	go func() {
		<-checker.Quit
	}()

	// Create a fake container task
	containerTaskInfo := &types.ContainerTaskInfo{
		ContainerID: "fakeContainerID",
		TaskInfo:    s.taskInfo,
	}
	s.executor.ContainerTasks[s.taskInfo.TaskID] = containerTaskInfo

	// Patch functions in order to catch calls
	runPreStopHooksCalled := false
	monkey.PatchInstanceMethod(reflect.TypeOf(s.executor.HookManager), "RunPreStopHooks", func(_ *hook.Manager, c container.Containerizer, info *types.ContainerTaskInfo) error {
		runPreStopHooksCalled = true

		return nil
	})

	runPostStopHooksCalled := false
	monkey.PatchInstanceMethod(reflect.TypeOf(s.executor.HookManager), "RunPostStopHooks", func(_ *hook.Manager, c container.Containerizer, info *types.ContainerTaskInfo) error {
		runPostStopHooksCalled = true

		return nil
	})

	runContainerStop := false
	monkey.PatchInstanceMethod(reflect.TypeOf(s.executor.Containerizer), "ContainerStop", func(_ *types.FakeContainerizer, id string) error {
		runContainerStop = true

		return nil
	})

	// Nominal case
	// - should stop and remove the checker
	// - should execute the pre/post stop hooks
	// - should stop the container
	// - should remove the associated task
	assert.Nil(s.T(), s.executor.tearDownTask(taskID, containerTaskInfo)) // Should be nil (no error)
	assert.Empty(s.T(), s.executor.HealthCheckers)                        // Should be empty (no more checkers)
	assert.True(s.T(), runPreStopHooksCalled)                             // Should be true (pre-stop hooks ran)
	assert.True(s.T(), runContainerStop)                                  // Should be true (container should be stopped)
	assert.True(s.T(), runPostStopHooksCalled)                            // Should be true (post-stop hooks ran)
	assert.Empty(s.T(), s.executor.ContainerTasks)                        // Should be empty (no more tasks)

	monkey.UnpatchAll()
}

func (s *ExecutorTestSuite) TestWaitContainer() {
	taskID := mesos.TaskID{
		Value: s.taskInfo.TaskID.Value,
	}
	containerTaskInfo := &types.ContainerTaskInfo{
		ContainerID: "fakeContainerID",
		TaskInfo:    s.taskInfo,
	}

	// Nominal case
	// - should wait for container
	// - should send a TASK_FINISHED update
	assert.Nil(s.T(), s.executor.waitContainer(containerTaskInfo, taskID))                                     // Should be nil (container exited, waited successfuly)
	assert.NotEmpty(s.T(), s.executor.UnackedUpdates)                                                          // Should not be empty (containing finished update)
	assert.Equal(s.T(), *mesos.TASK_FINISHED.Enum(), *pullFirstUpdate(s.executor.UnackedUpdates).Status.State) // Should be a TASK_FINISHED update
}

// Launch test suite
func TestExecutorSuite(t *testing.T) {
	suite.Run(t, new(ExecutorTestSuite))
}

func pullFirstUpdate(m map[string]executor.Call_Update) *executor.Call_Update {
	var key string
	var update *executor.Call_Update
	for k, u := range m {
		key = k
		update = &u
		break
	}

	if update != nil {
		delete(m, key)
	}

	return update
}
