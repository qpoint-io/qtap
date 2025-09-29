package babel

import (
	"os"
	"strings"
)

const officialImagesRegistry = "us-docker.pkg.dev/qpoint-edge/public/babel/"

type HTTPRequestImage string

var (
	// Python
	HTTPRequestPython3_9_0_Alpine    HTTPRequestImage = "babel-python:3.9.0-alpine"
	HTTPRequestPython3_9_0_Bullseye  HTTPRequestImage = "babel-python:3.9.0-debian-bullseye"
	HTTPRequestPython3_10_0_Alpine   HTTPRequestImage = "babel-python:3.10.0-alpine"
	HTTPRequestPython3_10_0_Bullseye HTTPRequestImage = "babel-python:3.10.0-debian-bullseye"
	HTTPRequestPython3_11_0_Alpine   HTTPRequestImage = "babel-python:3.11.0-alpine"
	HTTPRequestPython3_11_0_Bullseye HTTPRequestImage = "babel-python:3.11.0-debian-bullseye"
	HTTPRequestPython3_12_0_Alpine   HTTPRequestImage = "babel-python:3.12.0-alpine"
	HTTPRequestPython3_12_0_Bullseye HTTPRequestImage = "babel-python:3.12.0-debian-bullseye"
	// Ruby
	HTTPRequestRuby3_2_9_Alpine   HTTPRequestImage = "babel-ruby:3.2.9-alpine"
	HTTPRequestRuby3_2_9_Bullseye HTTPRequestImage = "babel-ruby:3.2.9-debian-bullseye"
	HTTPRequestRuby3_3_9_Alpine   HTTPRequestImage = "babel-ruby:3.3.9-alpine"
	HTTPRequestRuby3_3_9_Bullseye HTTPRequestImage = "babel-ruby:3.3.9-debian-bullseye"
	HTTPRequestRuby3_4_5_Alpine   HTTPRequestImage = "babel-ruby:3.4.5-alpine"
	HTTPRequestRuby3_4_5_Bullseye HTTPRequestImage = "babel-ruby:3.4.5-debian-bullseye"
	// PHP
	HTTPRequestPHP8_1_0_Alpine   HTTPRequestImage = "babel-php:8.1-alpine"
	HTTPRequestPHP8_1_0_Bullseye HTTPRequestImage = "babel-php:8.1-debian-bullseye"
	HTTPRequestPHP8_2_0_Alpine   HTTPRequestImage = "babel-php:8.2-alpine"
	HTTPRequestPHP8_2_0_Bullseye HTTPRequestImage = "babel-php:8.2-debian-bullseye"
	HTTPRequestPHP8_3_0_Alpine   HTTPRequestImage = "babel-php:8.3-alpine"
	HTTPRequestPHP8_3_0_Bullseye HTTPRequestImage = "babel-php:8.3-debian-bullseye"
	// Java
	HTTPRequestJava11_Corretto    HTTPRequestImage = "babel-java:11-corretto-alpine"
	HTTPRequestJava17_Corretto    HTTPRequestImage = "babel-java:17-corretto-alpine"
	HTTPRequestJava21_Corretto    HTTPRequestImage = "babel-java:21-corretto-alpine"
	HTTPRequestJava21_Temurin_JDK HTTPRequestImage = "babel-java:21-temurin-jdk"
	HTTPRequestJava21_Temurin_JRE HTTPRequestImage = "babel-java:21-temurin-jre"
	// NodeJS
	HTTPRequestNodeJS18_20_0_Alpine   HTTPRequestImage = "babel-nodejs:18.20.0-alpine"
	HTTPRequestNodeJS18_20_0_Bullseye HTTPRequestImage = "babel-nodejs:18.20.0-debian-bullseye"
	HTTPRequestNodeJS19_0_0_Alpine    HTTPRequestImage = "babel-nodejs:19.0.0-alpine"
	HTTPRequestNodeJS19_0_0_Bullseye  HTTPRequestImage = "babel-nodejs:19.0.0-debian-bullseye"
	HTTPRequestNodeJS22_16_0_Alpine   HTTPRequestImage = "babel-nodejs:22.16.0-alpine"
	HTTPRequestNodeJS22_16_0_Bullseye HTTPRequestImage = "babel-nodejs:22.16.0-debian-bullseye"
	HTTPRequestNodeJS23_0_0_Alpine    HTTPRequestImage = "babel-nodejs:23.0.0-alpine"
	HTTPRequestNodeJS23_0_0_Bullseye  HTTPRequestImage = "babel-nodejs:23.0.0-debian-bullseye"
	HTTPRequestNodeJS24_5_0_Alpine    HTTPRequestImage = "babel-nodejs:24.5.0-alpine"
	HTTPRequestNodeJS24_5_0_Bullseye  HTTPRequestImage = "babel-nodejs:24.5.0-debian-bullseye"
	// Go
	HTTPRequestGo1_14_0_Alpine   HTTPRequestImage = "babel-go:1.14.0-alpine"
	HTTPRequestGo1_14_0_Buster   HTTPRequestImage = "babel-go:1.14.0-debian-buster"
	HTTPRequestGo1_18_0_Alpine   HTTPRequestImage = "babel-go:1.18.0-alpine"
	HTTPRequestGo1_18_0_Bullseye HTTPRequestImage = "babel-go:1.18.0-debian-bullseye"
	HTTPRequestGo1_22_0_Alpine   HTTPRequestImage = "babel-go:1.22.0-alpine"
	HTTPRequestGo1_22_0_Bullseye HTTPRequestImage = "babel-go:1.22.0-debian-bullseye"
	HTTPRequestGo1_23_0_Alpine   HTTPRequestImage = "babel-go:1.23.0-alpine"
	HTTPRequestGo1_23_0_Bullseye HTTPRequestImage = "babel-go:1.23.0-debian-bullseye"
	HTTPRequestGo1_24_4_Alpine   HTTPRequestImage = "babel-go:1.24.4-alpine"
	HTTPRequestGo1_24_4_Bullseye HTTPRequestImage = "babel-go:1.24.4-debian-bullseye"
	HTTPRequestGo1_25_1_Alpine   HTTPRequestImage = "babel-go:1.25.1-alpine"
	HTTPRequestGo1_25_1_Bookworm HTTPRequestImage = "babel-go:1.25.1-debian-bookworm"
	HTTPRequestGo1_25_1_Trixie   HTTPRequestImage = "babel-go:1.25.1-debian-trixie"
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
		HTTPRequestPython3_9_0_Bullseye,
		HTTPRequestPython3_10_0_Alpine,
		HTTPRequestPython3_10_0_Bullseye,
		HTTPRequestPython3_11_0_Alpine,
		HTTPRequestPython3_11_0_Bullseye,
		HTTPRequestPython3_12_0_Alpine,
		HTTPRequestPython3_12_0_Bullseye,
	}
}

// AllRubyImages returns all available Ruby images
func AllRubyImages() []HTTPRequestImage {
	return []HTTPRequestImage{
		HTTPRequestRuby3_2_9_Alpine,
		HTTPRequestRuby3_2_9_Bullseye,
		HTTPRequestRuby3_3_9_Alpine,
		HTTPRequestRuby3_3_9_Bullseye,
		HTTPRequestRuby3_4_5_Alpine,
		HTTPRequestRuby3_4_5_Bullseye,
	}
}

// AllPHPImages returns all available PHP images
func AllPHPImages() []HTTPRequestImage {
	return []HTTPRequestImage{
		HTTPRequestPHP8_1_0_Alpine,
		HTTPRequestPHP8_1_0_Bullseye,
		HTTPRequestPHP8_2_0_Alpine,
		HTTPRequestPHP8_2_0_Bullseye,
		HTTPRequestPHP8_3_0_Alpine,
		HTTPRequestPHP8_3_0_Bullseye,
	}
}

// AllJavaImages returns all available Java images
func AllJavaImages() []HTTPRequestImage {
	return []HTTPRequestImage{
		HTTPRequestJava11_Corretto,
		HTTPRequestJava17_Corretto,
		HTTPRequestJava21_Corretto,
		HTTPRequestJava21_Temurin_JDK,
		HTTPRequestJava21_Temurin_JRE,
	}
}

// AllNodeJSImages returns all available Node.js images
func AllNodeJSImages() []HTTPRequestImage {
	return []HTTPRequestImage{
		HTTPRequestNodeJS18_20_0_Alpine,
		HTTPRequestNodeJS18_20_0_Bullseye,
		HTTPRequestNodeJS19_0_0_Alpine,
		HTTPRequestNodeJS19_0_0_Bullseye,
		HTTPRequestNodeJS22_16_0_Alpine,
		HTTPRequestNodeJS22_16_0_Bullseye,
		HTTPRequestNodeJS23_0_0_Alpine,
		HTTPRequestNodeJS23_0_0_Bullseye,
		HTTPRequestNodeJS24_5_0_Alpine,
		HTTPRequestNodeJS24_5_0_Bullseye,
	}
}

// AllGoImages returns all available Go images
func AllGoImages() []HTTPRequestImage {
	return []HTTPRequestImage{
		HTTPRequestGo1_18_0_Alpine,
		HTTPRequestGo1_18_0_Bullseye,
		HTTPRequestGo1_22_0_Alpine,
		HTTPRequestGo1_22_0_Bullseye,
		HTTPRequestGo1_23_0_Alpine,
		HTTPRequestGo1_23_0_Bullseye,
		HTTPRequestGo1_24_4_Alpine,
		HTTPRequestGo1_24_4_Bullseye,
		HTTPRequestGo1_25_1_Alpine,
		HTTPRequestGo1_25_1_Bookworm,
		HTTPRequestGo1_25_1_Trixie,
	}
}

// AllImages returns all available babel images
func AllImages() []HTTPRequestImage {
	images := make([]HTTPRequestImage, 0)
	images = append(images, AllPythonImages()...)
	images = append(images, AllRubyImages()...)
	images = append(images, AllPHPImages()...)
	images = append(images, AllNodeJSImages()...)
	images = append(images, AllGoImages()...)
	return images
}
