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
	hook               Hook
	hostNamespace      netns.NsHandle
	isolatedNamespace  netns.NsHandle
	iptablesDriver     *iptables.IPTables
	containerInterface string
	taskInfo           *mesos.TaskInfo
	containerIPs       map[string]net.IP
	preroutingChain    string
	forwardChain       string
	postroutingChain   string
}

func (s *IptablesTestSuite) SetupTest() {
	runtime.LockOSThread() // Lock thread to avoid namespace switching while testing

	s.c = types.NewFakeContainerizer() // Generate fake containerizer
	s.hook = IptablesHook
	s.containerInterface = "docker0"
	s.preroutingChain = "EXECUTOR-PREROUTING"
	s.forwardChain = "EXECUTOR-FORWARD"
	s.postroutingChain = "EXECUTOR-POSTROUTING"
	tcpProtocol := "tcp"
	udpProtocol := "udp"
	s.taskInfo = &mesos.TaskInfo{
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

	var gGetString *monkey.PatchGuard
	gGetString = monkey.Patch(viper.GetString, func(k string) string {
		gGetString.Unpatch()
		defer gGetString.Restore()

		switch k {
		case "iptables.container_bridge_interface":
			return s.containerInterface
		case "iptables.chains.prerouting":
			return s.preroutingChain
		case "iptables.chains.forward":
			return s.forwardChain
		case "iptables.chains.postrouting":
			return s.postroutingChain
		default:
			return viper.GetString(k)
		}
	})

	var gGetBool *monkey.PatchGuard
	gGetBool = monkey.Patch(viper.GetBool, func(k string) bool {
		gGetBool.Unpatch()
		defer gGetBool.Restore()

		switch k {
		case "iptables.ip_forwarding", "iptables.ip_masquerading":
			return true
		default:
			return viper.GetBool(k)
		}
	})

	driver, _ := iptables.New() // Get iptables driver
	s.iptablesDriver = driver

	driver.NewChain("nat", s.preroutingChain)
	driver.NewChain("filter", s.forwardChain)
	driver.NewChain("nat", s.postroutingChain)
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
func (s *IptablesTestSuite) TestIptablesHookRunPostRun() {
	// Store the state of the namespace network
	BaseForwardRule, _ := s.iptablesDriver.List("filter", s.forwardChain)
	BasePostRoutingRules, _ := s.iptablesDriver.List("nat", s.postroutingChain)
	BasePreRoutingRules, _ := s.iptablesDriver.List("nat", s.preroutingChain)

	// Injection should not be executed if the network is not in bridge mode
	info := &mesos.TaskInfo{}
	assert.Nil(s.T(), s.hook.RunPostRun(s.c, info, ""))

	forwardRules, _ := s.iptablesDriver.List("filter", s.forwardChain)
	postRoutingRules, _ := s.iptablesDriver.List("nat", s.postroutingChain)
	preRoutingRules, _ := s.iptablesDriver.List("nat", s.preroutingChain)

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
	assert.Nil(s.T(), s.hook.RunPostRun(s.c, s.taskInfo, ""))
	forwardRules, _ = s.iptablesDriver.List("filter", s.forwardChain)
	assert.Subset(
		s.T(),
		forwardRules,
		append(
			BaseForwardRule,
			[]string{
				"-A " + s.forwardChain + " -d 172.0.2.1/32 ! -i docker0 -o docker0 -p tcp -m tcp --dport 80 -j ACCEPT",
				"-A " + s.forwardChain + " -s 172.0.2.1/32 -i docker0 ! -o docker0 -p tcp -m tcp --sport 80 -j ACCEPT",
				"-A " + s.forwardChain + " -d 172.0.2.1/32 ! -i docker0 -o docker0 -p udp -m udp --dport 10000 -j ACCEPT",
				"-A " + s.forwardChain + " -s 172.0.2.1/32 -i docker0 ! -o docker0 -p udp -m udp --sport 10000 -j ACCEPT",
				"-A " + s.forwardChain + " -d 172.0.3.1/32 ! -i docker0 -o docker0 -p tcp -m tcp --dport 80 -j ACCEPT",
				"-A " + s.forwardChain + " -s 172.0.3.1/32 -i docker0 ! -o docker0 -p tcp -m tcp --sport 80 -j ACCEPT",
				"-A " + s.forwardChain + " -d 172.0.3.1/32 ! -i docker0 -o docker0 -p udp -m udp --dport 10000 -j ACCEPT",
				"-A " + s.forwardChain + " -s 172.0.3.1/32 -i docker0 ! -o docker0 -p udp -m udp --sport 10000 -j ACCEPT",
			}...,
		),
	)
	postRoutingRules, _ = s.iptablesDriver.List("nat", s.postroutingChain)
	assert.Subset(
		s.T(),
		postRoutingRules,
		append(
			BasePostRoutingRules,
			[]string{
				"-A " + s.postroutingChain + " -s 172.0.2.1/32 ! -o docker0 -j MASQUERADE",
				"-A " + s.postroutingChain + " -s 172.0.2.1/32 -d 172.0.2.1/32 -p tcp -m tcp --dport 80 -j MASQUERADE",
				"-A " + s.postroutingChain + " -s 172.0.2.1/32 -d 172.0.2.1/32 -p udp -m udp --dport 10000 -j MASQUERADE",
				"-A " + s.postroutingChain + " -s 172.0.3.1/32 ! -o docker0 -j MASQUERADE",
				"-A " + s.postroutingChain + " -s 172.0.3.1/32 -d 172.0.3.1/32 -p tcp -m tcp --dport 80 -j MASQUERADE",
				"-A " + s.postroutingChain + " -s 172.0.3.1/32 -d 172.0.3.1/32 -p udp -m udp --dport 10000 -j MASQUERADE",
			}...,
		),
	)

	preRoutingRules, _ = s.iptablesDriver.List("nat", s.preroutingChain)
	assert.Subset(
		s.T(),
		preRoutingRules,
		append(
			BasePreRoutingRules,
			[]string{
				"-A " + s.preroutingChain + " ! -i docker0 -p tcp -m tcp --dport 32000 -j DNAT --to-destination 172.0.2.1:80",
				"-A " + s.preroutingChain + " ! -i docker0 -p udp -m udp --dport 33000 -j DNAT --to-destination 172.0.2.1:10000",
				"-A " + s.preroutingChain + " ! -i docker0 -p tcp -m tcp --dport 32000 -j DNAT --to-destination 172.0.3.1:80",
				"-A " + s.preroutingChain + " ! -i docker0 -p udp -m udp --dport 33000 -j DNAT --to-destination 172.0.3.1:10000",
			}...,
		),
	)
}

// Check that :
// - hook does nothing when container is in host network
// - iptables are correctly removed at task hook execution
func (s *IptablesTestSuite) TestIptablesHookRunPreStop() {
	// Store the state of the namespace network
	referenceForwardRule, _ := s.iptablesDriver.List("filter", s.forwardChain)
	referencePostRoutingRules, _ := s.iptablesDriver.List("nat", s.postroutingChain)
	referencePreRoutingRules, _ := s.iptablesDriver.List("nat", s.preroutingChain)

	// Removing should not be executed if the network is not in bridge mode
	info := &mesos.TaskInfo{}
	assert.Nil(s.T(), s.hook.RunPreStop(s.c, info, ""))

	// Execute insert iptables hook to insert iptables
	s.hook.RunPostRun(s.c, s.taskInfo, "")

	// Execute remove iptables hook to remove inserted iptables
	assert.Nil(s.T(), s.hook.RunPreStop(s.c, s.taskInfo, ""))
	forwardRules, _ := s.iptablesDriver.List("filter", s.forwardChain)
	postRoutingRules, _ := s.iptablesDriver.List("nat", s.postroutingChain)
	preRoutingRules, _ := s.iptablesDriver.List("nat", s.preroutingChain)

	assert.Equal(
		s.T(),
		forwardRules,
		referenceForwardRule,
	)

	assert.Equal(
		s.T(),
		postRoutingRules,
		referencePostRoutingRules,
	)

	assert.Equal(
		s.T(),
		preRoutingRules,
		referencePreRoutingRules,
	)
}

func TestIptablesHookSuite(t *testing.T) {
	suite.Run(t, new(IptablesTestSuite))
}
