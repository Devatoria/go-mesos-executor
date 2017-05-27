package hook

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type HookManagerTestSuite struct {
	suite.Suite
	manager *Manager
}

func (s *HookManagerTestSuite) SetupTest() {
	s.manager = NewManager([]string{"sampleHook"})
}

// Check that given enabled hooks slice is well converted to map
func (s *HookManagerTestSuite) TestNewManager() {
	_, ok := s.manager.EnabledHooks["sampleHook"]
	assert.True(s.T(), ok)
}

func TestHookManagerSuite(t *testing.T) {
	suite.Run(t, new(HookManagerTestSuite))
}
