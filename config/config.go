package config

const (
	ImageStorePath     = "/var/mini-docker/images"
	ContainerStorePath = "/var/mini-docker/containers"
	CgroupRoot         = "/sys/fs/cgroup"
	BridgeName         = "minidocker0"
	BridgeCIDR         = "10.100.0.1/24"
	ContainerSubnet    = "10.100.0.0/24"

	DefaultRegistry   = "https://registry-1.docker.io"
	DefaultTag        = "latest"
	DefaultMemLimit   = 64 * 1024 * 1024 // 64 MB
	DefaultPidLimit   = 64
	DefaultCPULimit   = 1
	DefaultLogLevel   = "info"
)
