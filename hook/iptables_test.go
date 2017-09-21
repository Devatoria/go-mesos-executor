package hook

import (
	"net"
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

type IptablesTestSuite struct {
	suite.Suite
	c                  *types.FakeContainerizer
	insertIptablesHook Hook
	removeIptablesHook Hook
	hostNamespace      netns.NsHandle
	isolatedNamespace  netns.NsHandle
	iptablesDriver     *iptables.IPTables
	containerInterface string
	taskInfo           *types.ContainerTaskInfo
	containerIPs       map[string]net.IP
}

func (s *IptablesTestSuite) SetupTest() {
	runtime.LockOSThread() // Lock thread to avoid namespace switching while testing

	s.c = types.NewFakeContainerizer()        // Generate fake containerizer
	s.insertIptablesHook = InsertIptablesHook // Retrieve InsertIptableHook
	s.removeIptablesHook = RemoveIptablesHook // Retrieve InsertIptableHook
	s.containerInterface = "docker0"
	tcpProtocol := "tcp"
	udpProtocol := "udp"
	s.taskInfo = &types.ContainerTaskInfo{
		TaskInfo: mesos.TaskInfo{
			Container: &mesos.ContainerInfo{
				Docker: &mesos.ContainerInfo_DockerInfo{
					Network: mesos.ContainerInfo_DockerInfo_BRIDGE.Enum(),
					PortMappings: []mesos.ContainerInfo_DockerInfo_PortMapping{
						mesos.ContainerInfo_DockerInfo_PortMapping{
							HostPort:      32000,
							ContainerPort: 80,
							Protocol:      &tcpProtocol,
						},
						mesos.ContainerInfo_DockerInfo_PortMapping{
							HostPort:      33000,
							ContainerPort: 10000,
							Protocol:      &udpProtocol,
						},
					},
				},
			},
		},
	}

	s.containerIPs = map[string]net.IP{
		"bridge":       net.ParseIP("172.0.2.1"),
		"user_network": net.ParseIP("172.0.3.1"),
	}
	hostNs, _ := netns.Get() // Get current namespace
	s.hostNamespace = hostNs

	isolated, _ := netns.New() // Create an isolated namespace for the tests
	s.isolatedNamespace = isolated

	netns.Set(s.isolatedNamespace) // Ensure we are in the isolated namespace

	monkey.PatchInstanceMethod(reflect.TypeOf(&isolated), "Close", func(_ *netns.NsHandle) error {
		return nil
	})

	monkey.PatchInstanceMethod(
		reflect.TypeOf(s.c),
		"ContainerGetIPs",
		func(_ *types.FakeContainerizer, id string) (map[string]net.IP, error) {
			return s.containerIPs, nil
		},
	)

	monkey.Patch(viper.GetString, func(string) string {
		return s.containerInterface
	})

	driver, _ := iptables.New() // Get iptables driver
	s.iptablesDriver = driver
}

func (s *IptablesTestSuite) TearDownTest() {
	// Unpatch method after each test to allow the SetupTest to re-run as expected
	monkey.UnpatchAll()

	// Return to the real host namespace
	// and close fd
	netns.Set(s.hostNamespace)
	s.isolatedNamespace.Close()
	s.hostNamespace.Close()

	runtime.UnlockOSThread() // Unlock thread now that we've finished
}

// Check that :
// - hook does nothing if container is in host mode
// - iptables are correctly injected on at hook execution
func (s *IptablesTestSuite) TestInsertIptablesHookExecute() {
	// Store the state of the namespace network
	BaseForwardRule, _ := s.iptablesDriver.List("filter", "FORWARD")
	BasePostRoutingRules, _ := s.iptablesDriver.List("nat", "POSTROUTING")
	BasePreRoutingRules, _ := s.iptablesDriver.List("nat", "PREROUTING")

	// Injection should not be executed if the network is not in bridge mode
	info := &types.ContainerTaskInfo{}
	assert.Nil(s.T(), s.insertIptablesHook.Execute(s.c, info))

	forwardRules, _ := s.iptablesDriver.List("filter", "FORWARD")
	postRoutingRules, _ := s.iptablesDriver.List("nat", "POSTROUTING")
	preRoutingRules, _ := s.iptablesDriver.List("nat", "PREROUTING")

	assert.Equal(
		s.T(),
		BaseForwardRule,
		forwardRules,
	)

	assert.Equal(
		s.T(),
		BasePostRoutingRules,
		postRoutingRules,
	)

	assert.Equal(
		s.T(),
		BasePreRoutingRules,
		preRoutingRules,
	)

	// Now test for each table and chain that rule are correctly inserted,
	// next to the previous network states
	assert.Nil(s.T(), s.insertIptablesHook.Execute(s.c, s.taskInfo))
	forwardRules, _ = s.iptablesDriver.List("filter", "FORWARD")
	assert.Subset(
		s.T(),
		append(
			BaseForwardRule,
			[]string{
				"-A FORWARD -d 172.0.2.1/32 -i !docker0 -o docker0 -p tcp -m tcp --dport 80 -j ACCEPT",
				"-A FORWARD -d 172.0.2.1/32 -i !docker0 -o docker0 -p udp -m udp --dport 10000 -j ACCEPT",
				"-A FORWARD -d 172.0.3.1/32 -i !docker0 -o docker0 -p tcp -m tcp --dport 80 -j ACCEPT",
				"-A FORWARD -d 172.0.3.1/32 -i !docker0 -o docker0 -p udp -m udp --dport 10000 -j ACCEPT",
			}...,
		),
		forwardRules,
	)
	postRoutingRules, _ = s.iptablesDriver.List("nat", "POSTROUTING")
	assert.Subset(
		s.T(),
		append(
			BasePostRoutingRules,
			[]string{
				"-A POSTROUTING -s 172.0.2.1/32 -o !docker0 -j MASQUERADE",
				"-A POSTROUTING -s 172.0.2.1/32 -d 172.0.2.1/32 -p tcp -m tcp --dport 80 -j MASQUERADE",
				"-A POSTROUTING -s 172.0.2.1/32 -d 172.0.2.1/32 -p udp -m udp --dport 10000 -j MASQUERADE",
				"-A POSTROUTING -s 172.0.3.1/32 -o !docker0 -j MASQUERADE",
				"-A POSTROUTING -s 172.0.3.1/32 -d 172.0.3.1/32 -p tcp -m tcp --dport 80 -j MASQUERADE",
				"-A POSTROUTING -s 172.0.3.1/32 -d 172.0.3.1/32 -p udp -m udp --dport 10000 -j MASQUERADE",
			}...,
		),
		postRoutingRules,
	)

	preRoutingRules, _ = s.iptablesDriver.List("nat", "PREROUTING")
	assert.Subset(
		s.T(),
		append(
			BasePreRoutingRules,
			[]string{
				"-A PREROUTING -i !docker0 -p tcp -m tcp --dport 32000 -j DNAT --to-destination 172.0.2.1:80",
				"-A PREROUTING -i !docker0 -p udp -m udp --dport 33000 -j DNAT --to-destination 172.0.2.1:10000",
				"-A PREROUTING -i !docker0 -p tcp -m tcp --dport 32000 -j DNAT --to-destination 172.0.3.1:80",
				"-A PREROUTING -i !docker0 -p udp -m udp --dport 33000 -j DNAT --to-destination 172.0.3.1:10000",
			}...,
		),
		preRoutingRules,
	)
}

// Check that :
// - hook does nothing when container is in host network
// - iptables are correctly removed at task hook execution
func (s *IptablesTestSuite) TestRemoveIptablesHookExecute() {
	// Store the state of the namespace network
	referenceForwardRule, _ := s.iptablesDriver.List("filter", "FORWARD")
	referencePostRoutingRules, _ := s.iptablesDriver.List("nat", "POSTROUTING")
	referencePreRoutingRules, _ := s.iptablesDriver.List("nat", "PREROUTING")

	// Removing should not be executed if the network is not in bridge mode
	info := &types.ContainerTaskInfo{}
	assert.Nil(s.T(), s.removeIptablesHook.Execute(s.c, info))

	// Execute insert iptables hook to insert iptables
	s.insertIptablesHook.Execute(s.c, s.taskInfo)

	// Execute remove iptables hook to remove inserted iptables
	assert.Nil(s.T(), s.removeIptablesHook.Execute(s.c, s.taskInfo))
	forwardRules, _ := s.iptablesDriver.List("filter", "FORWARD")
	postRoutingRules, _ := s.iptablesDriver.List("nat", "POSTROUTING")
	preRoutingRules, _ := s.iptablesDriver.List("nat", "PREROUTING")

	assert.Equal(
		s.T(),
		referenceForwardRule,
		forwardRules,
	)

	assert.Equal(
		s.T(),
		referencePostRoutingRules,
		postRoutingRules,
	)

	assert.Equal(
		s.T(),
		referencePreRoutingRules,
		preRoutingRules,
	)
}

func TestIptablesHookSuite(t *testing.T) {
	suite.Run(t, new(IptablesTestSuite))
}
