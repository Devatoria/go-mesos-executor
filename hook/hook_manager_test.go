package hook

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type HookManagerTestSuite struct {
	suite.Suite
	errorHook    ErrorHook
	disabledHook disabledHook
	manager      *Manager
	hook         SampleHook
	priorityHook priorityHook
}

type priorityHook struct{}

func (h priorityHook) GetName() string {
	return "priority"
}

func (h priorityHook) GetPriority() int64 {
	return 100
}

type disabledHook struct{}

func (h disabledHook) GetName() string {
	return "disabled"
}

func (h disabledHook) GetPriority() int64 {
	return 0
}

func (s *HookManagerTestSuite) SetupTest() {
	s.manager = NewManager([]string{"sample", "error", "priority"})
	s.hook = SampleHook{}
	s.errorHook = ErrorHook{}
	s.disabledHook = disabledHook{}
	s.priorityHook = priorityHook{}
}

// Check that given enabled hooks slice is well converted to map
func (s *HookManagerTestSuite) TestNewManager() {
	// Enabled hooks slice should be well converted to map
	_, ok := s.manager.EnabledHooks["sample"]
	assert.True(s.T(), ok)

	// Hooks slice should be empty
	assert.Empty(s.T(), s.manager.Hooks)
}

// Check that hook is added into slices when registering
func (s *HookManagerTestSuite) TestRegister() {
	// Check hooks registering
	assert.Nil(s.T(), s.manager.RegisterHooks(s.hook))
	assert.Equal(s.T(), s.hook, s.manager.Hooks[0])

	// A disabled hook should not be added into a run slice
	assert.Nil(s.T(), s.manager.RegisterHooks(s.disabledHook))
	assert.NotContains(s.T(), s.manager.Hooks, s.disabledHook)

	// A prioritized hook should be placed before all the others in order to be ran before
	assert.Nil(s.T(), s.manager.RegisterHooks(s.priorityHook))
	assert.Equal(s.T(), []RunnableHook{s.priorityHook, s.hook}, s.manager.Hooks)
}

// Check that run fuction returns nil if hook executed well, or an error if not
func (s *HookManagerTestSuite) TestPreCreateHooks() {
	s.manager.RegisterHooks(s.hook)                            // Register a working hook
	assert.Nil(s.T(), s.manager.RunPreCreateHooks(nil, nil))   // Ensure it doesn't throw an error on running
	s.manager.RegisterHooks(s.errorHook)                       // Register a failing hook
	assert.Error(s.T(), s.manager.RunPreCreateHooks(nil, nil)) // Ensure it throws an error on running
}

// Check that run fuction returns nil if hook executed well, or an error if not
func (s *HookManagerTestSuite) TestPreRunHooks() {
	s.manager.RegisterHooks(s.hook)                             // Register a working hook
	assert.Nil(s.T(), s.manager.RunPreRunHooks(nil, nil, ""))   // Ensure it doesn't throw an error on running
	s.manager.RegisterHooks(s.errorHook)                        // Register a failing hook
	assert.Error(s.T(), s.manager.RunPreRunHooks(nil, nil, "")) // Ensure it throws an error on running
}

// Check that run fuction returns nil if hook executed well, or an error if not
func (s *HookManagerTestSuite) TestPostRunHooks() {
	s.manager.RegisterHooks(s.hook)                              // Register a working hook
	assert.Nil(s.T(), s.manager.RunPostRunHooks(nil, nil, ""))   // Ensure it doesn't throw an error on running
	s.manager.RegisterHooks(s.errorHook)                         // Register a failing hook
	assert.Error(s.T(), s.manager.RunPostRunHooks(nil, nil, "")) // Ensure it throws an error on running
}

// Check that run fuction returns nil if hook executed well, or an error if not
func (s *HookManagerTestSuite) TestPreStopHooks() {
	s.manager.RegisterHooks(s.hook)                            // Register a working hook
	assert.Nil(s.T(), s.manager.RunPreStopHooks(nil, nil, "")) // Ensure it doesn't throw an error on running
	s.manager.RegisterHooks(s.errorHook)                       // Register a failing hook
	assert.Nil(s.T(), s.manager.RunPreStopHooks(nil, nil, "")) // Ensure it doesn't throw an error on running
}

// Check that run fuction returns nil if hook executed well, or an error if not
func (s *HookManagerTestSuite) TestPostStopHooks() {
	s.manager.RegisterHooks(s.hook)                             // Register a working hook
	assert.Nil(s.T(), s.manager.RunPostStopHooks(nil, nil, "")) // Ensure it doesn't throw an error on running
	s.manager.RegisterHooks(s.errorHook)                        // Register a failing hook
	assert.Nil(s.T(), s.manager.RunPostStopHooks(nil, nil, "")) // Ensure it doesn't throw an error on running
}

func TestHookManagerSuite(t *testing.T) {
	suite.Run(t, new(HookManagerTestSuite))
}
