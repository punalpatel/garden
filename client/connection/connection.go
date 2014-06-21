package connection

import (
	"bytes"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"code.google.com/p/goprotobuf/proto"
	protocol "github.com/cloudfoundry-incubator/garden/protocol"
	"github.com/cloudfoundry-incubator/garden/transport"
	"github.com/cloudfoundry-incubator/garden/warden"
)

var ErrDisconnected = errors.New("disconnected")
var ErrInvalidMessage = errors.New("invalid message payload")

type Connection interface {
	Ping() error

	Capacity() (warden.Capacity, error)

	Create(spec warden.ContainerSpec) (string, error)
	List(properties warden.Properties) ([]string, error)
	Destroy(handle string) error

	Stop(handle string, background, kill bool) error

	Info(handle string) (warden.ContainerInfo, error)

	StreamIn(handle string, dstPath string, reader io.Reader) error
	StreamOut(handle string, srcPath string) (io.ReadCloser, error)

	LimitBandwidth(handle string, limits warden.BandwidthLimits) (warden.BandwidthLimits, error)
	LimitCPU(handle string, limits warden.CPULimits) (warden.CPULimits, error)
	LimitDisk(handle string, limits warden.DiskLimits) (warden.DiskLimits, error)
	LimitMemory(handle string, limit warden.MemoryLimits) (warden.MemoryLimits, error)

	CurrentBandwidthLimits(handle string) (warden.BandwidthLimits, error)
	CurrentCPULimits(handle string) (warden.CPULimits, error)
	CurrentDiskLimits(handle string) (warden.DiskLimits, error)
	CurrentMemoryLimits(handle string) (warden.MemoryLimits, error)

	Run(handle string, spec warden.ProcessSpec) (uint32, <-chan warden.ProcessStream, error)
	Attach(handle string, processID uint32) (<-chan warden.ProcessStream, error)

	NetIn(handle string, hostPort, containerPort uint32) (uint32, uint32, error)
	NetOut(handle string, network string, port uint32) error
}

type connection struct {
	httpClient        *http.Client
	noKeepaliveClient *http.Client
}

type WardenError struct {
	Message   string
	Data      string
	Backtrace []string
}

func (e *WardenError) Error() string {
	return e.Message
}

func New(network, address string) Connection {
	dialer := func(string, string) (net.Conn, error) {
		return net.DialTimeout(network, address, time.Second)
	}

	return &connection{
		httpClient: &http.Client{
			Transport: &http.Transport{
				Dial: dialer,
			},
		},
		noKeepaliveClient: &http.Client{
			Transport: &http.Transport{
				Dial:              dialer,
				DisableKeepAlives: true,
			},
		},
	}
}

func (c *connection) Ping() error {
	return c.post("/ping", &protocol.PingRequest{}, &protocol.PingResponse{})
}

func (c *connection) Capacity() (warden.Capacity, error) {
	res := &protocol.CapacityResponse{}

	err := c.post("/capacity", &protocol.CapacityRequest{}, res)
	if err != nil {
		return warden.Capacity{}, err
	}

	return warden.Capacity{
		MemoryInBytes: res.GetMemoryInBytes(),
		DiskInBytes:   res.GetDiskInBytes(),
		MaxContainers: res.GetMaxContainers(),
	}, nil
}

func (c *connection) Create(spec warden.ContainerSpec) (string, error) {
	req := &protocol.CreateRequest{}

	if spec.Handle != "" {
		req.Handle = proto.String(spec.Handle)
	}

	if spec.RootFSPath != "" {
		req.Rootfs = proto.String(spec.RootFSPath)
	}

	if spec.GraceTime != 0 {
		req.GraceTime = proto.Uint32(uint32(spec.GraceTime.Seconds()))
	}

	if spec.Network != "" {
		req.Network = proto.String(spec.Network)
	}

	for _, bm := range spec.BindMounts {
		var mode protocol.CreateRequest_BindMount_Mode
		var origin protocol.CreateRequest_BindMount_Origin

		switch bm.Mode {
		case warden.BindMountModeRO:
			mode = protocol.CreateRequest_BindMount_RO
		case warden.BindMountModeRW:
			mode = protocol.CreateRequest_BindMount_RW
		}

		switch bm.Origin {
		case warden.BindMountOriginHost:
			origin = protocol.CreateRequest_BindMount_Host
		case warden.BindMountOriginContainer:
			origin = protocol.CreateRequest_BindMount_Container
		}

		req.BindMounts = append(req.BindMounts, &protocol.CreateRequest_BindMount{
			SrcPath: proto.String(bm.SrcPath),
			DstPath: proto.String(bm.DstPath),
			Mode:    &mode,
			Origin:  &origin,
		})
	}

	props := []*protocol.Property{}
	for key, val := range spec.Properties {
		props = append(props, &protocol.Property{
			Key:   proto.String(key),
			Value: proto.String(val),
		})
	}

	req.Properties = props

	res := &protocol.CreateResponse{}
	err := c.post("/create", req, res)
	if err != nil {
		return "", err
	}

	return res.GetHandle(), nil
}

func (c *connection) Stop(handle string, background, kill bool) error {
	return c.post(
		"/stop",
		&protocol.StopRequest{
			Handle:     proto.String(handle),
			Background: proto.Bool(background),
			Kill:       proto.Bool(kill),
		},
		&protocol.StopResponse{},
	)
}

func (c *connection) Destroy(handle string) error {
	return c.post(
		"/destroy",
		&protocol.DestroyRequest{Handle: proto.String(handle)},
		&protocol.DestroyResponse{},
	)
}

func (c *connection) Run(handle string, spec warden.ProcessSpec) (uint32, <-chan warden.ProcessStream, error) {
	respBody, err := c.postWithProcessPayloadResponse(
		"/run",
		&protocol.RunRequest{
			Handle:     proto.String(handle),
			Script:     proto.String(spec.Script),
			Privileged: proto.Bool(spec.Privileged),
			Rlimits: &protocol.ResourceLimits{
				As:         spec.Limits.As,
				Core:       spec.Limits.Core,
				Cpu:        spec.Limits.Cpu,
				Data:       spec.Limits.Data,
				Fsize:      spec.Limits.Fsize,
				Locks:      spec.Limits.Locks,
				Memlock:    spec.Limits.Memlock,
				Msgqueue:   spec.Limits.Msgqueue,
				Nice:       spec.Limits.Nice,
				Nofile:     spec.Limits.Nofile,
				Nproc:      spec.Limits.Nproc,
				Rss:        spec.Limits.Rss,
				Rtprio:     spec.Limits.Rtprio,
				Sigpending: spec.Limits.Sigpending,
				Stack:      spec.Limits.Stack,
			},
			Env: convertEnvironmentVariables(spec.EnvironmentVariables),
		},
	)
	if err != nil {
		return 0, nil, err
	}

	firstResponse := &protocol.ProcessPayload{}
	err = transport.ReadMessage(respBody, firstResponse)
	if err != nil {
		return 0, nil, err
	}

	responses := make(chan warden.ProcessStream)

	go c.streamPayloads(respBody, responses)

	return firstResponse.GetProcessId(), responses, nil
}

func (c *connection) Attach(handle string, processID uint32) (<-chan warden.ProcessStream, error) {
	respBody, err := c.postWithProcessPayloadResponse(
		"/attach",
		&protocol.AttachRequest{
			Handle:    proto.String(handle),
			ProcessId: proto.Uint32(processID),
		},
	)

	if err != nil {
		return nil, err
	}

	responses := make(chan warden.ProcessStream)

	go c.streamPayloads(respBody, responses)

	return responses, nil
}

func (c *connection) NetIn(handle string, hostPort, containerPort uint32) (uint32, uint32, error) {
	res := &protocol.NetInResponse{}
	err := c.post(
		"/net_in",
		&protocol.NetInRequest{
			Handle:        proto.String(handle),
			HostPort:      proto.Uint32(hostPort),
			ContainerPort: proto.Uint32(containerPort),
		},
		res,
	)

	if err != nil {
		return 0, 0, err
	}

	return res.GetHostPort(), res.GetContainerPort(), nil
}

func (c *connection) NetOut(handle string, network string, port uint32) error {
	return c.post(
		"/net_out",
		&protocol.NetOutRequest{
			Handle:  proto.String(handle),
			Network: proto.String(network),
			Port:    proto.Uint32(port),
		},
		&protocol.NetOutResponse{},
	)
}

func (c *connection) LimitBandwidth(handle string, limits warden.BandwidthLimits) (warden.BandwidthLimits, error) {
	res := &protocol.LimitBandwidthResponse{}

	err := c.post(
		"/limit_bandwidth",
		&protocol.LimitBandwidthRequest{
			Handle: proto.String(handle),
			Rate:   proto.Uint64(limits.RateInBytesPerSecond),
			Burst:  proto.Uint64(limits.BurstRateInBytesPerSecond),
		},
		res,
	)

	if err != nil {
		return warden.BandwidthLimits{}, err
	}

	return warden.BandwidthLimits{
		RateInBytesPerSecond:      res.GetRate(),
		BurstRateInBytesPerSecond: res.GetBurst(),
	}, nil
}

func (c *connection) CurrentBandwidthLimits(handle string) (warden.BandwidthLimits, error) {
	res := &protocol.LimitBandwidthResponse{}

	err := c.post(
		"/limit_bandwidth",
		&protocol.LimitBandwidthRequest{
			Handle: proto.String(handle),
		},
		res,
	)

	if err != nil {
		return warden.BandwidthLimits{}, err
	}

	return warden.BandwidthLimits{
		RateInBytesPerSecond:      res.GetRate(),
		BurstRateInBytesPerSecond: res.GetBurst(),
	}, nil
}

func (c *connection) LimitCPU(handle string, limits warden.CPULimits) (warden.CPULimits, error) {
	res := &protocol.LimitCpuResponse{}

	err := c.post(
		"/limit_cpu",
		&protocol.LimitCpuRequest{
			Handle:        proto.String(handle),
			LimitInShares: proto.Uint64(limits.LimitInShares),
		},
		res,
	)

	if err != nil {
		return warden.CPULimits{}, err
	}

	return warden.CPULimits{
		LimitInShares: res.GetLimitInShares(),
	}, nil
}

func (c *connection) CurrentCPULimits(handle string) (warden.CPULimits, error) {
	res := &protocol.LimitCpuResponse{}

	err := c.post(
		"/limit_cpu",
		&protocol.LimitCpuRequest{
			Handle: proto.String(handle),
		},
		res,
	)

	if err != nil {
		return warden.CPULimits{}, err
	}

	return warden.CPULimits{
		LimitInShares: res.GetLimitInShares(),
	}, nil
}

func (c *connection) LimitDisk(handle string, limits warden.DiskLimits) (warden.DiskLimits, error) {
	res := &protocol.LimitDiskResponse{}

	err := c.post(
		"/limit_disk",
		&protocol.LimitDiskRequest{
			Handle: proto.String(handle),

			BlockSoft: proto.Uint64(limits.BlockSoft),
			BlockHard: proto.Uint64(limits.BlockHard),

			InodeSoft: proto.Uint64(limits.InodeSoft),
			InodeHard: proto.Uint64(limits.InodeHard),

			ByteSoft: proto.Uint64(limits.ByteSoft),
			ByteHard: proto.Uint64(limits.ByteHard),
		},
		res,
	)

	if err != nil {
		return warden.DiskLimits{}, err
	}

	return warden.DiskLimits{
		BlockSoft: res.GetBlockSoft(),
		BlockHard: res.GetBlockHard(),

		InodeSoft: res.GetInodeSoft(),
		InodeHard: res.GetInodeHard(),

		ByteSoft: res.GetByteSoft(),
		ByteHard: res.GetByteHard(),
	}, nil
}

func (c *connection) CurrentDiskLimits(handle string) (warden.DiskLimits, error) {
	res := &protocol.LimitDiskResponse{}

	err := c.post(
		"/limit_disk",
		&protocol.LimitDiskRequest{
			Handle: proto.String(handle),
		},
		res,
	)

	if err != nil {
		return warden.DiskLimits{}, err
	}

	return warden.DiskLimits{
		BlockSoft: res.GetBlockSoft(),
		BlockHard: res.GetBlockHard(),

		InodeSoft: res.GetInodeSoft(),
		InodeHard: res.GetInodeHard(),

		ByteSoft: res.GetByteSoft(),
		ByteHard: res.GetByteHard(),
	}, nil
}

func (c *connection) LimitMemory(handle string, limits warden.MemoryLimits) (warden.MemoryLimits, error) {
	res := &protocol.LimitMemoryResponse{}

	err := c.post(
		"/limit_memory",
		&protocol.LimitMemoryRequest{
			Handle:       proto.String(handle),
			LimitInBytes: proto.Uint64(limits.LimitInBytes),
		},
		res,
	)

	if err != nil {
		return warden.MemoryLimits{}, err
	}

	return warden.MemoryLimits{
		LimitInBytes: res.GetLimitInBytes(),
	}, nil
}

func (c *connection) CurrentMemoryLimits(handle string) (warden.MemoryLimits, error) {
	res := &protocol.LimitMemoryResponse{}

	err := c.post(
		"/limit_memory",
		&protocol.LimitMemoryRequest{
			Handle: proto.String(handle),
		},
		res,
	)

	if err != nil {
		return warden.MemoryLimits{}, err
	}

	return warden.MemoryLimits{
		LimitInBytes: res.GetLimitInBytes(),
	}, nil
}

func (c *connection) StreamIn(handle string, dstPath string, reader io.Reader) error {
	return c.postWithStreamedRequest(
		&url.URL{
			Scheme: "http",
			Host:   "warden",
			Path:   "/stream_in",
			RawQuery: url.Values{
				"handle":      []string{handle},
				"destination": []string{dstPath},
			}.Encode(),
		},
		reader,
	)
}

func (c *connection) StreamOut(handle string, srcPath string) (io.ReadCloser, error) {
	return c.postWithStreamedResponse(
		&url.URL{
			Scheme: "http",
			Host:   "warden",
			Path:   "/stream_out",
			RawQuery: url.Values{
				"handle": []string{handle},
				"source": []string{srcPath},
			}.Encode(),
		},
	)
}

func (c *connection) List(filterProperties warden.Properties) ([]string, error) {
	props := []*protocol.Property{}
	for key, val := range filterProperties {
		props = append(props, &protocol.Property{
			Key:   proto.String(key),
			Value: proto.String(val),
		})
	}

	req := &protocol.ListRequest{Properties: props}
	res := &protocol.ListResponse{}

	err := c.post("/list", req, res)
	if err != nil {
		return nil, err
	}

	return res.GetHandles(), nil
}

func (c *connection) Info(handle string) (warden.ContainerInfo, error) {
	req := &protocol.InfoRequest{Handle: proto.String(handle)}
	res := &protocol.InfoResponse{}

	err := c.post("/info", req, res)
	if err != nil {
		return warden.ContainerInfo{}, err
	}

	processIDs := []uint32{}
	for _, pid := range res.GetProcessIds() {
		processIDs = append(processIDs, uint32(pid))
	}

	properties := warden.Properties{}
	for _, prop := range res.GetProperties() {
		properties[prop.GetKey()] = prop.GetValue()
	}

	mappedPorts := []warden.PortMapping{}
	for _, mapping := range res.GetMappedPorts() {
		mappedPorts = append(mappedPorts, warden.PortMapping{
			HostPort:      mapping.GetHostPort(),
			ContainerPort: mapping.GetContainerPort(),
		})
	}

	bandwidthStat := res.GetBandwidthStat()
	cpuStat := res.GetCpuStat()
	diskStat := res.GetDiskStat()
	memoryStat := res.GetMemoryStat()

	return warden.ContainerInfo{
		State:  res.GetState(),
		Events: res.GetEvents(),

		HostIP:      res.GetHostIp(),
		ContainerIP: res.GetContainerIp(),

		ContainerPath: res.GetContainerPath(),

		ProcessIDs: processIDs,

		Properties: properties,

		BandwidthStat: warden.ContainerBandwidthStat{
			InRate:   bandwidthStat.GetInRate(),
			InBurst:  bandwidthStat.GetInBurst(),
			OutRate:  bandwidthStat.GetOutRate(),
			OutBurst: bandwidthStat.GetOutBurst(),
		},

		CPUStat: warden.ContainerCPUStat{
			Usage:  cpuStat.GetUsage(),
			User:   cpuStat.GetUser(),
			System: cpuStat.GetSystem(),
		},

		DiskStat: warden.ContainerDiskStat{
			BytesUsed:  diskStat.GetBytesUsed(),
			InodesUsed: diskStat.GetInodesUsed(),
		},

		MemoryStat: warden.ContainerMemoryStat{
			Cache:                   memoryStat.GetCache(),
			Rss:                     memoryStat.GetRss(),
			MappedFile:              memoryStat.GetMappedFile(),
			Pgpgin:                  memoryStat.GetPgpgin(),
			Pgpgout:                 memoryStat.GetPgpgout(),
			Swap:                    memoryStat.GetSwap(),
			Pgfault:                 memoryStat.GetPgfault(),
			Pgmajfault:              memoryStat.GetPgmajfault(),
			InactiveAnon:            memoryStat.GetInactiveAnon(),
			ActiveAnon:              memoryStat.GetActiveAnon(),
			InactiveFile:            memoryStat.GetInactiveFile(),
			ActiveFile:              memoryStat.GetActiveFile(),
			Unevictable:             memoryStat.GetUnevictable(),
			HierarchicalMemoryLimit: memoryStat.GetHierarchicalMemoryLimit(),
			HierarchicalMemswLimit:  memoryStat.GetHierarchicalMemswLimit(),
			TotalCache:              memoryStat.GetTotalCache(),
			TotalRss:                memoryStat.GetTotalRss(),
			TotalMappedFile:         memoryStat.GetTotalMappedFile(),
			TotalPgpgin:             memoryStat.GetTotalPgpgin(),
			TotalPgpgout:            memoryStat.GetTotalPgpgout(),
			TotalSwap:               memoryStat.GetTotalSwap(),
			TotalPgfault:            memoryStat.GetTotalPgfault(),
			TotalPgmajfault:         memoryStat.GetTotalPgmajfault(),
			TotalInactiveAnon:       memoryStat.GetTotalInactiveAnon(),
			TotalActiveAnon:         memoryStat.GetTotalActiveAnon(),
			TotalInactiveFile:       memoryStat.GetTotalInactiveFile(),
			TotalActiveFile:         memoryStat.GetTotalActiveFile(),
			TotalUnevictable:        memoryStat.GetTotalUnevictable(),
		},

		MappedPorts: mappedPorts,
	}, nil
}

func convertEnvironmentVariables(environmentVariables []warden.EnvironmentVariable) []*protocol.EnvironmentVariable {
	convertedEnvironmentVariables := []*protocol.EnvironmentVariable{}

	for _, env := range environmentVariables {
		convertedEnvironmentVariable := &protocol.EnvironmentVariable{
			Key:   proto.String(env.Key),
			Value: proto.String(env.Value),
		}

		convertedEnvironmentVariables = append(
			convertedEnvironmentVariables,
			convertedEnvironmentVariable,
		)
	}

	return convertedEnvironmentVariables
}

func (c *connection) streamPayloads(reader io.ReadCloser, stream chan<- warden.ProcessStream) {
	for {
		payload := &protocol.ProcessPayload{}

		err := transport.ReadMessage(reader, payload)
		if err != nil {
			break
		}

		if payload.ExitStatus != nil {
			stream <- warden.ProcessStream{
				ExitStatus: payload.ExitStatus,
			}

			break
		} else {
			var source warden.ProcessStreamSource

			switch payload.GetSource() {
			case protocol.ProcessPayload_stdin:
				source = warden.ProcessStreamSourceStdin
			case protocol.ProcessPayload_stdout:
				source = warden.ProcessStreamSourceStdout
			case protocol.ProcessPayload_stderr:
				source = warden.ProcessStreamSourceStderr
			}

			stream <- warden.ProcessStream{
				Source: source,
				Data:   []byte(payload.GetData()),
			}
		}
	}

	close(stream)
	reader.Close()
}

func (c *connection) post(route string, req, res proto.Message) error {
	reqBody := new(bytes.Buffer)

	err := transport.WriteMessage(reqBody, req)
	if err != nil {
		return err
	}

	httpResp, err := c.httpClient.Post("http://warden"+route, "application/octet-stream", reqBody)
	if err != nil {
		return err
	}

	defer httpResp.Body.Close()

	if httpResp.StatusCode < 200 || httpResp.StatusCode > 299 {
		return errors.New(httpResp.Status)
	}

	return transport.ReadMessage(httpResp.Body, res)
}

func (c *connection) postWithStreamedResponse(u *url.URL) (io.ReadCloser, error) {
	httpResp, err := c.noKeepaliveClient.Post(u.String(), "application/octet-stream", nil)
	if err != nil {
		return nil, err
	}

	if httpResp.StatusCode != http.StatusOK {
		httpResp.Body.Close()
		return nil, errors.New(httpResp.Status)
	}

	return httpResp.Body, nil
}

func (c *connection) postWithStreamedRequest(u *url.URL, reader io.Reader) error {
	resp, err := c.noKeepaliveClient.Post(u.String(), "application/octet-stream", reader)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return errors.New(resp.Status)
	}

	return nil
}

func (c *connection) postWithProcessPayloadResponse(route string, req proto.Message) (io.ReadCloser, error) {
	reqBody := new(bytes.Buffer)

	err := transport.WriteMessage(reqBody, req)
	if err != nil {
		return nil, err
	}

	httpResp, err := c.httpClient.Post("http://warden"+route, "application/octet-stream", reqBody)
	if err != nil {
		return nil, err
	}

	if httpResp.StatusCode != http.StatusOK {
		httpResp.Body.Close()
		return nil, errors.New(httpResp.Status)
	}

	return httpResp.Body, nil
}