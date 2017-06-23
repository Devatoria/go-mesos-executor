package hook

import (
	"fmt"
	"testing"

	"github.com/Devatoria/go-mesos-executor/container"
	"github.com/Devatoria/go-mesos-executor/types"

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
	s.hook = &Hook{
		Name:     "sampleHook",
		Priority: 0,
		Execute: func(c container.Containerizer, info *types.ContainerTaskInfo) error {
			return nil
		},
	}
	s.errorHook = &Hook{
		Name:     "errorHook",
		Priority: 0,
		Execute: func(c container.Containerizer, info *types.ContainerTaskInfo) error {
			return fmt.Errorf("An error")
		},
	}
	s.disabledHook = &Hook{
		Name:     "disabledHook",
		Priority: 0,
		Execute: func(c container.Containerizer, info *types.ContainerTaskInfo) error {
			return nil
		},
	}
	s.priorityHook = &Hook{
		Name:     "priorityHook",
		Priority: 100,
		Execute: func(c container.Containerizer, info *types.ContainerTaskInfo) error {
			return nil
		},
	}
}

// Check that given enabled hooks slice is well converted to map
func (s *HookManagerTestSuite) TestNewManager() {
	// Enabled hooks slice should be well converted to map
	_, ok := s.manager.EnabledHooks["sampleHook"]
	assert.True(s.T(), ok)

	// Run slices should be empty
	assert.Empty(s.T(), s.manager.PreCreateHooks)
	assert.Empty(s.T(), s.manager.PreRunHooks)
	assert.Empty(s.T(), s.manager.PostRunHooks)
	assert.Empty(s.T(), s.manager.PreStopHooks)
	assert.Empty(s.T(), s.manager.PostRunHooks)
}

// Check that hook is added into slices when registering
func (s *HookManagerTestSuite) TestRegister() {
	// Check pre-create slice
	assert.Nil(s.T(), s.manager.RegisterHooks("pre-create", s.hook))
	assert.Equal(s.T(), s.hook, s.manager.PreCreateHooks[0])

	// Check pre-run slice
	assert.Nil(s.T(), s.manager.RegisterHooks("pre-run", s.hook))
	assert.Equal(s.T(), s.hook, s.manager.PreRunHooks[0])

	// Check post-run slice
	assert.Nil(s.T(), s.manager.RegisterHooks("post-run", s.hook))
	assert.Equal(s.T(), s.hook, s.manager.PostRunHooks[0])

	// Check pre-stop slice
	assert.Nil(s.T(), s.manager.RegisterHooks("pre-stop", s.hook))
	assert.Equal(s.T(), s.hook, s.manager.PreStopHooks[0])

	// Check post-stop slice
	assert.Nil(s.T(), s.manager.RegisterHooks("post-stop", s.hook))
	assert.Equal(s.T(), s.hook, s.manager.PostRunHooks[0])

	// An error should be thrown if a hook is registered on a wrong "when"
	assert.Error(s.T(), s.manager.RegisterHooks("never", s.hook))

	// A disabled hook should not be added into a run slice
	assert.Nil(s.T(), s.manager.RegisterHooks("pre-create", s.disabledHook))
	assert.NotContains(s.T(), s.manager.PreCreateHooks, s.disabledHook)

	// A prioritized hook should be placed before all the others in order to be ran before
	assert.Nil(s.T(), s.manager.RegisterHooks("pre-create", s.priorityHook))
	assert.Equal(s.T(), []*Hook{s.priorityHook, s.hook}, s.manager.PreCreateHooks)
}

// Check that run fuction returns nil if hook executed well, or an error if not
func (s *HookManagerTestSuite) TestPreCreateHooks() {
	s.manager.RegisterHooks("pre-create", s.hook)              // Register a working hook
	assert.NotEmpty(s.T(), s.manager.PreCreateHooks)           // Ensure it has been registered
	assert.Nil(s.T(), s.manager.RunPreCreateHooks(nil, nil))   // Ensure it doesn't throw an error on running
	s.manager.RegisterHooks("pre-create", s.errorHook)         // Register a failing hook
	assert.Error(s.T(), s.manager.RunPreCreateHooks(nil, nil)) // Ensure it throws an error on running
}

// Check that run fuction returns nil if hook executed well, or an error if not
func (s *HookManagerTestSuite) TestPreRunHooks() {
	s.manager.RegisterHooks("pre-run", s.hook)              // Register a working hook
	assert.NotEmpty(s.T(), s.manager.PreRunHooks)           // Ensure it has been registered
	assert.Nil(s.T(), s.manager.RunPreRunHooks(nil, nil))   // Ensure it doesn't throw an error on running
	s.manager.RegisterHooks("pre-run", s.errorHook)         // Register a failing hook
	assert.Error(s.T(), s.manager.RunPreRunHooks(nil, nil)) // Ensure it throws an error on running
}

// Check that run fuction returns nil if hook executed well, or an error if not
func (s *HookManagerTestSuite) TestPostRunHooks() {
	s.manager.RegisterHooks("post-run", s.hook)              // Register a working hook
	assert.NotEmpty(s.T(), s.manager.PostRunHooks)           // Ensure it has been registered
	assert.Nil(s.T(), s.manager.RunPostRunHooks(nil, nil))   // Ensure it doesn't throw an error on running
	s.manager.RegisterHooks("post-run", s.errorHook)         // Register a failing hook
	assert.Error(s.T(), s.manager.RunPostRunHooks(nil, nil)) // Ensure it throws an error on running
}

// Check that run fuction returns nil if hook executed well, or an error if not
func (s *HookManagerTestSuite) TestPreStopHooks() {
	s.manager.RegisterHooks("pre-stop", s.hook)              // Register a working hook
	assert.NotEmpty(s.T(), s.manager.PreStopHooks)           // Ensure it has been registered
	assert.Nil(s.T(), s.manager.RunPreStopHooks(nil, nil))   // Ensure it doesn't throw an error on running
	s.manager.RegisterHooks("pre-stop", s.errorHook)         // Register a failing hook
	assert.Error(s.T(), s.manager.RunPreStopHooks(nil, nil)) // Ensure it throws an error on running
}

// Check that run fuction returns nil if hook executed well, or an error if not
func (s *HookManagerTestSuite) TestPostStopHooks() {
	s.manager.RegisterHooks("post-stop", s.hook)              // Register a working hook
	assert.NotEmpty(s.T(), s.manager.PostStopHooks)           // Ensure it has been registered
	assert.Nil(s.T(), s.manager.RunPostStopHooks(nil, nil))   // Ensure it doesn't throw an error on running
	s.manager.RegisterHooks("post-stop", s.errorHook)         // Register a failing hook
	assert.Error(s.T(), s.manager.RunPostStopHooks(nil, nil)) // Ensure it throws an error on running
}

func TestHookManagerSuite(t *testing.T) {
	suite.Run(t, new(HookManagerTestSuite))
}
