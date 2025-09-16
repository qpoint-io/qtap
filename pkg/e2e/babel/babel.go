package babel

import (
	"github.com/testcontainers/testcontainers-go"
)

type HTTPRequestImage string

var (
	// Python
	HTTPRequestPython3_9_0_Alpine  HTTPRequestImage = "babel-python:3.9.0-alpine"
	HTTPRequestPython3_9_0_Debian  HTTPRequestImage = "babel-python:3.9.0-debian-bullseye"
	HTTPRequestPython3_10_0_Alpine HTTPRequestImage = "babel-python:3.10.0-alpine"
	HTTPRequestPython3_10_0_Debian HTTPRequestImage = "babel-python:3.10.0-debian-bullseye"
	HTTPRequestPython3_11_0_Alpine HTTPRequestImage = "babel-python:3.11.0-alpine"
	HTTPRequestPython3_11_0_Debian HTTPRequestImage = "babel-python:3.11.0-debian-bullseye"
	HTTPRequestPython3_12_0_Alpine HTTPRequestImage = "babel-python:3.12.0-alpine"
	HTTPRequestPython3_12_0_Debian HTTPRequestImage = "babel-python:3.12.0-debian-bullseye"
	// Ruby
	HTTPRequestRuby3_2_9_Alpine HTTPRequestImage = "babel-ruby:3.2.9-alpine"
	HTTPRequestRuby3_2_9_Debian HTTPRequestImage = "babel-ruby:3.2.9-debian-bullseye"
	HTTPRequestRuby3_3_9_Alpine HTTPRequestImage = "babel-ruby:3.3.9-alpine"
	HTTPRequestRuby3_3_9_Debian HTTPRequestImage = "babel-ruby:3.3.9-debian-bullseye"
	HTTPRequestRuby3_4_5_Alpine HTTPRequestImage = "babel-ruby:3.4.5-alpine"
	HTTPRequestRuby3_4_5_Debian HTTPRequestImage = "babel-ruby:3.4.5-debian-bullseye"
)

type ContainerResult struct {
	ExitCode int
	Logs     string
	Error    error
}

func (c *ContainerResult) Accept(log testcontainers.Log) {
	c.Logs += string(log.Content) + "\n"
}
