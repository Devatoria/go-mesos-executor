package hook

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/Devatoria/go-mesos-executor/container"
	"github.com/Devatoria/go-mesos-executor/logger"

	"github.com/coreos/go-iptables/iptables"
	"github.com/mesos/mesos-go/api/v1/lib"
	"github.com/spf13/viper"
)

const (
	iptableHookDnatRuleTemplate           = "! -i %s -p %s -j DNAT --dport %s --to-destination %s --wait"
	iptableHookMasqueradeRuleTemplate     = "! -o %s -s %s/32 -j MASQUERADE --wait"
	iptableHookSelfMasqueradeRuleTemplate = "-d %s/32 -p %s -s %s/32 -j MASQUERADE --dport %s --wait"
	iptableHookForwardInRuleTemplate      = "-d %s/32 ! -i %s -o %s -p %s -j ACCEPT --dport %s --wait"
	iptableHookForwardOutRuleTemplate     = "-i %s ! -o %s -p %s -s %s/32 -j ACCEPT --sport %s --wait"
)

// containerIpCache is a map containing the containers ips. This map is useful when
// removing iptables when containers is stopped and does not have an IP anymore.
var iptablesHookContainerIPCache = sync.Map{}

// IptablesHook injects iptables rules on host. This iptables allow container masquerading
// and network forwarding to container.
var IptablesHook = Hook{
	Name:     "iptables",
	Priority: 0,
	RunPostRun: func(c container.Containerizer, taskInfo *mesos.TaskInfo, frameworkInfo *mesos.FrameworkInfo, containerID string) error {
		// Do not execute the hook if we are not on bridged network
		if taskInfo.GetContainer().GetDocker().GetNetwork() != mesos.ContainerInfo_DockerInfo_BRIDGE {
			logger.GetInstance().Warn("Insert Iptables hook can't inject iptables rules if network mode is not bridged")

			return nil
		}

		logger.GetInstance().Debug(fmt.Sprintf("Inserting iptables on host namespace for container %s", containerID))

		driver, err := iptables.New()
		if err != nil {
			return err
		}

		portMappings := taskInfo.GetContainer().GetDocker().GetPortMappings()

		// Get container ip
		containerIPs, err := c.ContainerGetIPs(containerID)
		if err != nil {
			return err
		}
		iptablesHookContainerIPCache.Store(containerID, containerIPs)

		return generateIptables(containerIPs, portMappings, driver, driver.Append, true)
	},
	RunPreStop: func(c container.Containerizer, taskInfo *mesos.TaskInfo, frameworkInfo *mesos.FrameworkInfo, containerID string) error {
		// Do not execute the hook if we are not on bridged network
		if taskInfo.GetContainer().GetDocker().GetNetwork() != mesos.ContainerInfo_DockerInfo_BRIDGE {
			logger.GetInstance().Warn("Iptables hook does not need to remove iptables rules if network mode is not bridged")

			return nil
		}

		logger.GetInstance().Debug(fmt.Sprintf("Removing iptables on host namespace for container %s", containerID))

		driver, err := iptables.New()
		if err != nil {
			return err
		}

		portMappings := taskInfo.GetContainer().GetDocker().GetPortMappings()

		// Retrieve container IPs from cache
		ipsCacheValue, ok := iptablesHookContainerIPCache.Load(containerID)
		if !ok {
			return fmt.Errorf(
				"could not find ip in cache for container %s",
				containerID,
			)
		}

		containerIPs, ok := ipsCacheValue.(map[string]net.IP)
		if !ok {
			return fmt.Errorf(
				"could not load ip from cache for container %s",
				containerID,
			)
		}

		return generateIptables(containerIPs, portMappings, driver, driver.Delete, false)
	},
}

// generateIptables generates all needed iptables for containers masquerading/ network forwarding.
// The action function is called with each iptable generated.
func generateIptables(
	containerIPs map[string]net.IP,
	portMappings []mesos.ContainerInfo_DockerInfo_PortMapping,
	driver *iptables.IPTables,
	action func(string, string, ...string) error,
	stopOnError bool) error {
	var err error

	// Get docker interface
	containerInterface := viper.GetString("iptables.container_bridge_interface")
	if containerInterface == "" {
		return fmt.Errorf("could not retrieve container brigde interface")
	}

	// Check if hook dedicated chains exist
	preroutingChain := viper.GetString("iptables.chains.prerouting")
	_, err = driver.List("nat", preroutingChain)
	if err != nil {
		return fmt.Errorf("%s prerouting chain doesn't exist in nat table", preroutingChain)
	}

	forwardChain := viper.GetString("iptables.chains.forward")
	_, err = driver.List("filter", forwardChain)
	if err != nil {
		return fmt.Errorf("%s forward chain doesn't exist in filter table", forwardChain)
	}

	postroutingChain := viper.GetString("iptables.chains.postrouting")
	_, err = driver.List("nat", postroutingChain)
	if err != nil {
		return fmt.Errorf("%s postrouting chain doesn't exist in nat table", postroutingChain)
	}

	ipForward := viper.GetBool("iptables.ip_forwarding")
	ipMasquerading := viper.GetBool("iptables.ip_masquerading")

	// Iterate over all container IPs, and for each IP, iterate on container/host binded ports.
	// Insert needed iptables for each IP and port.
	for _, containerIP := range containerIPs {
		// Insert rule for masquerading network flow going out of container. This rule only needs the
		// container IP
		if ipMasquerading {
			masqueradeRule := fmt.Sprintf(
				iptableHookMasqueradeRuleTemplate,
				containerInterface,
				containerIP.String(),
			)
			err = action("nat", postroutingChain, strings.Split(masqueradeRule, " ")...)
			if err != nil {
				if stopOnError {
					return err
				}

				logger.GetInstance().Warn(err.Error())
			}
		}

		for _, port := range portMappings {
			if ipMasquerading {
				// Insert rule for translating incoming data on host port to container
				dnatDestination := []string{containerIP.String(), ":", strconv.Itoa(int(port.GetContainerPort()))}
				dnatRule := fmt.Sprintf(
					iptableHookDnatRuleTemplate,
					containerInterface,
					port.GetProtocol(),
					strconv.Itoa(int(port.GetHostPort())),
					strings.Join(dnatDestination, ""),
				)
				err = action("nat", preroutingChain, strings.Split(dnatRule, " ")...)
				if err != nil {
					if stopOnError {
						return err
					}

					logger.GetInstance().Warn(err.Error())
				}

				// Insert rule for masquerading container -> container network flow
				selfMasqueradeRule := fmt.Sprintf(
					iptableHookSelfMasqueradeRuleTemplate,
					containerIP.String(),
					port.GetProtocol(),
					containerIP.String(),
					strconv.Itoa(int(port.GetContainerPort())),
				)
				err = action("nat", postroutingChain, strings.Split(selfMasqueradeRule, " ")...)
				if err != nil {
					if stopOnError {
						return err
					}

					logger.GetInstance().Warn(err.Error())
				}
			}

			if ipForward {
				// Insert rule for forwarding incoming data on host port to container
				forwardInRule := fmt.Sprintf(
					iptableHookForwardInRuleTemplate,
					containerIP.String(),
					containerInterface,
					containerInterface,
					port.GetProtocol(),
					strconv.Itoa(int(port.GetContainerPort())),
				)
				err = action("filter", forwardChain, strings.Split(forwardInRule, " ")...)
				if err != nil {
					if stopOnError {
						return err
					}

					logger.GetInstance().Warn(err.Error())
				}

				// Insert rule for forwarding incoming data on host port to container
				forwardOutRule := fmt.Sprintf(
					iptableHookForwardOutRuleTemplate,
					containerInterface,
					containerInterface,
					port.GetProtocol(),
					containerIP.String(),
					strconv.Itoa(int(port.GetContainerPort())),
				)
				err = action("filter", forwardChain, strings.Split(forwardOutRule, " ")...)
				if err != nil {
					if stopOnError {
						return err
					}

					logger.GetInstance().Warn(err.Error())
				}
			}
		}
	}

	return nil
}
