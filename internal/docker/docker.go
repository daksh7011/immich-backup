// internal/docker/docker.go
package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/docker/docker/api/types/container"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// Executor runs commands inside Docker containers and inspects container state.
type Executor interface {
	Exec(containerName, command string, args ...string) ([]byte, error)
	// ExecStream runs command + args inside containerName and copies its
	// stdout to w as it arrives, so a large output (a database dump) is never
	// held in memory. Cancelling ctx aborts the read.
	ExecStream(ctx context.Context, w io.Writer, containerName, command string, args ...string) error
	IsContainerRunning(containerName string) (bool, error)
}

// Client is a concrete Executor backed by the Docker Engine SDK.
type Client struct {
	cli *dockerclient.Client
}

// Host returns the Docker endpoint NewClient connects to: DOCKER_HOST when
// set, otherwise the platform default socket. Used in error messages so a
// wrong or missing DOCKER_HOST (rootless Docker, Colima) is visible.
func Host() string {
	if h := os.Getenv("DOCKER_HOST"); h != "" {
		return h
	}
	return dockerclient.DefaultDockerHost
}

// NewClient creates a Client connected to the Docker socket via environment
// variables (DOCKER_HOST) or the default Unix socket.
func NewClient() (*Client, error) {
	cli, err := dockerclient.NewClientWithOpts(
		dockerclient.FromEnv,
		dockerclient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("connect to Docker socket: %w", err)
	}
	return &Client{cli: cli}, nil
}

// Close releases the underlying Docker client connection.
func (c *Client) Close() { _ = c.cli.Close() }

// Exec runs command + args inside containerName and returns its stdout.
func (c *Client) Exec(containerName, command string, args ...string) ([]byte, error) {
	var stdout bytes.Buffer
	if err := c.ExecStream(context.Background(), &stdout, containerName, command, args...); err != nil {
		return nil, err
	}
	return stdout.Bytes(), nil
}

// maxExecStderr caps the stderr kept for an exec's error message.
const maxExecStderr = 64 * 1024

// ExecStream runs command + args inside containerName and copies its stdout
// to w. A non-zero exit returns an error carrying the command's stderr.
func (c *Client) ExecStream(ctx context.Context, w io.Writer, containerName, command string, args ...string) error {
	cmd := append([]string{command}, args...)

	execID, err := c.cli.ContainerExecCreate(ctx, containerName, container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return fmt.Errorf("exec create in %s: %w", containerName, err)
	}

	resp, err := c.cli.ContainerExecAttach(ctx, execID.ID, container.ExecStartOptions{})
	if err != nil {
		return fmt.Errorf("exec attach: %w", err)
	}
	defer resp.Close()
	// The attached connection does not watch ctx; closing it on cancel
	// unblocks the copy below.
	stop := context.AfterFunc(ctx, resp.Close)
	defer stop()

	stderr := &capWriter{max: maxExecStderr}
	if _, err := stdcopy.StdCopy(w, stderr, resp.Reader); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("read exec output: %w", err)
	}

	inspect, err := c.cli.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return fmt.Errorf("exec inspect: %w", err)
	}
	if inspect.ExitCode != 0 {
		return fmt.Errorf("exec exited with code %d: %s", inspect.ExitCode, stderr.buf.String())
	}
	return nil
}

// capWriter keeps the first max bytes written to it and drops the rest
// without failing the write.
type capWriter struct {
	buf bytes.Buffer
	max int
}

func (w *capWriter) Write(p []byte) (int, error) {
	if room := w.max - w.buf.Len(); room > 0 {
		w.buf.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

// IsContainerRunning returns true if containerName exists and is running.
// Returns false (no error) if the container does not exist.
func (c *Client) IsContainerRunning(containerName string) (bool, error) {
	ctx := context.Background()
	info, err := c.cli.ContainerInspect(ctx, containerName)
	if err != nil {
		if dockerclient.IsErrNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("inspect %s: %w", containerName, err)
	}
	return isRunning(info.State), nil
}

// isRunning reports whether a container can run commands. Docker keeps
// Running true while a container waits to be restarted or is paused, and
// neither can exec, so both count as not running.
func isRunning(s *container.State) bool {
	return s != nil && s.Running && !s.Restarting && !s.Paused
}
