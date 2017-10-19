package executor

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"syscall"
	"testing"
	"time"

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
	ferr := func(c container.Containerizer, t *mesos.TaskInfo, containerID string) error {
		return fmt.Errorf("An error")
	}
	s.errorHook = &hook.Hook{
		Name:     "error",
		Priority: 0,
		RunPreCreate: func(c container.Containerizer, t *mesos.TaskInfo) error {
			return fmt.Errorf("An error")
		},
		RunPreRun:   ferr,
		RunPostRun:  ferr,
		RunPreStop:  ferr,
		RunPostStop: ferr,
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
	assert.Empty(s.T(), s.executor.UnackedUpdates)

	// Add fake update
	s.executor.UnackedUpdates["fakeUUID"] = s.callUpdate

	// Unacked should not be empty
	assert.Contains(s.T(), s.executor.getUnackedUpdates(), s.callUpdate)
	status := s.callUpdate.GetStatus()
	evAckUpdate := executor.Event_Acknowledged{
		UUID: status.GetUUID(),
	}
	ev := executor.Event{
		Acknowledged: &evAckUpdate,
	}
	assert.Nil(s.T(), s.executor.handleAcknowledged(&ev))
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
	assert.Empty(s.T(), s.executor.UnackedUpdates)

	// Generating fake event
	evLaunch := executor.Event_Launch{
		Task: s.taskInfo,
	}
	ev := executor.Event{
		Launch: &evLaunch,
	}

	// Should return an error if a hook fails during launch
	s.executor.HookManager.RegisterHooks(s.errorHook)
	assert.Error(s.T(), s.executor.handleLaunch(&ev))
	assert.Equal(s.T(), *mesos.TASK_FAILED.Enum(), *pullFirstUpdate(s.executor.UnackedUpdates).Status.State) // Should be a TASK_FAILED update

	// Nominal case (long-running container)
	s.executor.HookManager.Hooks = []*hook.Hook{}                                                             // Remove previously added failing hooks
	assert.Nil(s.T(), s.executor.handleLaunch(&ev))                                                           // Should return nil (launch successful)
	assert.NotEmpty(s.T(), s.executor.UnackedUpdates)                                                         // Should not be empty (TASK_RUNNING update)
	assert.Equal(s.T(), *mesos.TASK_RUNNING.Enum(), *pullFirstUpdate(s.executor.UnackedUpdates).Status.State) // Should be a TASK_RUNNING update
	assert.Equal(s.T(), "fakeContainerID", s.executor.ContainerID)                                            // Should be equal to the container ID
	assert.Empty(s.T(), s.executor.UnackedUpdates)                                                            // Should be empty
	close(done)                                                                                               // Simulate container exit
	for len(s.executor.UnackedUpdates) == 0 {                                                                 // Wait for TASK_FINISHED update to be sent (async)
		<-time.After(100 * time.Millisecond)
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
	assert.Empty(s.T(), s.executor.UnackedUpdates)

	// Add a fake running container task
	s.executor.ContainerID = "fakeContainerID"
	s.executor.TaskInfo = s.taskInfo

	// Generating fake event
	evKill := executor.Event_Kill{
		TaskID: s.taskInfo.GetTaskID(),
	}
	ev := executor.Event{
		Kill: &evKill,
	}

	// Nominal case
	s.executor.HookManager.RegisterHooks(s.errorHook)
	assert.Nil(s.T(), s.executor.handleKill(&ev))                                                            // Should return nil (kill successful, even with hook failure)
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
	assert.Empty(s.T(), s.executor.UnackedUpdates)

	// Add a fake running container task
	s.executor.ContainerID = "fakeContainerID"
	s.executor.TaskInfo = s.taskInfo

	// Generating fake event
	ev := executor.Event{}

	// Nominal case
	s.executor.HookManager.RegisterHooks(s.errorHook)
	assert.Nil(s.T(), s.executor.handleShutdown(&ev))                                                        // Should return nil (kill successful, even with hook failure)
	assert.NotEmpty(s.T(), s.executor.UnackedUpdates)                                                        // Should not be empty (TASK_KILLED update)
	assert.Equal(s.T(), *mesos.TASK_KILLED.Enum(), *pullFirstUpdate(s.executor.UnackedUpdates).Status.State) // Should be a TASK_KILLED update
	assert.True(s.T(), s.executor.Shutdown)                                                                  // Should be set to true in order to stop main loops
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
	s.executor.TaskInfo = s.taskInfo
	taskStatus := s.executor.newStatus()
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
	s.executor.TaskInfo = s.taskInfo
	taskStatus := s.executor.newStatus()
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
	s.executor.HealthChecker = healthcheck.NewChecker(0, nil, "", nil)
	s.executor.TaskInfo = s.taskInfo

	// Health state update should update task status
	monkey.PatchInstanceMethod(reflect.TypeOf(s.executor.HealthChecker), "Run", func(c *healthcheck.Checker) {
		c.Healthy <- true      // Simulate health state update from checker
		c.Exited <- struct{}{} // Simulate checker exit (in order to stop loop)
	})
	s.executor.healthCheck()
	update := pullFirstUpdate(s.executor.UnackedUpdates)
	assert.Equal(s.T(), true, *update.Status.Healthy)
	assert.Equal(s.T(), *mesos.TASK_RUNNING.Enum(), *update.Status.State)

	// A done from the checker should kill the task
	monkey.PatchInstanceMethod(reflect.TypeOf(s.executor.HealthChecker), "Run", func(c *healthcheck.Checker) {
		// Simulate health checker quit handling
		// because tearDown function (triggered by handleKill)
		// will try to shutdown the checker
		go func() {
			<-c.Quit
		}()
		c.Done <- struct{}{} // Simulate checker stop signal
	})
	s.executor.healthCheck()
	assert.Len(s.T(), s.executor.UnackedUpdates, 1) // Should contain an update (KILLED or FAILED)
}

func (s *ExecutorTestSuite) TestTearDown() {
	// Create fake checker for task
	s.executor.HealthChecker = healthcheck.NewChecker(0, nil, "", nil)

	// Handle the fake checker exit (avoid deadlock)
	go func() {
		<-s.executor.HealthChecker.Quit
	}()

	// Create a fake container task
	s.executor.ContainerID = "fakeContainerID"
	s.executor.TaskInfo = s.taskInfo

	// Patch functions in order to catch calls
	runPreStopHooksCalled := false
	monkey.PatchInstanceMethod(reflect.TypeOf(s.executor.HookManager), "RunPreStopHooks", func(_ *hook.Manager, c container.Containerizer, info *mesos.TaskInfo, containerID string) error {
		runPreStopHooksCalled = true

		return nil
	})

	runPostStopHooksCalled := false
	monkey.PatchInstanceMethod(reflect.TypeOf(s.executor.HookManager), "RunPostStopHooks", func(_ *hook.Manager, c container.Containerizer, info *mesos.TaskInfo, containerID string) error {
		runPostStopHooksCalled = true

		return nil
	})

	runContainerStop := false
	monkey.PatchInstanceMethod(reflect.TypeOf(s.executor.Containerizer), "ContainerStop", func(_ *types.FakeContainerizer, id string) error {
		runContainerStop = true

		return nil
	})

	// Nominal case
	// - should execute the pre/post stop hooks
	// - should stop the container
	// - should remove the associated task
	s.executor.tearDown()
	assert.True(s.T(), runPreStopHooksCalled)  // Should be true (pre-stop hooks ran)
	assert.True(s.T(), runContainerStop)       // Should be true (container should be stopped)
	assert.True(s.T(), runPostStopHooksCalled) // Should be true (post-stop hooks ran)

	monkey.UnpatchAll()
}

func (s *ExecutorTestSuite) TestWaitContainer() {
	s.executor.ContainerID = "fakeContainerID"
	s.executor.TaskInfo = s.taskInfo

	// Nominal case
	// - should wait for container
	// - should send a TASK_FINISHED update
	assert.Nil(s.T(), s.executor.waitContainer())                                                              // Should be nil (container exited, waited successfuly)
	assert.NotEmpty(s.T(), s.executor.UnackedUpdates)                                                          // Should not be empty (containing finished update)
	assert.Equal(s.T(), *mesos.TASK_FINISHED.Enum(), *pullFirstUpdate(s.executor.UnackedUpdates).Status.State) // Should be a TASK_FINISHED update

	// Error case (exit code is not 0)
	monkey.PatchInstanceMethod(reflect.TypeOf(s.executor.Containerizer), "ContainerWait", func(_ *types.FakeContainerizer, id string) (int, error) {
		return 1, nil
	})
	defer monkey.UnpatchAll()

	assert.Error(s.T(), s.executor.waitContainer())                                                          // Should throw an error (container exited with non-zero code)
	assert.NotEmpty(s.T(), s.executor.UnackedUpdates)                                                        // Should not be empty
	assert.Equal(s.T(), *mesos.TASK_FAILED.Enum(), *pullFirstUpdate(s.executor.UnackedUpdates).Status.State) // Should be a TASK_FAILED update
}

// Check that a trapped signal triggers the shutdown of the executor
func (s *ExecutorTestSuite) TestHandleStopSignals() {
	go func() {
		s.executor.StopSignals <- syscall.SIGHUP
	}()

	assert.False(s.T(), s.executor.Shutdown)
	s.executor.handleStopSignals()
	assert.True(s.T(), s.executor.Shutdown)
	s.executor.Shutdown = false
}

func (s *ExecutorTestSuite) TestThrowError() {
	defer monkey.UnpatchAll()

	// Patch functions in order to catch calls
	runPreStopHooksCalled := false
	monkey.PatchInstanceMethod(reflect.TypeOf(s.executor.HookManager), "RunPreStopHooks", func(_ *hook.Manager, c container.Containerizer, info *mesos.TaskInfo, containerID string) error {
		runPreStopHooksCalled = true

		return nil
	})

	runPostStopHooksCalled := false
	monkey.PatchInstanceMethod(reflect.TypeOf(s.executor.HookManager), "RunPostStopHooks", func(_ *hook.Manager, c container.Containerizer, info *mesos.TaskInfo, containerID string) error {
		runPostStopHooksCalled = true

		return nil
	})

	runContainerStop := false
	monkey.PatchInstanceMethod(reflect.TypeOf(s.executor.Containerizer), "ContainerStop", func(_ *types.FakeContainerizer, id string) error {
		runContainerStop = true

		return nil
	})

	err := errors.New("an error")                                        // Generate a random error
	assert.Equal(s.T(), err, s.executor.throwError(err))                 // Should return the given error
	assert.NotEmpty(s.T(), s.executor.UnackedUpdates)                    // Should contain an update
	update := pullFirstUpdate(s.executor.UnackedUpdates)                 // Get the thrown update
	assert.Equal(s.T(), *mesos.TASK_FAILED.Enum(), *update.Status.State) // Should be a TASK_FAILED update
	assert.Equal(s.T(), err.Error(), *update.Status.Message)             // Should be the same update message as the given error message
	assert.True(s.T(), runPreStopHooksCalled)                            // Should be true (pre-stop hooks ran)
	assert.True(s.T(), runContainerStop)                                 // Should be true (container should be stopped)
	assert.True(s.T(), runPostStopHooksCalled)                           // Should be true (post-stop hooks ran)
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
