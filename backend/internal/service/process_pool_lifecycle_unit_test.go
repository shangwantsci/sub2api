//go:build unit

package service

import (
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestForwarder 启动一个真实子进程并挂上 superviseForwarder。
// 子进程退出但未 Wait 时它是僵尸进程，Signal(0) 仍会成功——这正是旧实现
// 无法感知「forwarder 已死」的原因，所以这里用真实的 Wait 路径验证。
func newTestForwarder(t *testing.T, cmd *exec.Cmd) *forwarderProcess {
	t.Helper()
	require.NoError(t, cmd.Start())
	fwd := &forwarderProcess{accountID: 1, cmd: cmd, done: make(chan struct{})}
	go superviseForwarder(fwd)
	t.Cleanup(func() { fwd.stop(2 * time.Second) })
	return fwd
}

func TestForwarderProcess_NotAliveUntilExitIsReaped(t *testing.T) {
	fwd := newTestForwarder(t, exec.Command("sh", "-c", "exit 0"))

	select {
	case <-fwd.done:
	case <-time.After(5 * time.Second):
		t.Fatal("superviseForwarder did not reap the exited process")
	}

	assert.False(t, fwd.alive(), "an exited forwarder must not be reported alive")
}

func TestForwarderProcess_AliveWhileRunning(t *testing.T) {
	fwd := newTestForwarder(t, exec.Command("sleep", "30"))

	assert.True(t, fwd.alive(), "a running forwarder must be reported alive")

	fwd.stop(2 * time.Second)
	assert.False(t, fwd.alive(), "stop must wait for the process to be reaped")
}
