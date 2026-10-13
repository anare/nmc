package shell

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
)

func Default() string {
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}
	if _, err := exec.LookPath("/bin/bash"); err == nil {
		return "/bin/bash"
	}
	if _, err := exec.LookPath("/bin/sh"); err == nil {
		return "/bin/sh"
	}
	return "sh"
}

func Run(ctx context.Context, shell, directory, command string) ([]byte, error) {
	if shell == "" {
		shell = Default()
	}
	cmd := exec.CommandContext(ctx, shell, "-c", command)
	cmd.Dir = directory
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = killProcessGroup(cmd)
	output := NewBuffer(0)
	cmd.Stdout = output
	cmd.Stderr = output
	err := cmd.Run()
	return []byte(output.String()), err
}

func RunInteractive(ctx context.Context, shell, directory, command string) error {
	if shell == "" {
		shell = Default()
	}
	cmd := exec.CommandContext(ctx, shell, "-c", command)
	cmd.Dir = directory
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = killProcessGroup(cmd)
	if err := cmd.Run(); err != nil {
		return err
	}
	return ctx.Err()
}

func killProcessGroup(command *exec.Cmd) func() error {
	return func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
}

func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

type Job struct {
	mu      sync.Mutex
	command *exec.Cmd
	output  *Buffer
	done    chan struct{}
	err     error
	killed  bool
}

func Start(ctx context.Context, shell, directory, command string, output *Buffer) (*Job, error) {
	if shell == "" {
		shell = Default()
	}
	if output == nil {
		output = NewBuffer(0)
	}
	cmd := exec.Command(shell, "-c", command)
	cmd.Dir = directory
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout, cmd.Stderr = output, output
	job := &Job{command: cmd, output: output, done: make(chan struct{})}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	job.mu.Lock()
	killed := job.killed
	job.mu.Unlock()
	if killed && cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	go func() {
		job.err = cmd.Wait()
		close(job.done)
	}()
	go func() {
		select {
		case <-ctx.Done():
			_ = job.Kill()
		case <-job.done:
		}
	}()
	return job, nil
}

func (j *Job) Kill() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.command.Process == nil {
		j.killed = true
		return nil
	}
	return syscall.Kill(-j.command.Process.Pid, syscall.SIGKILL)
}

func (j *Job) Wait() error {
	<-j.done
	return j.err
}
