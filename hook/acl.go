package hook

import (
	"fmt"
	"net"
	"runtime"
	"strings"

	"github.com/Devatoria/go-mesos-executor/container"
	"github.com/Devatoria/go-mesos-executor/logger"
	"github.com/Devatoria/go-mesos-executor/types"

	"github.com/coreos/go-iptables/iptables"
	"github.com/mesos/mesos-go/api/v1/lib"
	"github.com/vishvananda/netns"
	"go.uber.org/zap"
)

const (
	aclHookLabel    = "EXECUTOR_ALLOWED_IP"
	aclHookProcPath = "/mnt/proc"
)

// ACLHook injects iptables rules into container namespace on post-run
// to allow only some IP to access the container. This hook needs to access
// to host procs (to mount network namespace).
var ACLHook = Hook{
	Name:     "acl",
	Priority: 0,
	Execute: func(c container.Containerizer, info *types.ContainerTaskInfo) error {
		for _, label := range info.TaskInfo.GetLabels().GetLabels() {
			// Ignore labels we do not care about
			if label.GetKey() != aclHookLabel {
				continue
			}

			// Stop here if label is set but null
			if label.GetValue() == "" {
				break
			}

			// Do not execute the hook if we are not on bridged network
			if info.TaskInfo.GetContainer().GetDocker().GetNetwork() != mesos.ContainerInfo_DockerInfo_BRIDGE {
				return fmt.Errorf("ACL hook can't inject iptables rules if network mode is not bridged")
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
				if _, _, err := net.ParseCIDR(ip); err == nil {
					parsedIPs = append(parsedIPs, ip)
					continue
				}

				return fmt.Errorf("Invalid IP: %s", ip)
			}

			logger.GetInstance().Production.Info("Injecting iptables rules",
				zap.Reflect("allowed", parsedIPs),
			)

			// Get container PID (to enter namespace)
			pid, err := c.ContainerGetPID(info.ContainerID)
			if err != nil {
				return fmt.Errorf("Unable to get the container PID: %v", err)
			}

			// Get container namespace
			ns, err := netns.GetFromPath(fmt.Sprintf("%s/%d/ns/net", aclHookProcPath, pid))
			if err != nil {
				return err
			}
			defer ns.Close()

			// Inject rules
			for _, ip := range parsedIPs {
				err = injectRuleIntoNamespace(ns, fmt.Sprintf("-s %s -j ACCEPT", ip))
				if err != nil {
					return fmt.Errorf("Error while injecting iptables rule: %v", err)
				}
			}

			// Inject base rules (allow gateway for health checks and drop everything)
			gateway, err := c.ContainerGetGatewayIP(info.ContainerID)
			if err != nil {
				return fmt.Errorf("Unable to get container gatewa IP: %v", err)
			}

			_ = injectRuleIntoNamespace(ns, fmt.Sprintf("-s %s -j ACCEPT", gateway.String()))
			_ = injectRuleIntoNamespace(ns, "-j DROP")
		}

		return nil
	},
}

// injectRuleIntoNamespace injects an iptables rule into the given namespace
func injectRuleIntoNamespace(ns netns.NsHandle, rule string) error {
	// Get current namespace
	currentNs, err := netns.Get()
	if err != nil {
		return err
	}
	defer currentNs.Close()

	// Current namespace and given namespace can't be the same
	// (otherwise, we will insert the rule on the host)
	if currentNs.UniqueId() == ns.UniqueId() {
		return fmt.Errorf("Current namespace and given namespace have the same unique ID: %s", ns.UniqueId())
	}

	logger.GetInstance().Development.Debug("Injecting a rule into the container namespace",
		zap.String("namespace", ns.UniqueId()),
		zap.String("rule", rule),
	)

	// Lock OS thread to avoid accidental namespace switching
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Switch on given namespace
	err = netns.Set(ns)
	if err != nil {
		return err
	}
	defer netns.Set(currentNs) // Ensure we will return into the base namespace

	// Inject rule
	ruleParts := strings.Split(rule, " ")
	driver, err := iptables.New()
	if err != nil {
		return err
	}

	err = driver.Append("filter", "INPUT", ruleParts...)
	if err != nil {
		return err
	}

	return nil
}
