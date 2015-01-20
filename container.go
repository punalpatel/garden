package garden

import (
	"io"
	"net"
)

//go:generate counterfeiter . Container

type Container interface {
	Handle() string

	// Stop stops a container.
	//
	// If kill is false, garden stops a container by sending the processes running inside it the SIGTERM signal.
	// It then waits for the processes to terminate before returning a response.
	// If one or more processes do not terminate within 10 seconds,
	// garden sends these processes the SIGKILL signal, killing them ungracefully.
	//
	// If kill is true, garden stops a container by sending the processing running inside it a SIGKILL signal.
	//
	// Once a container is stopped, garden does not allow spawning new processes inside the container.
	// It is possible to copy files in to and out of a stopped container.
	// It is only when a container is destroyed that its filesystem is cleaned up.
	//
	// Errors:
	// * None.
	Stop(kill bool) error

	// Returns information about a container.
	Info() (ContainerInfo, error)

	// StreamIn streams data into a file in a container.
	//
	// Errors:
	// *  TODO.
	StreamIn(dstPath string, tarStream io.Reader) error

	// StreamOut streams a file out of a container.
	//
	// Errors:
	// * TODO.
	StreamOut(srcPath string) (io.ReadCloser, error)

	// Limits the network bandwidth for a container.
	LimitBandwidth(limits BandwidthLimits) error

	CurrentBandwidthLimits() (BandwidthLimits, error)

	// Limits the CPU shares for a container.
	LimitCPU(limits CPULimits) error

	CurrentCPULimits() (CPULimits, error)

	// Limits the disk usage for a container.
	//
	// The disk limits that are set by this command only have effect for the container's unprivileged user.
	// Files/directories created by its privileged user are not subject to these limits.
	//
	// TODO: explain how disk management works.
	LimitDisk(limits DiskLimits) error

	CurrentDiskLimits() (DiskLimits, error)

	// Limits the memory usage for a container.
	//
	// The limit applies to all process in the container. When the limit is
	// exceeded, the container will be automatically stopped.
	//
	// Errors:
	// * The kernel does not support setting memory.memsw.limit_in_bytes.
	LimitMemory(limits MemoryLimits) error

	CurrentMemoryLimits() (MemoryLimits, error)

	// Map a port on the host to a port in the container so that traffic to the
	// host port is forwarded to the container port.
	//
	// If a host port is not given, a port will be acquired from the server's port
	// pool.
	//
	// If a container port is not given, the port will be the same as the
	// container port.
	//
	// The two resulting ports are returned in the response.
	//
	// Errors:
	// * When no port can be acquired from the server's port pool.
	NetIn(hostPort, containerPort uint32) (uint32, uint32, error)

	// Whitelist outbound network traffic.
	//
	// If the configuration directive deny_networks is not used,
	// all networks are already whitelisted and this command is effectively a no-op.
	//
	// * netOutRuler: one of:
	//
	//    AllRule{
	//       Network string
	//       Log     bool
	//    }
	//    TCPRule{
	//       Network   string
	//       Port      uint32
	//       PortRange PortRange{ Start uint32; End uint32 }
	//       Log       bool
	//    }
	//    UDPRule{
	//       Network   string
	//       Port      uint32
	//       PortRange PortRange{ Start uint32; End uint32 }
	//    }
	//    ICMPRule{
	//       Network string
	//       Type    ICMPType(int32)    // default all ICMP types
	//       Code    ICMPCode(int32)    // default all ICMP codes
	//    }
	//    NetOutRule{
	//       Network   string
	//       Port      uint32
	//       PortRange PortRange{ Start uint32; End uint32 }
	//       Protocol  Protocol
	//       IcmpType  int32
	//       IcmpCode  int32
	//       Log       bool
	//    }
	//
	// all of which implement the interface method Rule() NetOutRule,
	// and where:
	//
	// * Network: Network to whitelist (in the form 1.2.3.4/8) or a range of IP
	//            addresses to whitelist (separated by -)
	//
	// * Port: port to whitelist
	//
	// * PortRange: range of ports to whitelist; Start to End inclusive
	//
	// * Protocol : the protocol to be whitelisted (default TCP)
	//
	// * IcmpType: the ICMP type value to be whitelisted when protocol=ICMP (a
	//             value of -1 means all types)
	//
	// * IcmpCode: the ICMP code value to be whitelisted when protocol=ICMP (a
	//             value of -1 means all codes)
	//
	// * Type: (in ICMPRule only) the result of ICMPType(int32) used as IcmpType;
	//         the default is -1, meaning all types
	//
	// * Code: (in ICMPRule only) the result of ICMPCode(int32) used as IcmpCode;
	//         the default is -1, meaning all codes
	//
	// * Log: boolean specifying whether or not logging should be enabled, only
	//        applies for protocol TCP.
	//
	// Errors:
	// * None.
	NetOut(netOutRule NetOutRule) error

	// Run a script inside a container.
	//
	// The 'privileged' flag remains for backwards compatibility, but the 'user' flag is preferred.
	// The root user will be mapped to a non-root UID in the host unless the container (not this process) was created with 'privileged' true.
	//
	// Errors:
	// * TODO.
	Run(ProcessSpec, ProcessIO) (Process, error)

	// Attach starts streaming the output back to the client from a specified process.
	//
	// Errors:
	// * processID does not refer to a running process.
	Attach(processID uint32, io ProcessIO) (Process, error)

	// GetProperty returns the value of the property with the specified name.
	//
	// Errors:
	// * When the property does not exist on the container.
	GetProperty(name string) (string, error)

	// Set a named property on a container to a specified value.
	//
	// Errors:
	// * None.
	SetProperty(name string, value string) error

	// Remove a property with the specified name from a container.
	//
	// Errors:
	// * None.
	RemoveProperty(name string) error
}

type Protocol uint8

const (
	ProtocolTCP Protocol = 1 << iota
	ProtocolUDP
	ProtocolICMP

	ProtocolAll Protocol = (1 << iota) - 1
)

type NetworkInfo struct {
	Start net.IP
	End   net.IP
}

func AllNetworks() NetworkInfo {
	return IPRange(net.ParseIP("0.0.0.0"), net.ParseIP("255.255.255.255"))
}

func IPRange(start, end net.IP) NetworkInfo {
	return NetworkInfo{
		Start: start,
		End:   end,
	}
}

func IP(ip net.IP) NetworkInfo {
	return IPRange(ip, ip)
}

func CidrNetwork(ipNet net.IPNet) {
	return IPRange(ipNet.IP, lastIP(ipNet))
}

type Ports struct {
	Start uint16
	End   uint16
}

func AllPorts() *Ports {
	return Port(0)
}

func Port(port uint16) *Ports {
	return PortRange(port, port)
}

func PortRange(start, end uint16) *Ports {
	return &Ports{
		Start: start,
		End:   end,
	}
}

type ICMPCtrl struct {
	Type *uint8
	Code *uint8
}

func AllICMP() *ICMPCtrl {
	return &ICMPCtrl{}
}

func TypeICMP(iType uint8) *ICMPCtrl {
	pType := iType
	return &ICMPCtrl{
		Type: &pType,
		Code: nil,
	}
}

func ICMPControl(iType, iCode uint8) *ICMPCtrl {
	pType, pCode := iType, iCode
	return &ICMPCtrl{
		Type: &pType,
		Code: &pCode,
	}
}

type NetOutRule struct {
	Protocol Protocol
	Network  NetworkInfo
	Port     *Ports
	IcmpInfo *ICMPCtrl
	Log      bool
}

type AllRule struct {
	Network string
	Log     bool
}

type UDPRule struct {
	Network   string
	Port      uint32
	PortRange Ports
}

type TCPRule struct {
	Network   string
	Port      uint32
	PortRange Ports
	Log       bool
}

type ICMPRule struct {
	Network string
	Type    *iCMPType
	Code    *iCMPCode
}

// ProcessSpec contains parameters for running a script inside a container.
type ProcessSpec struct {
	Path string   // Path to command to execute.
	Args []string // Arguments to pass to command.
	Env  []string // Environment variables.
	Dir  string   // Working directory (default: home directory).

	Privileged bool   // Whether to run the script as root or not. Can be overriden by 'user', if specified.
	User       string // The name of a user in the container to run the process as. If not specified defaults to 'root' for privileged processes, and 'vcap' for unprivileged processes.

	Limits ResourceLimits // Resource limits
	TTY    *TTYSpec       // Execute with a TTY for stdio.
}

type TTYSpec struct {
	WindowSize *WindowSize
}

type WindowSize struct {
	Columns int
	Rows    int
}

type ProcessIO struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

//go:generate counterfeiter . Process

type Process interface {
	ID() uint32
	Wait() (int, error)
	SetTTY(TTYSpec) error
	Signal(Signal) error
}

type Signal int

const (
	SignalTerminate Signal = iota
	SignalKill
)

type PortMapping struct {
	HostPort      uint32
	ContainerPort uint32
}

// ContainerInfo holds information about a container.
type ContainerInfo struct {
	State         string                 // Either "active" or "stopped".
	Events        []string               // List of events that occurred for the container. It currently includes only "oom" (Out Of Memory) event if it occurred.
	HostIP        string                 // The IP address of the gateway which controls the host side of the container's virtual ethernet pair.
	ContainerIP   string                 // The IP address of the container side of the container's virtual ethernet pair.
	ExternalIP    string                 //
	ContainerPath string                 // The path to the directory holding the container's files (both its control scripts and filesystem).
	ProcessIDs    []uint32               // List of running processes.
	MemoryStat    ContainerMemoryStat    //
	CPUStat       ContainerCPUStat       //
	DiskStat      ContainerDiskStat      //
	BandwidthStat ContainerBandwidthStat //
	Properties    Properties             // List of properties defined for the container.
	MappedPorts   []PortMapping          //
}

type ContainerMemoryStat struct {
	Cache                   uint64
	Rss                     uint64
	MappedFile              uint64
	Pgpgin                  uint64
	Pgpgout                 uint64
	Swap                    uint64
	Pgfault                 uint64
	Pgmajfault              uint64
	InactiveAnon            uint64
	ActiveAnon              uint64
	InactiveFile            uint64
	ActiveFile              uint64
	Unevictable             uint64
	HierarchicalMemoryLimit uint64
	HierarchicalMemswLimit  uint64
	TotalCache              uint64
	TotalRss                uint64
	TotalMappedFile         uint64
	TotalPgpgin             uint64
	TotalPgpgout            uint64
	TotalSwap               uint64
	TotalPgfault            uint64
	TotalPgmajfault         uint64
	TotalInactiveAnon       uint64
	TotalActiveAnon         uint64
	TotalInactiveFile       uint64
	TotalActiveFile         uint64
	TotalUnevictable        uint64
}

type ContainerCPUStat struct {
	Usage  uint64
	User   uint64
	System uint64
}

type ContainerDiskStat struct {
	BytesUsed  uint64
	InodesUsed uint64
}

type ContainerBandwidthStat struct {
	InRate   uint64
	InBurst  uint64
	OutRate  uint64
	OutBurst uint64
}

type BandwidthLimits struct {
	RateInBytesPerSecond      uint64
	BurstRateInBytesPerSecond uint64
}

type DiskLimits struct {
	BlockSoft uint64
	BlockHard uint64

	InodeSoft uint64
	InodeHard uint64

	ByteSoft uint64 // New soft block limit specified in bytes. Only has effect when BlockSoft is not specified.
	ByteHard uint64 // New hard block limit specified in bytes. Only has effect when BlockHard is not specified.
}

type MemoryLimits struct {
	LimitInBytes uint64 //	Memory usage limit in bytes.
}

type CPULimits struct {
	LimitInShares uint64
}

// Resource limits.
//
// Please refer to the manual page of getrlimit for a description of the individual fields:
// http://www.kernel.org/doc/man-pages/online/pages/man2/getrlimit.2.html
type ResourceLimits struct {
	As         *uint64
	Core       *uint64
	Cpu        *uint64
	Data       *uint64
	Fsize      *uint64
	Locks      *uint64
	Memlock    *uint64
	Msgqueue   *uint64
	Nice       *uint64
	Nofile     *uint64
	Nproc      *uint64
	Rss        *uint64
	Rtprio     *uint64
	Sigpending *uint64
	Stack      *uint64
}