package babel

import (
	"os"
	"strings"

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
	// PHP
	HTTPRequestPHP8_1_0_Alpine HTTPRequestImage = "babel-php:8.1-alpine"
	HTTPRequestPHP8_1_0_Debian HTTPRequestImage = "babel-php:8.1-debian-bullseye"
	HTTPRequestPHP8_2_0_Alpine HTTPRequestImage = "babel-php:8.2-alpine"
	HTTPRequestPHP8_2_0_Debian HTTPRequestImage = "babel-php:8.2-debian-bullseye"
	HTTPRequestPHP8_3_0_Alpine HTTPRequestImage = "babel-php:8.3-alpine"
	HTTPRequestPHP8_3_0_Debian HTTPRequestImage = "babel-php:8.3-debian-bullseye"
)

func (i HTTPRequestImage) String() string {
	if os.Getenv("USE_LOCAL_IMAGES") == "true" {
		return string(i)
	}
	return officialImagesRegistry + string(i)
}

// TestName returns a name for the image that can be used for consistent
// Go test names and reporting. For example:
// babel-python:3.9.0-alpine -> python_3.9.0_alpine
func (i HTTPRequestImage) TestName() string {
	name, _ := strings.CutPrefix(string(i), "babel-")
	name = strings.ReplaceAll(name, ":", "/")
	name = strings.ReplaceAll(name, "-", "/")
	name = strings.ReplaceAll(name, ".", "_")
	return name
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

// AllPHPImages returns all available PHP images
func AllPHPImages() []HTTPRequestImage {
	return []HTTPRequestImage{
		HTTPRequestPHP8_1_0_Alpine,
		HTTPRequestPHP8_1_0_Debian,
		HTTPRequestPHP8_2_0_Alpine,
		HTTPRequestPHP8_2_0_Debian,
		HTTPRequestPHP8_3_0_Alpine,
		HTTPRequestPHP8_3_0_Debian,
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
