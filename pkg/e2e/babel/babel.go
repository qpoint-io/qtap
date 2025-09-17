package babel

import (
	"os"

	"github.com/testcontainers/testcontainers-go"
)

const officialImagesRegistry = "us-docker.pkg.dev/qpoint-edge/public/babel/"

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

func (i HTTPRequestImage) String() string {
	if os.Getenv("USE_LOCAL_IMAGES") == "true" {
		return string(i)
	}
	return officialImagesRegistry + string(i)
}

// AllPythonImages returns all available Python images
func AllPythonImages() []HTTPRequestImage {
	return []HTTPRequestImage{
		HTTPRequestPython3_9_0_Alpine,
		HTTPRequestPython3_9_0_Debian,
		HTTPRequestPython3_10_0_Alpine,
		HTTPRequestPython3_10_0_Debian,
		HTTPRequestPython3_11_0_Alpine,
		HTTPRequestPython3_11_0_Debian,
		HTTPRequestPython3_12_0_Alpine,
		HTTPRequestPython3_12_0_Debian,
	}
}

// AllRubyImages returns all available Ruby images
func AllRubyImages() []HTTPRequestImage {
	return []HTTPRequestImage{
		HTTPRequestRuby3_2_9_Alpine,
		HTTPRequestRuby3_2_9_Debian,
		HTTPRequestRuby3_3_9_Alpine,
		HTTPRequestRuby3_3_9_Debian,
		HTTPRequestRuby3_4_5_Alpine,
		HTTPRequestRuby3_4_5_Debian,
	}
}

// AllImages returns all available babel images
func AllImages() []HTTPRequestImage {
	images := make([]HTTPRequestImage, 0)
	images = append(images, AllPythonImages()...)
	images = append(images, AllRubyImages()...)
	return images
}

type ContainerResult struct {
	ExitCode int
	Logs     string
	Error    error
}

func (c *ContainerResult) Accept(log testcontainers.Log) {
	c.Logs += string(log.Content) + "\n"
}
