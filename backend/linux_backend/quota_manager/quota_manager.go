package quota_manager

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"syscall"

	"github.com/vito/garden/backend"
	"github.com/vito/garden/command_runner"
)

type QuotaManager interface {
	SetLimits(uid uint32, limits backend.DiskLimits) error
	GetLimits(uid uint32) (backend.DiskLimits, error)
	GetUsage(uid uint32) (backend.ContainerDiskStat, error)
}

type LinuxQuotaManager struct {
	mountPoint string

	rootPath string

	runner command_runner.CommandRunner
}

const QUOTA_BLOCK_SIZE = 1024

func New(containerDepotPath, rootPath string, runner command_runner.CommandRunner) (*LinuxQuotaManager, error) {
	dfOut := new(bytes.Buffer)

	df := &exec.Cmd{
		Path:   "df",
		Args:   []string{"-P", containerDepotPath},
		Stdout: dfOut,
	}

	err := runner.Run(df)
	if err != nil {
		return nil, err
	}

	dfOutputWords := strings.Split(string(dfOut.Bytes()), " ")
	mountPoint := strings.Trim(dfOutputWords[len(dfOutputWords)-1], "\n")

	return &LinuxQuotaManager{
		rootPath: rootPath,
		runner:   runner,

		mountPoint: mountPoint,
	}, nil
}

func (m *LinuxQuotaManager) SetLimits(uid uint32, limits backend.DiskLimits) error {
	if limits.ByteSoft != 0 {
		limits.BlockSoft = (limits.ByteSoft + QUOTA_BLOCK_SIZE - 1) / QUOTA_BLOCK_SIZE
	}

	if limits.ByteHard != 0 {
		limits.BlockHard = (limits.ByteHard + QUOTA_BLOCK_SIZE - 1) / QUOTA_BLOCK_SIZE
	}

	return m.runner.Run(
		&exec.Cmd{
			Path: "setquota",
			Args: []string{
				"-u",
				fmt.Sprintf("%d", uid),
				fmt.Sprintf("%d", limits.BlockSoft),
				fmt.Sprintf("%d", limits.BlockHard),
				fmt.Sprintf("%d", limits.InodeSoft),
				fmt.Sprintf("%d", limits.InodeHard),
				m.mountPoint,
			},
		},
	)
}

func (m *LinuxQuotaManager) GetLimits(uid uint32) (backend.DiskLimits, error) {
	repquota := &exec.Cmd{
		Path: path.Join(m.rootPath, "bin", "repquota"),
		Args: []string{m.mountPoint, fmt.Sprintf("%d", uid)},
	}

	limits := backend.DiskLimits{}

	repR, repW, err := os.Pipe()
	if err != nil {
		return limits, err
	}

	defer repR.Close()
	defer repW.Close()

	repquota.Stdout = repW

	err = m.runner.Start(repquota)
	if err != nil {
		return limits, err
	}

	var skip uint32

	_, err = fmt.Fscanf(
		repR,
		"%d %d %d %d %d %d %d %d",
		&skip,
		&skip,
		&limits.BlockSoft,
		&limits.BlockHard,
		&skip,
		&skip,
		&limits.InodeSoft,
		&limits.InodeHard,
	)

	return limits, err
}

func (m *LinuxQuotaManager) GetUsage(uid uint32) (backend.ContainerDiskStat, error) {
	repquota := &exec.Cmd{
		Path: path.Join(m.rootPath, "bin", "repquota"),
		Args: []string{m.mountPoint, fmt.Sprintf("%d", uid)},
	}

	usage := backend.ContainerDiskStat{}

	repR, repW, err := os.Pipe()
	if err != nil {
		return usage, err
	}

	defer repR.Close()
	defer repW.Close()

	repquota.Stdout = repW

	err = m.runner.Start(repquota)
	if err != nil {
		return usage, err
	}

	var skip uint32

	_, err = fmt.Fscanf(
		repR,
		"%d %d %d %d %d %d %d %d",
		&skip,
		&usage.BytesUsed,
		&skip,
		&skip,
		&skip,
		&usage.InodesUsed,
		&skip,
		&skip,
	)

	return usage, err
}

func findMountPoint(location string) (string, error) {
	isMount, err := isMountPoint(location)
	if err != nil {
		return "", err
	}

	if isMount {
		return location, nil
	}

	return findMountPoint(path.Dir(location))
}

func isMountPoint(location string) (bool, error) {
	stat, err := os.Stat(location)
	if err != nil {
		return false, err
	}

	parentStat, err := os.Stat(path.Dir(location))
	if err != nil {
		return false, err
	}

	sys := stat.Sys().(*syscall.Stat_t)
	parentSys := parentStat.Sys().(*syscall.Stat_t)

	if sys.Dev != parentSys.Dev {
		return true, nil
	}

	if sys.Ino == parentSys.Ino {
		return true, nil
	}

	return false, nil
}