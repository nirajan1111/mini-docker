package network

import (
	"fmt"

	log "github.com/sirupsen/logrus"
	"github.com/nirajansah/mini-docker/config"
)

// VethPair creates and configures a virtual ethernet pair for container networking.
//
// A veth pair is like a virtual cable with two ends:
//   - One end (veth-{id}) stays in the host namespace, connected to the bridge
//   - Other end (eth0) is moved into the container's network namespace
//
// This enables network communication between container and host/internet.
type VethPair struct {
	containerID string
	pid         int
	bridge      *Bridge
}

func NewVethPair(containerID string, pid int, bridge *Bridge) *VethPair {
	return &VethPair{
		containerID: containerID,
		pid:         pid,
		bridge:      bridge,
	}
}

// Setup creates the veth pair and configures networking.
func (v *VethPair) Setup() (string, error) {
	hostVeth := "veth-" + v.containerID[:8]
	containerVeth := "eth0"

	log.Debugf("creating veth pair: %s <-> %s (PID %d)", hostVeth, containerVeth, v.pid)

	// 1. Create the veth pair
	if err := run("ip", "link", "add", hostVeth, "type", "veth", "peer", "name", containerVeth); err != nil {
		return "", fmt.Errorf("create veth pair: %w", err)
	}

	// 2. Attach host end to the bridge
	if err := run("ip", "link", "set", hostVeth, "master", config.BridgeName); err != nil {
		return "", fmt.Errorf("attach to bridge: %w", err)
	}

	// 3. Bring host end up
	if err := run("ip", "link", "set", hostVeth, "up"); err != nil {
		return "", fmt.Errorf("bring host veth up: %w", err)
	}

	// 4. Move container end into the container's network namespace
	if err := run("ip", "link", "set", containerVeth, "netns", fmt.Sprintf("%d", v.pid)); err != nil {
		return "", fmt.Errorf("move veth to container ns: %w", err)
	}

	// 5. Configure the container's network interface
	containerIP := v.bridge.AllocateIP()
	nsenter := fmt.Sprintf("%d", v.pid)

	// Set IP address on the container's eth0
	if err := run("nsenter", "-t", nsenter, "-n", "ip", "addr", "add", containerIP, "dev", "eth0"); err != nil {
		return "", fmt.Errorf("set container IP: %w", err)
	}

	// Bring loopback up
	if err := run("nsenter", "-t", nsenter, "-n", "ip", "link", "set", "lo", "up"); err != nil {
		log.Warnf("bring loopback up: %v", err)
	}

	// Bring eth0 up
	if err := run("nsenter", "-t", nsenter, "-n", "ip", "link", "set", "eth0", "up"); err != nil {
		return "", fmt.Errorf("bring container eth0 up: %w", err)
	}

	// 6. Set default route through the bridge
	gateway := v.bridge.GatewayIP()
	if err := run("nsenter", "-t", nsenter, "-n", "ip", "route", "add", "default", "via", gateway); err != nil {
		return "", fmt.Errorf("set default route: %w", err)
	}

	log.Infof("container network: %s via %s", containerIP, gateway)
	return containerIP, nil
}

// Cleanup removes the veth pair (removing host end automatically removes container end).
func (v *VethPair) Cleanup() {
	hostVeth := "veth-" + v.containerID[:8]
	if err := run("ip", "link", "del", hostVeth); err != nil {
		log.Debugf("cleanup veth %s: %v", hostVeth, err)
	}
}
