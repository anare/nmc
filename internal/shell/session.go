package shell

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
)

type Session struct {
	command  *exec.Cmd
	terminal *os.File
	output   *Buffer
	done     chan struct{}
	err      error
}

func StartSession(ctx context.Context, shell, directory string, notify func()) (*Session, error) {
	if shell == "" {
		shell = Default()
	}
	command := exec.Command(shell, "-i")
	command.Dir = directory
	terminal, err := pty.Start(command)
	if err != nil {
		return nil, err
	}
	session := &Session{command: command, terminal: terminal, output: NewBuffer(1 << 20), done: make(chan struct{})}
	go func() {
		_, _ = io.Copy(sessionWriter{buffer: session.output, notify: notify}, terminal)
	}()
	go func() {
		session.err = command.Wait()
		close(session.done)
		_ = terminal.Close()
		if notify != nil {
			notify()
		}
	}()
	go func() {
		select {
		case <-ctx.Done():
			_ = session.Close()
		case <-session.done:
		}
	}()
	return session, nil
}

type sessionWriter struct {
	buffer *Buffer
	notify func()
}

func (w sessionWriter) Write(data []byte) (int, error) {
	n, err := w.buffer.Write(data)
	if w.notify != nil {
		w.notify()
	}
	return n, err
}

func (s *Session) Write(data []byte) (int, error) {
	if s == nil || s.terminal == nil {
		return 0, errors.New("shell session is closed")
	}
	return s.terminal.Write(data)
}

func (s *Session) Output() []string {
	if s == nil {
		return nil
	}
	return s.output.Lines()
}

func (s *Session) Resize(width, height int) error {
	if s == nil || s.terminal == nil {
		return errors.New("shell session is closed")
	}
	return pty.Setsize(s.terminal, &pty.Winsize{Cols: uint16(width), Rows: uint16(height)})
}

func (s *Session) Wait() error {
	<-s.done
	return s.err
}

func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	select {
	case <-s.done:
		return nil
	default:
	}
	if s.command.Process == nil {
		return nil
	}
	return syscall.Kill(-s.command.Process.Pid, syscall.SIGTERM)
}
