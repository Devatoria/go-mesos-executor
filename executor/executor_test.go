/*
	WE MUST DETAIL TEST RUN WITH DETAILED STEPS HERE
*/
package executor

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/mesos/mesos-go/api/v1/lib"
	"github.com/mesos/mesos-go/api/v1/lib/executor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

// ExecutorTestSuite is a struct with all what we need to run the test suite
type ExecutorTestSuite struct {
	suite.Suite
	agentInfo     mesos.AgentInfo
	config        Config
	executor      *Executor
	executorInfo  mesos.ExecutorInfo
	frameworkInfo mesos.FrameworkInfo
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

	// Executor
	s.executor = NewExecutor(s.config, nil)

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

	// Task information
	s.taskInfo = mesos.TaskInfo{
		TaskID: mesos.TaskID{Value: "fakeTaskID"},
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
	callUpdate := executor.Call_Update{
		Status: mesos.TaskStatus{},
	}

	s.executor.UnackedUpdates["fakeUUID"] = callUpdate
	assert.Contains(s.T(), s.executor.getUnackedUpdates(), callUpdate)
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

// Launch test suite
func TestExecutorSuite(t *testing.T) {
	suite.Run(t, new(ExecutorTestSuite))
}
