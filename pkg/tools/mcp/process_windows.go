//go:build windows

package mcp

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

func cancelProcess(cmd *exec.Cmd) error {
	// Attempt graceful termination by sending CTRL+BREAK to the process group
	_ = windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(cmd.Process.Pid))
	// Fallback to hard kill in case the event does not terminate the process
	return cmd.Process.Kill()
}
