package hook

import (
	"fmt"
	"os"
	"testing"

	"github.com/Devatoria/go-mesos-executor/types"

	"github.com/bouk/monkey"
	"github.com/mesos/mesos-go/api/v1/lib"
	uuid "github.com/satori/go.uuid"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type NetnsHookTestSuite struct {
	suite.Suite
	c        *types.FakeContainerizer
	hook     Hook
	root     string
	taskInfo *mesos.TaskInfo
}

func (s *NetnsHookTestSuite) SetupTest() {
	s.hook = NetnsHook
	s.c = types.NewFakeContainerizer()                                         // Generate fake containerizer
	s.root = fmt.Sprintf("/tmp/executor-netns-test-%s", uuid.NewV4().String()) // Fake root folder
	s.taskInfo = &mesos.TaskInfo{                                              // Task info
		TaskID: mesos.TaskID{
			Value: "fakeTaskID",
		},
	}

	assert.Nil(s.T(), os.Mkdir(s.root, 0755)) // Create fake root
}

func (s *NetnsHookTestSuite) TearDownTest() {
	assert.Nil(s.T(), os.Remove(s.root)) // Remove fake root (should be empty)
}

func (s *NetnsHookTestSuite) TestNetnsHookExecute() {
	defer monkey.UnpatchAll()

	assert.Error(s.T(), s.hook.RunPostRun(s.c, s.taskInfo, "fakeContainerID")) // Should return an error (unexisting root path)

	// Patch viper in order to return a fake netns path
	var guard *monkey.PatchGuard
	guard = monkey.Patch(viper.GetString, func(k string) string {
		guard.Unpatch()
		defer guard.Restore()

		switch k {
		case "netns.path", "proc_path":
			return fmt.Sprintf("%s%s", s.root, viper.GetString(k))
		default:
			return viper.GetString(k)
		}
	})

	assert.Nil(s.T(), s.hook.RunPostRun(s.c, s.taskInfo, "fakeContainerID"))                                       // Should return nil
	assert.True(s.T(), exists(fmt.Sprintf("%s/%s", viper.GetString("netns.path"), s.taskInfo.TaskID.GetValue())))  // Should have created the netns folder and link
	assert.Nil(s.T(), s.hook.RunPostStop(s.c, s.taskInfo, "fakeContainerID"))                                      // Should return nil
	assert.False(s.T(), exists(fmt.Sprintf("%s/%s", viper.GetString("netns.path"), s.taskInfo.TaskID.GetValue()))) // Should have removed the task symlink
	assert.Error(s.T(), s.hook.RunPostStop(s.c, s.taskInfo, "fakeContainerID"))                                    // Should return an error (unexisting link)
}

func TestNetnsHookSuite(t *testing.T) {
	suite.Run(t, new(NetnsHookTestSuite))
}

// Returns true if the given path (dir, file or symlink) exists,
// false otherwise
func exists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}

	_, err = os.Lstat(path)

	return err == nil
}
