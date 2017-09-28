package hook

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/Devatoria/go-mesos-executor/container"
	"github.com/Devatoria/go-mesos-executor/logger"
	"github.com/Devatoria/go-mesos-executor/types"

	"github.com/coreos/go-iptables/iptables"
	"github.com/mesos/mesos-go/api/v1/lib"
	"github.com/spf13/viper"
)

const (
	iptableHookDnatRuleTemplate           = "-i !%s -p %s -j DNAT --dport %s --to-destination %s --wait"
	iptableHookMasqueradeRuleTemplate     = "-o !%s -s %s/32 -j MASQUERADE --wait"
	iptableHookSelfMasqueradeRuleTemplate = "-d %s/32 -p %s -s %s/32 -j MASQUERADE --dport %s --wait"
	iptableHookForwardRuleTemplate        = "-d %s/32 -i !%s -o %s -p %s -j ACCEPT --dport %s --wait"
)

// containerIpCache is a map containing the containers ips. This map is useful when
// removing iptables when containers is stopped and does not have an IP anymore.
var iptablesHookContainerIPCache = sync.Map{}

// InsertIptablesHook injects iptables rules on host. This iptables allow container masquerading
// and network forwarding to container.
var InsertIptablesHook = Hook{
	Name:     "insertIptables",
	Priority: 0,
	Execute: func(c container.Containerizer, info *types.ContainerTaskInfo) error {
		// Do not execute the hook if we are not on bridged network
		if info.TaskInfo.GetContainer().GetDocker().GetNetwork() != mesos.ContainerInfo_DockerInfo_BRIDGE {
			logger.GetInstance().Warn("Insert Iptables hook can't inject iptables rules if network mode is not bridged")

			return nil
		}

		logger.GetInstance().Debug(fmt.Sprintf("Inserting iptables on host namespace for container %s", info.ContainerID))

		driver, err := iptables.New()
		if err != nil {
			return err
		}

		portMappings := info.TaskInfo.GetContainer().GetDocker().GetPortMappings()

		// Get container ip
		containerIPs, err := c.ContainerGetIPs(info.ContainerID)
		if err != nil {
			return err
		}
		iptablesHookContainerIPCache.Store(info.ContainerID, containerIPs)

		err = generateIptables(containerIPs, portMappings, driver.Append, true)
		if err != nil {
			return err
		}

		return nil
	},
}

// RemoveIptablesHook removes iptables injected by injectIptables hook.
var RemoveIptablesHook = Hook{
	Name:     "removeIptables",
	Priority: 0,
	Execute: func(c container.Containerizer, info *types.ContainerTaskInfo) error {
		// Do not execute the hook if we are not on bridged network
		if info.TaskInfo.GetContainer().GetDocker().GetNetwork() != mesos.ContainerInfo_DockerInfo_BRIDGE {
			logger.GetInstance().Warn("Iptables hook does not need to remove iptables rules if network mode is not bridged")

			return nil
		}

		logger.GetInstance().Debug(fmt.Sprintf("Removing iptables on host namespace for container %s", info.ContainerID))

		driver, err := iptables.New()
		if err != nil {
			return err
		}

		portMappings := info.TaskInfo.GetContainer().GetDocker().GetPortMappings()

		// Retrieve container IPs from cache
		ipsCacheValue, ok := iptablesHookContainerIPCache.Load(info.ContainerID)
		if !ok {
			return fmt.Errorf(
				"Could not find ip in cache for container %s.",
				info.ContainerID,
			)
		}

		containerIPs, ok := ipsCacheValue.(map[string]net.IP)
		if !ok {
			return fmt.Errorf(
				"Could not load ip from cache for container %s.",
				info.ContainerID,
			)
		}

		err = generateIptables(containerIPs, portMappings, driver.Delete, false)
		if err != nil {
			return err
		}

		return nil
	},
}

// generateIptables generates all needed iptables for containers masquerading/ network forwarding.
// The action function is called with each iptable generated.
func generateIptables(
	containerIPs map[string]net.IP,
	portMappings []mesos.ContainerInfo_DockerInfo_PortMapping,
	action func(string, string, ...string) error,
	stopOnError bool) error {
	// Get docker interface
	containerInterface := viper.GetString("iptables.container_bridge_interface")
	if containerInterface == "" {
		return fmt.Errorf("Error : could not retrieve container brigde interface.")
	}

	// Iterate over all container IPs, and for each IP, iterate on container/host binded ports.
	// Insert needed iptables for each IP and port.
	for _, containerIP := range containerIPs {
		// Insert rule for masquerading network flow going out of container. This rule only needs the
		// container IP
		masqueradeRule := fmt.Sprintf(
			iptableHookMasqueradeRuleTemplate,
			containerInterface,
			containerIP.String(),
		)
		err := action("nat", "POSTROUTING", strings.Split(masqueradeRule, " ")...)
		if err != nil {
			if stopOnError == true {
				return err
			} else {
				logger.GetInstance().Warn(err.Error())
			}
		}

		for _, port := range portMappings {
			// Insert rule for translating incoming data on host port to container
			dnatDestination := []string{containerIP.String(), ":", strconv.Itoa(int(port.GetContainerPort()))}
			dnatRule := fmt.Sprintf(
				iptableHookDnatRuleTemplate,
				containerInterface,
				port.GetProtocol(),
				strconv.Itoa(int(port.GetHostPort())),
				strings.Join(dnatDestination, ""),
			)
			err = action("nat", "PREROUTING", strings.Split(dnatRule, " ")...)
			if err != nil {
				if stopOnError == true {
					return err
				} else {
					logger.GetInstance().Warn(err.Error())
				}
			}

			// Insert rule for masquerading container -> container network flow
			selfMasqueradeRule := fmt.Sprintf(
				iptableHookSelfMasqueradeRuleTemplate,
				containerIP.String(),
				port.GetProtocol(),
				containerIP.String(),
				strconv.Itoa(int(port.GetContainerPort())),
			)
			err = action("nat", "POSTROUTING", strings.Split(selfMasqueradeRule, " ")...)
			if err != nil {
				if stopOnError == true {
					return err
				} else {
					logger.GetInstance().Warn(err.Error())
				}
			}

			// Insert rule for forwarding incoming data on host port to container
			forwardRule := fmt.Sprintf(
				iptableHookForwardRuleTemplate,
				containerIP.String(),
				containerInterface,
				containerInterface,
				port.GetProtocol(),
				strconv.Itoa(int(port.GetContainerPort())),
			)
			err = action("filter", "FORWARD", strings.Split(forwardRule, " ")...)
			if err != nil {
				if stopOnError == true {
					return err
				} else {
					logger.GetInstance().Warn(err.Error())
				}
			}
		}
	}

	return nil
}
