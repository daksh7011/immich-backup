// internal/docker/docker.go
package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/docker/docker/api/types/container"
	dockerclient "github.com/docker/docker/client"
)

// Executor runs commands inside Docker containers and inspects container state.
type Executor interface {
	Exec(containerName, command string, args ...string) ([]byte, error)
	IsContainerRunning(containerName string) (bool, error)
}

// Client is a concrete Executor backed by the Docker Engine SDK.
type Client struct {
	cli *dockerclient.Client
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

// Exec runs command + args inside containerName and returns combined stdout+stderr.
func (c *Client) Exec(containerName, command string, args ...string) ([]byte, error) {
	ctx := context.Background()
	cmd := append([]string{command}, args...)

	execID, err := c.cli.ContainerExecCreate(ctx, containerName, container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return nil, fmt.Errorf("exec create in %s: %w", containerName, err)
	}

	resp, err := c.cli.ContainerExecAttach(ctx, execID.ID, container.ExecStartOptions{})
	if err != nil {
		return nil, fmt.Errorf("exec attach: %w", err)
	}
	defer resp.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, resp.Reader); err != nil {
		return nil, fmt.Errorf("read exec output: %w", err)
	}
	return buf.Bytes(), nil
}

// IsContainerRunning returns true if containerName exists and is in Running state.
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
	return info.State.Running, nil
}
