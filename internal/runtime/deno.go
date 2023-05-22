package runtime

import "fmt"

type Deno struct {
}

func (d *Deno) Start(bundle, listen string) (*Process, error) {
	// initialize a process
	proc := &Process{
		Name: "deno",
	}

	// start the process
	if err := proc.Start(); err != nil {
		return nil, fmt.Errorf("spawning deno: %w", err)
	}

	// return the process
	return proc, nil
}
