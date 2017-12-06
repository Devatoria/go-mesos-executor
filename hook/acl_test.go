package hook

import (
	"reflect"
	"runtime"
	"testing"

	"github.com/Devatoria/go-mesos-executor/types"
	"github.com/spf13/viper"

	"github.com/bouk/monkey"
	"github.com/coreos/go-iptables/iptables"
	"github.com/mesos/mesos-go/api/v1/lib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"github.com/vishvananda/netns"
)

type ACLHookTestSuite struct {
	suite.Suite
	c                 *types.FakeContainerizer
	frameworkInfo     *mesos.FrameworkInfo
	hook              Hook
	hostNamespace     netns.NsHandle
	isolatedNamespace netns.NsHandle
	iptablesDriver    *iptables.IPTables
	externalInterface string
	aclChain          string
}

func (s *ACLHookTestSuite) SetupTest() {
	runtime.LockOSThread() // Lock thread to avoid namespace switching while testing

	s.c = types.NewFakeContainerizer() // Generate fake containerizer
	s.hook = ACLHook                   // Retrieve hook

	hostNs, _ := netns.Get() // Get current namespace
	s.hostNamespace = hostNs

	isolated, _ := netns.New() // Create an isolated namespace for the tests
	s.isolatedNamespace = isolated

	s.externalInterface = "ext1"
	s.aclChain = "INPUT"

	netns.Set(s.isolatedNamespace) // Ensure we are in the isolated namespace

	monkey.PatchInstanceMethod(reflect.TypeOf(&isolated), "Close", func(_ *netns.NsHandle) error {
		return nil
	})

	var guard *monkey.PatchGuard
	guard = monkey.Patch(viper.GetString, func(arg string) string {
		guard.Unpatch()
		defer guard.Restore()
		switch arg {
		case "acl.external_interface":
			return s.externalInterface
		case "acl.chain":
			return s.aclChain
		default:
			return viper.GetString(arg)
		}
	})

	driver, _ := iptables.New() // Get iptables driver
	s.iptablesDriver = driver
}

func (s *ACLHookTestSuite) TearDownTest() {
	// Unpatch method after each test to allow the SetupTest to re-run as expected
	monkey.UnpatchAll()

	// Return to the real host namespace
	// and close fd
	netns.Set(s.hostNamespace)
	s.isolatedNamespace.Close()
	s.hostNamespace.Close()

	runtime.UnlockOSThread() // Unlock thread now that we've finished
}

// Check that:
// - an error is thrown if network is not set to bridged
// - nothing is done if the label is absent from info
// - an error is thrown if label port index does not match port mapping
// - an error is thrown if label has no value
// - an error is thrown if an IP is incorrect
// - all rules are well injected into network namespace
// - rules are set for all interfaces if not configured
// - an rror is thrown if chain does not exist
func (s *ACLHookTestSuite) TestACLHookRunPostRun() {

	// Store the state of the filter chains
	baseInputRule, _ := s.iptablesDriver.List("filter", "INPUT")
	baseForwardRule, _ := s.iptablesDriver.List("filter", "FORWARD")
	baseOutputRule, _ := s.iptablesDriver.List("filter", "OUTPUT")

	// Injection should not be executed if the network is not in bridge mode
	taskInfo := &mesos.TaskInfo{}
	assert.Nil(s.T(), s.hook.RunPostRun(s.c, taskInfo, s.frameworkInfo, ""))
	rules, _ := s.iptablesDriver.List("filter", "INPUT")
	assert.Subset(s.T(), baseInputRule, rules)

	// Injection should be skipped if label is not present
	taskInfo.Container = &mesos.ContainerInfo{
		Docker: &mesos.ContainerInfo_DockerInfo{
			Network: mesos.ContainerInfo_DockerInfo_BRIDGE.Enum(),
		},
	}
	assert.Nil(s.T(), s.hook.RunPostRun(s.c, taskInfo, s.frameworkInfo, ""))
	rules, _ = s.iptablesDriver.List("filter", "INPUT")
	assert.Subset(s.T(), baseInputRule, rules)

	// Injection should return an error if port index does not match port configuration
	taskInfo.Labels = &mesos.Labels{
		Labels: []mesos.Label{
			mesos.Label{
				Key: "EXECUTOR_0_ACL",
			},
		},
	}
	assert.Error(s.T(), s.hook.RunPostRun(s.c, taskInfo, s.frameworkInfo, ""))
	rules, _ = s.iptablesDriver.List("filter", "INPUT")
	assert.Subset(s.T(), baseInputRule, rules)

	// Injection should return error if label has no value
	protocol := "tcp"
	taskInfo.Container.Docker.PortMappings = []mesos.ContainerInfo_DockerInfo_PortMapping{
		mesos.ContainerInfo_DockerInfo_PortMapping{
			ContainerPort: 80,
			HostPort:      10000,
			Protocol:      &protocol,
		},
	}
	assert.Error(s.T(), s.hook.RunPostRun(s.c, taskInfo, s.frameworkInfo, ""))
	rules, _ = s.iptablesDriver.List("filter", "INPUT")
	assert.Subset(s.T(), baseInputRule, rules)

	// Injection should return an error if one of the given IP is invalid
	labelValue := "8.8.8.8,invalidIP"
	taskInfo.Labels.Labels[0].Value = &labelValue
	assert.Error(s.T(), s.hook.RunPostRun(s.c, taskInfo, s.frameworkInfo, ""))
	rules, _ = s.iptablesDriver.List("filter", "INPUT")
	assert.Subset(s.T(), baseInputRule, rules)

	// Injection should be okay
	labelValue = "8.8.8.8,10.0.0.0/24"
	taskInfo.Labels.Labels[0].Value = &labelValue
	assert.Nil(s.T(), s.hook.RunPostRun(s.c, taskInfo, s.frameworkInfo, ""))
	rules, _ = s.iptablesDriver.List("filter", "INPUT")
	assert.Subset(s.T(),
		append(
			baseInputRule,
			"-A INPUT -s 8.8.8.8/32 -i ext1 -p tcp -m tcp --dport 10000 -j ACCEPT",
			"-A INPUT -s 10.0.0.0/24 -i ext1 -p tcp -m tcp --dport 10000 -j ACCEPT",
		),
		rules,
	)
	assert.Nil(s.T(), s.iptablesDriver.ClearChain("filter", "INPUT"))

	// Acl is done for all interfaces if not specified
	s.externalInterface = ""
	assert.Nil(s.T(), s.hook.RunPostRun(s.c, taskInfo, s.frameworkInfo, ""))
	rules, _ = s.iptablesDriver.List("filter", "INPUT")
	assert.Subset(s.T(),
		append(
			baseInputRule,
			"-A INPUT -s 8.8.8.8/32 -i all -p tcp -m tcp --dport 10000 -j ACCEPT",
			"-A INPUT -s 10.0.0.0/24 -i all -p tcp -m tcp --dport 10000 -j ACCEPT",
		),
		rules,
	)
	s.externalInterface = "ext1"
	assert.Nil(s.T(), s.iptablesDriver.ClearChain("filter", "INPUT"))

	// Error is thrown if chain does not exist
	s.aclChain = "UNKNOWNCHAIN"
	assert.Error(s.T(), s.hook.RunPostRun(s.c, taskInfo, s.frameworkInfo, ""))
	rules, _ = s.iptablesDriver.List("filter", "INPUT")
	assert.Subset(s.T(), baseInputRule, rules)

	// Error is thrown if chain is forward
	s.aclChain = "FORWARD"
	assert.Error(s.T(), s.hook.RunPostRun(s.c, taskInfo, s.frameworkInfo, ""))
	rules, _ = s.iptablesDriver.List("filter", "FORWARD")
	assert.Subset(s.T(), baseForwardRule, rules)

	// Error is thrown if chain is output
	s.aclChain = "OUTPUT"
	assert.Error(s.T(), s.hook.RunPostRun(s.c, taskInfo, s.frameworkInfo, ""))
	rules, _ = s.iptablesDriver.List("filter", "OUTPUT")
	assert.Subset(s.T(), baseOutputRule, rules)
}

// Check that :
// - hook does nothing when container is in host network
// - acls are correctly removed at task hook execution
func (s *ACLHookTestSuite) TestACLHookRunPreStop() {
	// Store the state of the input chain
	referenceInputRule, _ := s.iptablesDriver.List("filter", "INPUT")

	// Removing should not be executed if the network is not in bridge mode
	taskInfo := &mesos.TaskInfo{}
	assert.Nil(s.T(), s.hook.RunPreStop(s.c, taskInfo, s.frameworkInfo, ""))

	// Execute post run acl hook to inject iptables
	protocol := "tcp"
	labelValue := "8.8.8.8,10.0.0.0/24"
	label := mesos.Labels{
		Labels: []mesos.Label{
			mesos.Label{
				Key:   "ACL_0_ALLOWED",
				Value: &labelValue,
			},
		},
	}
	taskInfo = &mesos.TaskInfo{
		Container: &mesos.ContainerInfo{
			Docker: &mesos.ContainerInfo_DockerInfo{
				Network: mesos.ContainerInfo_DockerInfo_BRIDGE.Enum(),
				PortMappings: []mesos.ContainerInfo_DockerInfo_PortMapping{
					mesos.ContainerInfo_DockerInfo_PortMapping{
						HostPort:      10000,
						ContainerPort: 80,
						Protocol:      &protocol,
					},
				},
			},
		},
		Labels: &label,
	}
	s.hook.RunPostRun(s.c, taskInfo, s.frameworkInfo, "")

	// Execute remove acl hook to remove injected iptables
	assert.Nil(s.T(), s.hook.RunPreStop(s.c, taskInfo, s.frameworkInfo, ""))
	inputRules, _ := s.iptablesDriver.List("filter", "INPUT")

	assert.Equal(
		s.T(),
		referenceInputRule,
		inputRules,
	)
}

func TestAclHookSuite(t *testing.T) {
	suite.Run(t, new(ACLHookTestSuite))
}
