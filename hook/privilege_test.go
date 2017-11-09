package hook

import (
	"testing"

	"github.com/Devatoria/go-mesos-executor/types"

	"github.com/mesos/mesos-go/api/v1/lib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type PrivilegeHookTestSuite struct {
	suite.Suite
	c        *types.FakeContainerizer
	hook     Hook
	taskInfo *mesos.TaskInfo
}

func (s *PrivilegeHookTestSuite) SetupTest() {
	s.hook = PrivilegeHook
	s.c = types.NewFakeContainerizer()
}

func (s *PrivilegeHookTestSuite) TestPrivilegeHookExecuteModeTrue() {
	privilegeMode := true
	s.taskInfo = &mesos.TaskInfo{                                              // Task info
		Container: &mesos.ContainerInfo{
			Docker: &mesos.ContainerInfo_DockerInfo {
				Privileged: &privilegeMode,
			},
		},
	}

	assert.Error(s.T(), s.hook.RunPreCreate(s.c, s.taskInfo)) // Should return an error (privilege mode unauthorized)
}

func (s *PrivilegeHookTestSuite) TestPrivilegeHookModeFalse() {
	privilegeMode := false
	s.taskInfo = &mesos.TaskInfo{                                              // Task info
		Container: &mesos.ContainerInfo{
			Docker: &mesos.ContainerInfo_DockerInfo {
				Privileged: &privilegeMode,
			},
		},
	}

	assert.Nil(s.T(), s.hook.RunPreCreate(s.c, s.taskInfo)) // Should return nil (it works)
}

func TestPrivilegeHookSuite(t *testing.T) {
	suite.Run(t, new(PrivilegeHookTestSuite))
}
