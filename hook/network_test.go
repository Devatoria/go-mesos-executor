package hook

import (
	"testing"

	"github.com/Devatoria/go-mesos-executor/types"

	"github.com/mesos/mesos-go/api/v1/lib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type NetworkTestSuite struct {
	suite.Suite
	c             *types.FakeContainerizer
	hook          Hook
	taskInfo      *mesos.TaskInfo
	frameworkInfo *mesos.FrameworkInfo
}

func (s *NetworkTestSuite) SetupTest() {
	s.hook = NetworkHook
	s.c = types.NewFakeContainerizer() // Generate fake containerizer

	s.frameworkInfo = &mesos.FrameworkInfo{
		Name: "myFrameworkName",
	}

}

// Check that :
// - hook changes network to user mode and set the network name with the framework name
func (s *NetworkTestSuite) TestNetworkExecute() {
	assert.Nil(s.T(), s.hook.RunPreCreate(s.c, s.taskInfo, s.frameworkInfo))

	assert.Equal(s.T(), s.taskInfo.GetContainer().GetDocker().GetNetwork(), mesos.ContainerInfo_DockerInfo_USER)
	assert.Equal(s.T(), s.taskInfo.GetContainer().GetNetworkInfos()[0], s.frameworkInfo.GetName())
}

func TestNetworkSuite(t *testing.T) {
	suite.Run(t, new(NetnsHookTestSuite))
}
