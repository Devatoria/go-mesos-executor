package hook

import (
	"fmt"
	"testing"

	"github.com/Devatoria/go-mesos-executor/container"

	"github.com/mesos/mesos-go/api/v1/lib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type HookManagerTestSuite struct {
	suite.Suite
	errorHook    *Hook
	disabledHook *Hook
	manager      *Manager
	hook         *Hook
	priorityHook *Hook
}

func (s *HookManagerTestSuite) SetupTest() {
	s.manager = NewManager([]string{"sampleHook", "errorHook", "priorityHook"})
	fnil := func(c container.Containerizer, info *mesos.TaskInfo, frameworkInfo *mesos.FrameworkInfo, containerID string) error {
		return nil
	}
	ferr := func(c container.Containerizer, info *mesos.TaskInfo, frameworkInfo *mesos.FrameworkInfo, containerID string) error {
		return fmt.Errorf("an error")
	}
	s.hook = &Hook{
		Name:     "sampleHook",
		Priority: 0,
		RunPreCreate: func(c container.Containerizer, taskInfo *mesos.TaskInfo, frameworkInfo *mesos.FrameworkInfo) error {
			return nil
		},
		RunPreRun:   fnil,
		RunPostRun:  fnil,
		RunPreStop:  fnil,
		RunPostStop: fnil,
	}
	s.errorHook = &Hook{
		Name:     "errorHook",
		Priority: 0,
		RunPreCreate: func(c container.Containerizer, taskInfo *mesos.TaskInfo, frameworkInfo *mesos.FrameworkInfo) error {
			return fmt.Errorf("an error")
		},
		RunPreRun:   ferr,
		RunPostRun:  ferr,
		RunPreStop:  ferr,
		RunPostStop: ferr,
	}
	s.disabledHook = &Hook{
		Name:     "disabledHook",
		Priority: 0,
		RunPreCreate: func(c container.Containerizer, taskInfo *mesos.TaskInfo, frameworkInfo *mesos.FrameworkInfo) error {
			return nil
		},
	}
	s.priorityHook = &Hook{
		Name:     "priorityHook",
		Priority: 100,
		RunPreCreate: func(c container.Containerizer, taskInfo *mesos.TaskInfo, frameworkInfo *mesos.FrameworkInfo) error {
			return nil
		},
	}
}

// Check that given enabled hooks slice is well converted to map
func (s *HookManagerTestSuite) TestNewManager() {
	// Enabled hooks slice should be well converted to map
	_, ok := s.manager.EnabledHooks["sampleHook"]
	assert.True(s.T(), ok)

	// Hooks slice should be empty
	assert.Empty(s.T(), s.manager.Hooks)
}

// Check that hook is added into slices when registering
func (s *HookManagerTestSuite) TestRegister() {
	// Check hooks registering
	s.manager.RegisterHooks(s.hook)
	assert.Equal(s.T(), s.hook, s.manager.Hooks[0])

	// A disabled hook should not be added into a run slice
	s.manager.RegisterHooks(s.disabledHook)
	assert.NotContains(s.T(), s.manager.Hooks, s.disabledHook)

	// A prioritized hook should be placed before all the others in order to be ran before
	s.manager.RegisterHooks(s.priorityHook)
	assert.Equal(s.T(), []*Hook{s.priorityHook, s.hook}, s.manager.Hooks)
}

// Check that run fuction returns nil if hook executed well, or an error if not
func (s *HookManagerTestSuite) TestPreCreateHooks() {
	s.manager.RegisterHooks(s.hook)                                 // Register a working hook
	assert.Nil(s.T(), s.manager.RunPreCreateHooks(nil, nil, nil))   // Ensure it doesn't throw an error on running
	s.manager.RegisterHooks(s.errorHook)                            // Register a failing hook
	assert.Error(s.T(), s.manager.RunPreCreateHooks(nil, nil, nil)) // Ensure it throws an error on running
}

// Check that run fuction returns nil if hook executed well, or an error if not
func (s *HookManagerTestSuite) TestPreRunHooks() {
	s.manager.RegisterHooks(s.hook)                                  // Register a working hook
	assert.Nil(s.T(), s.manager.RunPreRunHooks(nil, nil, nil, ""))   // Ensure it doesn't throw an error on running
	s.manager.RegisterHooks(s.errorHook)                             // Register a failing hook
	assert.Error(s.T(), s.manager.RunPreRunHooks(nil, nil, nil, "")) // Ensure it throws an error on running
}

// Check that run fuction returns nil if hook executed well, or an error if not
func (s *HookManagerTestSuite) TestPostRunHooks() {
	s.manager.RegisterHooks(s.hook)                                   // Register a working hook
	assert.Nil(s.T(), s.manager.RunPostRunHooks(nil, nil, nil, ""))   // Ensure it doesn't throw an error on running
	s.manager.RegisterHooks(s.errorHook)                              // Register a failing hook
	assert.Error(s.T(), s.manager.RunPostRunHooks(nil, nil, nil, "")) // Ensure it throws an error on running
}

// Check that run fuction returns nil if hook executed well, or an error if not
func (s *HookManagerTestSuite) TestPreStopHooks() {
	s.manager.RegisterHooks(s.hook)                                 // Register a working hook
	assert.Nil(s.T(), s.manager.RunPreStopHooks(nil, nil, nil, "")) // Ensure it doesn't throw an error on running
	s.manager.RegisterHooks(s.errorHook)                            // Register a failing hook
	assert.Nil(s.T(), s.manager.RunPreStopHooks(nil, nil, nil, "")) // Ensure it doesn't throw an error on running
}

// Check that run fuction returns nil if hook executed well, or an error if not
func (s *HookManagerTestSuite) TestPostStopHooks() {
	s.manager.RegisterHooks(s.hook)                                  // Register a working hook
	assert.Nil(s.T(), s.manager.RunPostStopHooks(nil, nil, nil, "")) // Ensure it doesn't throw an error on running
	s.manager.RegisterHooks(s.errorHook)                             // Register a failing hook
	assert.Nil(s.T(), s.manager.RunPostStopHooks(nil, nil, nil, "")) // Ensure it doesn't throw an error on running
}

func TestHookManagerSuite(t *testing.T) {
	suite.Run(t, new(HookManagerTestSuite))
}
