package hook

import (
	"reflect"
	"runtime"
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
	c                 *types.FakeContainerizer
	hook              Hook
	hostNamespace     netns.NsHandle
	isolatedNamespace netns.NsHandle
	iptablesDriver    *iptables.IPTables
	randomNamespace   netns.NsHandle
}

func (s *AclHookTestSuite) SetupTest() {
	s.c = types.NewFakeContainerizer() // Generate fake containerizer
	s.hook = ACLHook                   // Retrieve hook

	hostNs, _ := netns.Get() // Get current namespace
	s.hostNamespace = hostNs

	ns, _ := netns.New() // Create a new namespace
	s.randomNamespace = ns

	isolated, _ := netns.New() // Create an isolated namespace for the tests
	s.isolatedNamespace = isolated

	netns.Set(s.isolatedNamespace) // Ensure we are in the isolated namespace

	// Patch GetFromPath and Get functions to return respectively the newly created
	// namespace and the host namespace.
	monkey.Patch(netns.GetFromPath, func(path string) (netns.NsHandle, error) {
		return s.randomNamespace, nil
	})
	monkey.Patch(netns.Get, func() (netns.NsHandle, error) {
		return s.isolatedNamespace, nil
	})
	monkey.PatchInstanceMethod(reflect.TypeOf(&ns), "Close", func(_ *netns.NsHandle) error {
		return nil
	})

	driver, _ := iptables.New() // Get iptables driver
	s.iptablesDriver = driver
}

func (s *AclHookTestSuite) TearDownTest() {
	// Unpatch method after each test to allow the SetupTest to re-run as expected
	monkey.UnpatchAll()

	// Return to the real host namespace
	// and close fd
	netns.Set(s.hostNamespace)
	s.randomNamespace.Close()
	s.isolatedNamespace.Close()
	s.hostNamespace.Close()

	runtime.UnlockOSThread() // Unlock thread now that we've finished
}

// Check that:
// - nothing is done if the label is absent from info
// - nothing is done if the label is present but value is empty
// - an error is thrown if network is not set to bridged
// - an error is thrown if an IP is incorrect
// - an error if thrown if network namespace can't be retrieved
// - all rules are well injected into network namespace
func (s *AclHookTestSuite) TestACLHookExecute() {
	// NOTE: please note that this lock must be done after EVERY
	// call to hook run because hook unlock it after rule insertion!
	runtime.LockOSThread()

	// Injection should not be executed if the network is not in bridge mode
	info := &mesos.TaskInfo{}
	assert.Nil(s.T(), s.hook.RunPostRun(s.c, info, ""))
	runtime.LockOSThread()
	netns.Set(s.randomNamespace)
	rules, _ := s.iptablesDriver.List("filter", "INPUT")
	assert.Equal(s.T(), []string{
		"-P INPUT ACCEPT", // Default policy
	}, rules)
	netns.Set(s.isolatedNamespace)

	// Injection should be skipped if label is not present
	info.Container = &mesos.ContainerInfo{
		Docker: &mesos.ContainerInfo_DockerInfo{
			Network: mesos.ContainerInfo_DockerInfo_BRIDGE.Enum(),
		},
	}
	assert.Nil(s.T(), s.hook.RunPostRun(s.c, info, ""))
	runtime.LockOSThread()
	netns.Set(s.randomNamespace)
	rules, _ = s.iptablesDriver.List("filter", "INPUT")
	assert.Equal(s.T(), []string{
		"-P INPUT ACCEPT",                                         // Default policy
		"-A INPUT -i lo -j ACCEPT",                                // Default lo allow rule
		"-A INPUT -m state --state RELATED,ESTABLISHED -j ACCEPT", // Default related/established traffic allow rule
		"-A INPUT -j DROP",                                        // Default DROP rule
	}, rules)
	assert.Nil(s.T(), s.iptablesDriver.ClearChain("filter", "INPUT"))
	netns.Set(s.isolatedNamespace)

	// Injection should be skipped if label has no value
	info.Labels = &mesos.Labels{
		Labels: []mesos.Label{
			mesos.Label{
				Key: aclHookLabel,
			},
		},
	}
	assert.Nil(s.T(), s.hook.RunPostRun(s.c, info, ""))
	runtime.LockOSThread()
	netns.Set(s.randomNamespace)
	rules, _ = s.iptablesDriver.List("filter", "INPUT")
	assert.Equal(s.T(), []string{
		"-P INPUT ACCEPT",                                         // Default policy
		"-A INPUT -i lo -j ACCEPT",                                // Default lo allow rule
		"-A INPUT -m state --state RELATED,ESTABLISHED -j ACCEPT", // Default related/established traffic allow rule
		"-A INPUT -j DROP",                                        // Default DROP rule
	}, rules)
	assert.Nil(s.T(), s.iptablesDriver.ClearChain("filter", "INPUT"))
	netns.Set(s.isolatedNamespace)

	// Injection should return an error if one of the given IP is invalid
	labelValue := "8.8.8.8,invalidIP"
	info.Labels.Labels[0].Value = &labelValue
	assert.Error(s.T(), s.hook.RunPostRun(s.c, info, ""))
	runtime.LockOSThread()
	netns.Set(s.randomNamespace)
	rules, _ = s.iptablesDriver.List("filter", "INPUT")
	assert.Equal(s.T(), []string{
		"-P INPUT ACCEPT", // Default policy
	}, rules)
	netns.Set(s.isolatedNamespace)

	// Injection should be okay, but no rules should have been
	// added because no ports have been defined
	labelValue = "8.8.8.8,10.0.0.0/24"
	assert.Nil(s.T(), s.hook.RunPostRun(s.c, info, ""))
	runtime.LockOSThread()
	netns.Set(s.randomNamespace)
	rules, _ = s.iptablesDriver.List("filter", "INPUT")
	assert.Equal(s.T(), []string{
		"-P INPUT ACCEPT",                                         // Default policy
		"-A INPUT -i lo -j ACCEPT",                                // Default lo allow rule
		"-A INPUT -m state --state RELATED,ESTABLISHED -j ACCEPT", // Default related/established traffic allow rule
		"-A INPUT -j DROP",                                        // Default DROP rule
	}, rules)
	assert.Nil(s.T(), s.iptablesDriver.ClearChain("filter", "INPUT"))
	netns.Set(s.isolatedNamespace)

	// Injection should be okay
	protocol := "tcp"
	info.Container.Docker.PortMappings = []mesos.ContainerInfo_DockerInfo_PortMapping{
		mesos.ContainerInfo_DockerInfo_PortMapping{
			ContainerPort: 80,
			Protocol:      &protocol,
		},
	}
	assert.Nil(s.T(), s.iptablesDriver.ClearChain("filter", "INPUT"))
	assert.Nil(s.T(), s.hook.RunPostRun(s.c, info, ""))
	runtime.LockOSThread()
	netns.Set(s.randomNamespace)
	rules, _ = s.iptablesDriver.List("filter", "INPUT")
	assert.Equal(s.T(), []string{
		"-P INPUT ACCEPT",                                            // Default policy
		"-A INPUT -s 8.8.8.8/32 -p tcp -m tcp --dport 80 -j ACCEPT",  // First user-defined rule
		"-A INPUT -s 10.0.0.0/24 -p tcp -m tcp --dport 80 -j ACCEPT", // Second user-defined rule
		"-A INPUT -i lo -j ACCEPT",                                   // Default lo allow rule
		"-A INPUT -m state --state RELATED,ESTABLISHED -j ACCEPT",    // Default related/established traffic allow rule
		"-A INPUT -j DROP",                                           // Default DROP rule
	}, rules)
	netns.Set(s.isolatedNamespace)
}

// Check that:
// - an error it thrown if current and container namespace are the same
// - given rules are well injected into given namespace
func (s *AclHookTestSuite) TestInjectRuleIntoNamespace() {
	assert.Error(s.T(), injectRuleIntoNamespace(s.isolatedNamespace, ""))                  // Should throw an error (same unique ID)
	assert.Error(s.T(), injectRuleIntoNamespace(s.randomNamespace, ""))                    // Try to inject a bad rule
	assert.Nil(s.T(), injectRuleIntoNamespace(s.randomNamespace, "-s 10.0.0.1 -j ACCEPT")) // Try to inject a good rule

	runtime.LockOSThread()                               // Lock thread to avoid namespace switching while testing
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
