package runtime

import (
	"fmt"
	"path/filepath"
	"strings"
)

type Deno struct {
}

func (d *Deno) Start(bundle, listen string) (Stoppable, error) {
	// let's split the string
	parts := strings.Split(listen, ":")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid address format, expected '127.0.0.1:3000' got '%s'", listen)
	}

	// initialize a process
	proc := &Process{
		Name: "deno",
		Args: []string{
			"run",
			"--allow-env",
			"--allow-net",
			filepath.Join(bundle, "deno.ts"),
		},
		Env: []string{
			fmt.Sprintf("HOSTNAME=%s", parts[0]),
			fmt.Sprintf("PORT=%s", parts[1]),
		},
	}

	// start the process
	if err := proc.Start(); err != nil {
		return nil, fmt.Errorf("spawning deno: %w", err)
	}

	// return the process
	return proc, nil
}
