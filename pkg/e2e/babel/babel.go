package babel

import (
	"github.com/testcontainers/testcontainers-go"
)

type HTTPRequestImage string

var (
	HTTPRequestGo1_22_0_Alpine HTTPRequestImage = "babel-go:1.22.0-alpine"
)

type ContainerResult struct {
	ExitCode int
	Logs     string
	Error    error
}

func (c *ContainerResult) Accept(log testcontainers.Log) {
	c.Logs += string(log.Content) + "\n"
}
