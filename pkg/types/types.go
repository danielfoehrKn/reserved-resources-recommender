package types

const (
	// DefaultkubepodsCgroupName is the default cgroup name for kubepods
	// TODO: Should be based on cgroup driver (systemd: kubepods.slice, cgroups: kubepods)
	DefaultkubepodsCgroupName = "kubepods"
	// SystemSliceCgroupName is the default cgroup name for system.slice
	SystemSliceCgroupName = "system.slice"
	// DefaultDockerCgroupName is the default cgroup name for the docker container runtime
	DefaultDockerCgroupName = "docker.service"
)
