//go:build ignore
// +build ignore

// This file shows example usage of the Container.Update() and Pod.Update() methods
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/qpoint-io/qtap/pkg/container"
	"go.uber.org/zap"
)

func main() {
	// Create a container manager
	logger, _ := zap.NewDevelopment()
	manager := container.NewManager(logger, "", "", "")

	// Start the manager to connect to container runtimes
	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		log.Fatalf("Failed to start container manager: %v", err)
	}

	// Get a container by ID (this would be a real container ID in practice)
	containerID := "some-container-id"
	c := manager.GetByID(containerID)
	if c == nil {
		fmt.Println("Container not found")
		return
	}

	fmt.Printf("Container: %s, Image: %s\n", c.Name, c.Image)

	// Update the container to get latest data
	if err := c.Update(); err != nil {
		fmt.Printf("Failed to update container: %v\n", err)
	} else {
		fmt.Printf("Updated Container: %s, Image: %s\n", c.Name, c.Image)
	}

	// If the container has a pod, update it too
	if pod := c.Pod(); pod != nil {
		fmt.Printf("Pod: %s/%s\n", pod.Namespace, pod.Name)

		if err := pod.Update(); err != nil {
			fmt.Printf("Failed to update pod: %v\n", err)
		} else {
			fmt.Printf("Updated Pod: %s/%s with %d labels\n",
				pod.Namespace, pod.Name, len(pod.Labels))
		}
	}
}
