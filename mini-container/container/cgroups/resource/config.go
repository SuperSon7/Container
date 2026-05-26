package resource

type Config struct {
	// MemoryLimit is the memory limit in bytes. A negative value maps to "max".
	MemoryLimit int64 `json:"memory,omitempty"`

	// CpuQuota is the allowed CPU time in a period, in microseconds.
	CpuQuota int64 `json:"cpu_quota,omitempty"`

	// CpuPeriod is the CPU quota period in microseconds. Zero uses the kernel default.
	CpuPeriod uint64 `json:"cpu_period,omitempty"`

	// PidsLimit limits the number of processes. Nil keeps the current limit.
	PidsLimit *int64 `json:"pids_limit,omitempty"`
}
