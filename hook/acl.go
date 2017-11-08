package hook

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"

	"github.com/Devatoria/go-mesos-executor/container"
	"github.com/Devatoria/go-mesos-executor/logger"
	"github.com/spf13/viper"

	"github.com/coreos/go-iptables/iptables"
	"github.com/mesos/mesos-go/api/v1/lib"
	"go.uber.org/zap"
)

type ACLHook struct {
	label        *regexp.Regexp
	ruleTemplate string
}

func (h *ACLHook) GetName() string {
	return "acl"
}

func (h *ACLHook) GetPriority() int64 {
	return 0
}

func (h *ACLHook) SetupHook() {
	h.label = regexp.MustCompile("EXECUTOR_(?P<portIndex>[0-9]+)_ACL")
	h.ruleTemplate = "-i %s -p %s -s %s --dport %s -j ACCEPT"
}

func (h *ACLHook) RunPostRun(containerizer container.Containerizer, taskInfo *mesos.TaskInfo, containerID string) error {
	// Do not execute the hook if we are not on bridged network
	if taskInfo.GetContainer().GetDocker().GetNetwork() != mesos.ContainerInfo_DockerInfo_BRIDGE {
		logger.GetInstance().Warn("ACL hook can't inject iptables rules if network mode is not bridged")

		return nil
	}

	driver, err := iptables.New()
	if err != nil {
		return err
	}

	chain, err := h.checkChain(driver)
	if err != nil {
		return err
	}

	return h.generateACL(taskInfo, chain, driver.Append, true)
}

func (h *ACLHook) RunPreStop(containerizer container.Containerizer, taskInfo *mesos.TaskInfo, containerID string) error {
	// Do not execute the hook if we are not on bridged network
	if taskInfo.GetContainer().GetDocker().GetNetwork() != mesos.ContainerInfo_DockerInfo_BRIDGE {
		logger.GetInstance().Warn("ACL hook can't inject iptables rules if network mode is not bridged")

		return nil
	}

	driver, err := iptables.New()
	if err != nil {
		return err
	}

	chain, err := h.checkChain(driver)
	if err != nil {
		return err
	}

	return h.generateACL(taskInfo, chain, driver.Delete, false)
}

// checkChain retrieves the iptables chain to use from configuration
// checks that this chains does exists in the filter table. It then returns
// the chain if found or an error if not found. An error is also returned if
// the configured chain is the built-in FORWARD or OUPUT
func (h *ACLHook) checkChain(driver *iptables.IPTables) (string, error) {
	// Get acl chain
	aclChain := viper.GetString("acl.chain")
	if aclChain == "" {
		return "", fmt.Errorf("no iptables chain set for acl hook")
	}

	if aclChain == "FORWARD" || aclChain == "OUTPUT" {
		return "", fmt.Errorf("forward and ouput chains cannot be used for acl injection")
	}

	chains, err := driver.ListChains("filter")
	if err != nil {
		return "", err
	}

	for i := range chains {
		if chains[i] == aclChain {
			return aclChain, nil
		}
	}

	return "", fmt.Errorf("Chain %s does not exists", aclChain)
}

// generateACL generates all needed iptables for access control.
// The action function is called with each iptable generated on the specified chain.
func (h *ACLHook) generateACL(
	info *mesos.TaskInfo,
	chain string,
	action func(string, string, ...string) error,
	stopOnError bool) error {
	var err error

	// Get external interface
	externalInterface := viper.GetString("acl.external_interface")
	if externalInterface == "" {
		logger.GetInstance().Warn(
			"No external interface set for acl hook. Acls will be set for all interfaces.")
		externalInterface = "all"
	}

	// Get task container ports
	portMappings := info.GetContainer().GetDocker().GetPortMappings()

	// Iterates over labels to find acl labels, check their value,
	// and insert corresponding iptables
	for _, label := range info.GetLabels().GetLabels() {
		match := h.label.FindStringSubmatch(label.GetKey())
		// Ignore labels we do not care about
		if match == nil {
			continue
		}

		// Check that port index is valid and match port mapping
		var portMapping mesos.ContainerInfo_DockerInfo_PortMapping
		var portIndex int
		if len(match) > 1 {
			portIndex, err = strconv.Atoi(match[1])
			if err != nil {
				return fmt.Errorf("Port index %d is not valid", portIndex)
			}
		} else {
			return fmt.Errorf("Could not retrieve port index")
		}

		if len(portMappings) > portIndex {
			portMapping = portMappings[portIndex]
		} else {
			return fmt.Errorf("Port index %d does not match port mapping definition", portIndex)
		}

		// Expected label value is a list of IP (with or without CIDR): 1.1.1.0/24,2.3.4.5,...
		// We need to split on coma and parse IP to check it they are well formated
		var parsedIPs []string
		ips := strings.Split(label.GetValue(), ",")
		for _, ip := range ips {
			// IP is correct but with no CIDR (we add it)
			if net.ParseIP(ip) != nil {
				parsedIPs = append(parsedIPs, fmt.Sprintf("%s/32", ip))
				continue
			}

			// IP is correct but with a CIDR
			if _, _, err = net.ParseCIDR(ip); err == nil {
				parsedIPs = append(parsedIPs, ip)
				continue
			}

			return fmt.Errorf("Invalid IP: %s", ip)
		}

		logger.GetInstance().Info("Injecting iptables rules",
			zap.Reflect("allowed", parsedIPs),
		)

		// Inject rules
		for _, ip := range parsedIPs {
			aclRule := fmt.Sprintf(
				h.ruleTemplate,
				externalInterface,
				portMapping.GetProtocol(),
				ip,
				strconv.Itoa(int(portMapping.GetHostPort())),
			)
			err = action("filter", chain, strings.Split(aclRule, " ")...)
			if err != nil {
				if stopOnError {
					return fmt.Errorf("Error while injecting acl iptables rule: %v", err)
				}
			}
		}
	}

	// Search for default allowed CIDR (always allowed, even if no label is given)
	defaultAllowedCIDR := viper.GetStringSlice("acl.default_allowed_cidr")
	if len(defaultAllowedCIDR) > 0 {
		for _, cidr := range defaultAllowedCIDR {
			for _, port := range portMappings {
				aclRule := fmt.Sprintf(
					h.ruleTemplate,
					externalInterface,
					port.GetProtocol(),
					cidr,
					strconv.Itoa(int(port.GetHostPort())),
				)
				err = action("filter", chain, strings.Split(aclRule, " ")...)
				if err != nil {
					if stopOnError {
						return fmt.Errorf("Error while injecting acl iptables rule: %v", err)
					}
				}
			}
		}
	}

	return nil
}
