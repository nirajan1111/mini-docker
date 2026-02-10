# mini-docker

A container runtime built from scratch in Go. No Docker libraries, no container runtimes as dependencies — just raw Linux syscalls and the Go standard library.

I built this to understand how containers *actually* work under the hood. Turns out, a "container" isn't some magical VM — it's just a regular Linux process with a few kernel features bolted on: namespaces for isolation, cgroups for resource limits, and a union filesystem for layered images.

## What it does

```
$ sudo mini-docker pull alpine
pulling library/alpine:latest from https://registry-1.docker.io
multi-arch image detected, selecting arm64/linux
manifest has 1 layers
layer 1/1: sha256:d8ad8cd72600 (4.00 MB)
image alpine:latest pulled successfully (1 layers)

$ sudo mini-docker run alpine /bin/sh
/ # hostname
a3f8e2b10c4d
/ # ps aux
PID   USER     TIME  COMMAND
    1 root      0:00 /bin/sh
    2 root      0:00 ps aux
/ # cat /etc/os-release
NAME="Alpine Linux"
/ # exit
```

The container gets its own hostname, its own PID space (the shell is PID 1), its own filesystem, its own network namespace, and resource limits — all from ~1900 lines of Go.

## How containers work (what I learned)

A container is a process that *thinks* it's alone on the machine. Linux provides the kernel features to make this illusion work:

### Namespaces — "what can this process see?"

Namespaces limit a process's view of the system. mini-docker creates 5 of them:

| Namespace | What it isolates | Effect |
|-----------|-----------------|--------|
| **PID** | Process IDs | Container's first process is PID 1, can't see host processes |
| **UTS** | Hostname | Container gets its own hostname (the container ID) |
| **Mount** | Filesystem mounts | Container's mounts don't leak to the host |
| **IPC** | Inter-process communication | Shared memory / semaphores are isolated |
| **Network** | Network stack | Container gets its own network interfaces and IP |

The implementation uses `clone()` flags when spawning the container process:

```go
cmd.SysProcAttr = &syscall.SysProcAttr{
    Cloneflags: syscall.CLONE_NEWUTS |   // own hostname
                syscall.CLONE_NEWPID |   // own PID space
                syscall.CLONE_NEWNS  |   // own mounts
                syscall.CLONE_NEWIPC |   // own IPC
                syscall.CLONE_NEWNET,    // own network
}
```

### Cgroups — "how much can this process use?"

Cgroups (control groups) limit resource consumption. Without them, a container could eat all your RAM or fork-bomb the host.

mini-docker supports both cgroup v1 and v2 (auto-detected):

- **Memory**: caps RAM usage (default 64MB)
- **CPU**: limits CPU time via CFS bandwidth control
- **PIDs**: caps the number of processes (prevents fork bombs)

```
# cgroup v2 (modern kernels):
/sys/fs/cgroup/mini-docker.slice/mini-docker-{id}.scope/
    memory.max    -> 67108864
    pids.max      -> 64
    cpu.max       -> 100000 100000

# cgroup v1 (older kernels):
/sys/fs/cgroup/memory/mini-docker/{id}/memory.limit_in_bytes
/sys/fs/cgroup/pids/mini-docker/{id}/pids.max
/sys/fs/cgroup/cpu/mini-docker/{id}/cpu.cfs_quota_us
```

### OverlayFS — "how does the filesystem work?"

Container images are made of layers. OverlayFS stacks them into a single view:

```
┌─────────────────────────────────────────┐
│           merged (container view)       │  ← what the process sees as /
├─────────────────────────────────────────┤
│           upper  (container writes)     │  ← changes go here (copy-on-write)
├─────────────────────────────────────────┤
│           lower  (image layers, r/o)    │  ← original image, never modified
│  ┌─────────────────────────────────┐    │
│  │  layer 2 (app files)            │    │
│  ├─────────────────────────────────┤    │
│  │  layer 1 (base OS)              │    │
│  └─────────────────────────────────┘    │
└─────────────────────────────────────────┘
```

When a container writes a file, OverlayFS copies it to the upper dir first (copy-on-write). The original image stays untouched, so multiple containers can share the same base layers.

### pivot_root — "why not just chroot?"

mini-docker uses `pivot_root` instead of `chroot` to switch the container's root filesystem. The difference matters:

- `chroot` just changes the path resolution root. A root process can escape it.
- `pivot_root` actually swaps the mount point. The old root gets unmounted entirely — there's nothing to escape to.

```go
syscall.PivotRoot(newRoot, putOld)  // swap root
syscall.Unmount("/.pivot_old", syscall.MNT_DETACH)  // remove old root
os.RemoveAll("/.pivot_old")  // clean up
```

### Network isolation

Each container gets its own network namespace with a virtual ethernet (veth) pair:

```
   Host                              Container
┌──────────────┐                 ┌──────────────────┐
│ minidocker0  │◄── veth pair ──►│ eth0 (10.100.0.x)│
│ 10.100.0.1   │                 │                  │
│ (bridge)     │                 └──────────────────┘
└──────┬───────┘
       │ NAT (iptables MASQUERADE)
       │
   host eth0 → internet
```

### The /proc/self/exe trick

This is the most clever part. When you run `mini-docker run`, the process needs to:
1. Create new namespaces (can only be done at process creation)
2. Set up the filesystem inside those namespaces

But you can't do both in the same process. So mini-docker re-executes *itself* with a hidden `_child` command:

```
mini-docker run alpine /bin/sh
    │
    ├── fork with CLONE_NEW* flags
    │       │
    │       └── /proc/self/exe _child --id abc123 --image alpine ...
    │               │
    │               ├── mount overlayfs
    │               ├── pivot_root
    │               ├── mount /proc, /sys, /dev
    │               └── exec /bin/sh  (replaces the process)
    │
    ├── apply cgroups to child PID
    ├── set up networking (veth pair)
    └── wait for child to exit
```

## Architecture

```
mini-docker/
├── main.go                  # entry point
├── cmd/                     # CLI commands (cobra)
│   ├── root.go              # root command, flag parsing
│   ├── run.go               # mini-docker run
│   ├── pull.go              # mini-docker pull
│   ├── ps.go                # mini-docker ps
│   ├── rm.go                # mini-docker rm
│   ├── exec.go              # mini-docker exec
│   ├── images.go            # mini-docker images
│   └── child.go             # hidden _child command (runs inside namespaces)
├── runtime/                 # container lifecycle
│   ├── runtime.go           # parent process: namespaces, cgroups, wait
│   └── init.go              # child process: overlayfs, pivot_root, exec
├── image/                   # Docker Hub interaction
│   ├── registry.go          # V2 registry API (token auth, manifest resolution)
│   └── image.go             # image/layer structs
├── storage/                 # filesystem management
│   ├── store.go             # image + container metadata stores
│   ├── overlay.go           # overlayfs mount/unmount
│   └── archive.go           # tar/gzip extraction
├── cgroups/
│   └── cgroup.go            # resource limits (v1 + v2 auto-detect)
├── network/
│   ├── bridge.go            # virtual bridge (like docker0)
│   └── veth.go              # veth pair setup + IP assignment
└── config/
    └── config.go            # constants and defaults
```

### Storage layout on disk

```
/var/mini-docker/
├── images/
│   ├── _layers/
│   │   ├── sha256-abc123.../      # extracted layer filesystem
│   │   └── sha256-def456.../
│   └── alpine/
│       └── latest.json            # image metadata
└── containers/
    └── a3f8e2b10c4d.../
        ├── upper/                 # container writes (OverlayFS)
        ├── work/                  # OverlayFS internal scratch
        ├── merged/                # union mount (what container sees)
        └── config.json            # container metadata
```

## Building

**Requirements**: Go 1.21+ and a Linux machine (or VM). Container features use Linux kernel syscalls that don't exist on macOS/Windows.

```bash
# on Linux:
make build

# cross-compile from macOS for Linux:
make build-linux       # x86_64
make build-linux-arm   # arm64 (Apple Silicon VMs)
```

## Usage

Everything requires root (namespace creation, mounting, cgroups all need it).

```bash
# pull an image from Docker Hub
sudo ./mini-docker pull alpine
sudo ./mini-docker pull ubuntu

# run a container
sudo ./mini-docker run alpine /bin/sh
sudo ./mini-docker run --memory 33554432 --pids-limit 16 alpine /bin/sh
sudo ./mini-docker run --name mycontainer alpine /bin/echo "hello"

# list containers and images
sudo ./mini-docker ps
sudo ./mini-docker images

# clean up
sudo ./mini-docker rm <container-id>
```

### Flags for `run`

| Flag | Default | Description |
|------|---------|-------------|
| `--name` | auto-generated | container name |
| `-m, --memory` | 67108864 (64MB) | memory limit in bytes |
| `--pids-limit` | 64 | max number of processes |
| `--cpus` | 1 | number of CPUs |

## Testing with Multipass (macOS)

If you're on macOS, the easiest way to get a Linux VM is [Multipass](https://multipass.run/):

```bash
brew install multipass
multipass launch --name docker-dev
multipass shell docker-dev

# inside the VM:
git clone <this-repo>
cd mini-docker
make build
sudo ./build/mini-docker pull alpine
sudo ./build/mini-docker run alpine /bin/sh
```

## What I'd add next

- [ ] Volume mounts (`-v host:container`)
- [ ] Port forwarding (`-p 8080:80`)
- [ ] Image layer caching with content-addressable storage
- [ ] Container logs (`mini-docker logs <id>`)
- [ ] Resource usage stats (`mini-docker stats`)
- [ ] Seccomp profiles for syscall filtering
- [ ] User namespace mapping (rootless containers)

## References

These helped me understand the internals:

- [Linux namespaces man page](https://man7.org/linux/man-pages/man7/namespaces.7.html)
- [cgroups v2 documentation](https://docs.kernel.org/admin-guide/cgroup-v2.html)
- [OverlayFS kernel docs](https://docs.kernel.org/filesystems/overlayfs.html)
- [Docker registry HTTP API V2](https://distribution.github.io/distribution/spec/api/)
- [pivot_root(2)](https://man7.org/linux/man-pages/man2/pivot_root.2.html)
- Containers from scratch talks by Liz Rice and Eric Chiang
