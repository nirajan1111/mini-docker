package network

import (
	"fmt"
	"net"
	"os/exec"

	log "github.com/sirupsen/logrus"
	"github.com/nirajansah/mini-docker/config"
)

// Bridge manages the virtual bridge network for containers.
// Similar to Docker's docker0 bridge, this creates a virtual switch that
// connects all container network namespaces to the host.
//
// Network topology:
//
//	Host namespace                   Container namespace
//	┌─────────────┐                 ┌──────────────────┐
//	│  minidocker0 │◄──── veth ────►│ eth0 (10.100.0.x)│
//	│  10.100.0.1  │    pair        │                  │
//	│  (bridge)    │                │                  │
//	└──────┬───────┘                └──────────────────┘
//	       │
//	   NAT (iptables)
//	       │
//	   ┌───┴───┐
//	   │  eth0 │  (host physical interface)
//	   └───────┘
type Bridge struct {
	name string
	cidr string
}

var nextIP byte = 2 // Start assigning from 10.100.0.2

func NewBridge() *Bridge {
	return &Bridge{
		name: config.BridgeName,
		cidr: config.BridgeCIDR,
	}
}

// Setup creates the bridge interface if it doesn't exist.
func (b *Bridge) Setup() error {
	// Check if bridge already exists
	if _, err := net.InterfaceByName(b.name); err == nil {
		log.Debugf("bridge %s already exists", b.name)
		return nil
	}

	log.Infof("creating bridge %s with CIDR %s", b.name, b.cidr)

	// Create the bridge interface
	if err := run("ip", "link", "add", b.name, "type", "bridge"); err != nil {
		return fmt.Errorf("create bridge: %w", err)
	}

	// Assign IP address to the bridge
	if err := run("ip", "addr", "add", b.cidr, "dev", b.name); err != nil {
		return fmt.Errorf("assign bridge IP: %w", err)
	}

	// Bring the bridge up
	if err := run("ip", "link", "set", b.name, "up"); err != nil {
		return fmt.Errorf("bring bridge up: %w", err)
	}

	// Enable IP forwarding (allows traffic between namespaces and host)
	if err := run("sysctl", "-w", "net.ipv4.ip_forward=1"); err != nil {
		log.Warnf("enable ip forwarding: %v", err)
	}

	// Set up NAT so containers can reach the internet
	if err := run("iptables", "-t", "nat", "-A", "POSTROUTING",
		"-s", config.ContainerSubnet, "-j", "MASQUERADE"); err != nil {
		log.Warnf("setup NAT: %v", err)
	}

	return nil
}

// AllocateIP returns the next available IP for a container.
func (b *Bridge) AllocateIP() string {
	ip := fmt.Sprintf("10.100.0.%d/24", nextIP)
	nextIP++
	return ip
}

// GatewayIP returns the bridge IP (used as default gateway by containers).
func (b *Bridge) GatewayIP() string {
	return "10.100.0.1"
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %s: %w", name, args, string(out), err)
	}
	return nil
}
