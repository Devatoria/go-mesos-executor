package hook

import (
	"testing"

	"github.com/Devatoria/go-mesos-executor/types"

	"github.com/bouk/monkey"
	"github.com/coreos/go-iptables/iptables"
	"github.com/mesos/mesos-go/api/v1/lib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"github.com/vishvananda/netns"
)

type AclHookTestSuite struct {
	suite.Suite
	c               *types.FakeContainerizer
	hook            Hook
	hostNamespace   netns.NsHandle
	iptablesDriver  *iptables.IPTables
	randomNamespace netns.NsHandle
}

func (s *AclHookTestSuite) SetupTest() {
	s.c = types.NewFakeContainerizer() // Generate fake containerizer
	s.hook = ACLHook                   // Retrieve hook

	hostNs, _ := netns.Get() // Get current namespace
	s.hostNamespace = hostNs

	ns, _ := netns.New() // Create a new namespace
	s.randomNamespace = ns

	netns.Set(s.hostNamespace) // Ensure we are in the host namespace

	// Patch GetFromPath and Get functions to return respectively the newly created
	// namespace and the host namespace.
	monkey.Patch(netns.GetFromPath, func(path string) (netns.NsHandle, error) {
		return s.randomNamespace, nil
	})
	monkey.Patch(netns.Get, func() (netns.NsHandle, error) {
		return s.hostNamespace, nil
	})

	driver, _ := iptables.New() // Get iptables driver
	s.iptablesDriver = driver
}

func (s *AclHookTestSuite) TearDownTest() {
	// Unpatch method after each test to allow the SetupTest to re-run as expected
	monkey.Unpatch(netns.GetFromPath)
	monkey.Unpatch(netns.Get)
}

// Check that:
// - nothing is done if the label is absent from info
// - nothing is done if the label is present but value is empty
// - an error is thrown if network is not set to bridged
// - an error is thrown if an IP is incorrect
// - an error if thrown if network namespace can't be retrieved
// - all rules are well injected into network namespace
func (s *AclHookTestSuite) TestACLHookExecute() {
	// Injection should be skipped if label is not present
	info := &types.ContainerTaskInfo{}
	assert.Nil(s.T(), s.hook.Execute(s.c, info))

	// Injection should be skipped if label has no value
	info.TaskInfo.Labels = &mesos.Labels{
		Labels: []mesos.Label{
			mesos.Label{
				Key: aclHookLabel,
			},
		},
	}
	assert.Nil(s.T(), s.hook.Execute(s.c, info))

	// Injection should return an error if the network is not set to bridged
	labelValue := "8.8.8.8"
	info.TaskInfo.Labels.Labels[0].Value = &labelValue
	assert.Error(s.T(), s.hook.Execute(s.c, info))

	// Injection should return an error if one of the given IP is invalid
	labelValue = "8.8.8.8,invalidIP"
	info.TaskInfo.Container = &mesos.ContainerInfo{
		Docker: &mesos.ContainerInfo_DockerInfo{
			Network: mesos.ContainerInfo_DockerInfo_BRIDGE.Enum(),
		},
	}
	assert.Error(s.T(), s.hook.Execute(s.c, info))

	// Injection should be okay
	labelValue = "8.8.8.8,10.0.0.0/24"
	assert.Nil(s.T(), s.hook.Execute(s.c, info))

	// Check that injected rules are okay
	netns.Set(s.randomNamespace)
	rules, _ := s.iptablesDriver.List("filter", "INPUT")
	assert.Equal(s.T(), []string{
		"-P INPUT ACCEPT",                    // Default policy
		"-A INPUT -s 8.8.8.8/32 -j ACCEPT",   // First injected IP
		"-A INPUT -s 10.0.0.0/24 -j ACCEPT",  // Second injected IP
		"-A INPUT -s 127.0.0.1/32 -j ACCEPT", // Bridge IP
		"-A INPUT -j DROP",                   // Default DROP rule
	}, rules)
}

// Check that:
// - an error it thrown if current and container namespace are the same
// - given rules are well injected into given namespace
func (s *AclHookTestSuite) TestInjectRuleIntoNamespace() {
	assert.Error(s.T(), injectRuleIntoNamespace(s.hostNamespace, ""))                      // Should throw an error (same unique ID)
	assert.Error(s.T(), injectRuleIntoNamespace(s.randomNamespace, ""))                    // Try to inject a bad rule
	assert.Nil(s.T(), injectRuleIntoNamespace(s.randomNamespace, "-s 10.0.0.1 -j ACCEPT")) // Try to inject a good rule

	netns.Set(s.randomNamespace)                         // Enter random namespace
	rules, _ := s.iptablesDriver.List("filter", "INPUT") // Retrieve chain rules
	assert.Equal(s.T(), []string{
		"-P INPUT ACCEPT",
		"-A INPUT -s 10.0.0.1/32 -j ACCEPT",
	}, rules) // Ensure rules have been well inserted
}

func TestAclHookSuite(t *testing.T) {
	suite.Run(t, new(AclHookTestSuite))
}
