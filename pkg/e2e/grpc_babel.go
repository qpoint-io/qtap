package e2e

import (
	"os"
	"strings"
)

type GRPCRequestImage string

var (
	// Go
	GRPCRequestGo1_25_1_Alpine GRPCRequestImage = "babel-go:1.25.1-grpc-alpine"
	// Java
	GRPCRequestJava21_Alpine GRPCRequestImage = "babel-java:21-grpc-corretto-alpine"
	// Python
	GRPCRequestPython3_12_0_Alpine GRPCRequestImage = "babel-python:3.12.0-grpc-alpine"
	// NodeJS
	GRPCRequestNodeJS22_16_0_Alpine GRPCRequestImage = "babel-nodejs:22.16.0-grpc-alpine"
	// Ruby
	GRPCRequestRuby3_4_5_Alpine GRPCRequestImage = "babel-ruby:3.4.5-grpc-alpine"
	// PHP
	GRPCRequestPHP8_3_Alpine GRPCRequestImage = "babel-php:8.3-grpc-alpine"
)

func (i GRPCRequestImage) String() string {
	if os.Getenv("USE_LOCAL_IMAGES") == "true" {
		return string(i)
	}
	return officialImagesRegistry + string(i)
}

// TestName returns a name for the image that can be used for consistent
// Go test names and reporting. For example:
// babel-go:1.25.1-grpc-alpine -> go/1_25_1/grpc/alpine
func (i GRPCRequestImage) TestName() string {
	name, _ := strings.CutPrefix(string(i), "babel-")
	name = strings.ReplaceAll(name, ":", "/")
	name = strings.ReplaceAll(name, "-", "/")
	name = strings.ReplaceAll(name, ".", "_")
	return name
}

// AllGRPCGoImages returns all available gRPC Go images
func AllGRPCGoImages() []GRPCRequestImage {
	return []GRPCRequestImage{
		GRPCRequestGo1_25_1_Alpine,
	}
}

// AllGRPCJavaImages returns all available gRPC Java images
func AllGRPCJavaImages() []GRPCRequestImage {
	return []GRPCRequestImage{
		GRPCRequestJava21_Alpine,
	}
}

// AllGRPCPythonImages returns all available gRPC Python images
func AllGRPCPythonImages() []GRPCRequestImage {
	return []GRPCRequestImage{
		GRPCRequestPython3_12_0_Alpine,
	}
}

// AllGRPCNodeJSImages returns all available gRPC NodeJS images
func AllGRPCNodeJSImages() []GRPCRequestImage {
	return []GRPCRequestImage{
		GRPCRequestNodeJS22_16_0_Alpine,
	}
}

// AllGRPCRubyImages returns all available gRPC Ruby images
func AllGRPCRubyImages() []GRPCRequestImage {
	return []GRPCRequestImage{
		GRPCRequestRuby3_4_5_Alpine,
	}
}

// AllGRPCPHPImages returns all available gRPC PHP images
func AllGRPCPHPImages() []GRPCRequestImage {
	return []GRPCRequestImage{
		GRPCRequestPHP8_3_Alpine,
	}
}

// AllGRPCImages returns all available gRPC babel images
func AllGRPCImages() []GRPCRequestImage {
	var images []GRPCRequestImage
	images = append(images, AllGRPCGoImages()...)
	images = append(images, AllGRPCJavaImages()...)
	images = append(images, AllGRPCPythonImages()...)
	images = append(images, AllGRPCNodeJSImages()...)
	images = append(images, AllGRPCRubyImages()...)
	images = append(images, AllGRPCPHPImages()...)
	return images
}
