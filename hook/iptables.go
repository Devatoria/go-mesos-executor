package hook

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Devatoria/go-mesos-executor/container"
	"github.com/Devatoria/go-mesos-executor/logger"
	"github.com/Devatoria/go-mesos-executor/types"

	"github.com/coreos/go-iptables/iptables"
	"github.com/mesos/mesos-go/api/v1/lib"
	"github.com/spf13/viper"
)

const (
	iptableHookDnatRuleTemplate           = "-i !%s -p %s -j DNAT --dport %s --to-destination %s"
	iptableHookMasqueradeRuleTemplate     = "-o !%s -s %s/32 -j MASQUERADE"
	iptableHookSelfMasqueradeRuleTemplate = "-d %s/32 -p %s -s %s/32 -j MASQUERADE --dport %s"
	iptableHookForwardRuleTemplate        = "-d %s/32 -i !%s -o %s -p %s -j ACCEPT --dport %s"
)

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

		logger.GetInstance().Debug("Inserting iptables on host namespace")

		driver, err := iptables.New()
		if err != nil {
			return err
		}

		err = generateIptables(c, info, driver.Append)
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

		logger.GetInstance().Debug("Removing iptables on host namespace")

		driver, err := iptables.New()
		if err != nil {
			return err
		}

		err = generateIptables(c, info, driver.Delete)
		if err != nil {
			return err
		}

		return nil
	},
}

// generateIptables generates all needed iptables for containers masquerading/ network forwarding.
// The action function is called with each iptable generated.
func generateIptables(c container.Containerizer, info *types.ContainerTaskInfo, action func(string, string, ...string) error) error {
	// Get docker interface
	containerInterface := viper.GetString("iptables.container_bridge_interface")

	// Get task container ports
	portMappings := info.TaskInfo.GetContainer().GetDocker().GetPortMappings()

	// Get container ip
	containerIPs, err := c.ContainerGetIPs(info.ContainerID)
	if err != nil {
		return err
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
		err = action("nat", "POSTROUTING", strings.Split(masqueradeRule, " ")...)
		if err != nil {
			return err
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
				return err
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
				return err
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
				return err
			}
		}
	}

	return nil
}
