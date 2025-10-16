package e2e

func RegisterGo() {
	clients := map[string]*ClientCapabilities{
		"net/http": {
			Name:          "net/http",
			HTTPProtocols: []HTTPProtocol{HTTPProtocolHTTP1_0, HTTPProtocolHTTP1_1, HTTPProtocolHTTP2_0},
		},
	}

	// Register Go 1.14.0 Alpine
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestGo1_14_0_Alpine,
		Language: Go,
		Version:  "1.14.0",
		OS:       "alpine",
		Clients:  clients,
	})
	// Register Go 1.14.0 Buster
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestGo1_14_0_Buster,
		Language: Go,
		Version:  "1.14.0",
		OS:       "buster",
		Clients:  clients,
	})

	// Register Go 1.18.0 Alpine
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestGo1_18_0_Alpine,
		Language: Go,
		Version:  "1.18.0",
		OS:       "alpine",
		Clients:  clients,
	})
	// Register Go 1.18.0 Bullseye
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestGo1_18_0_Bullseye,
		Language: Go,
		Version:  "1.18.0",
		OS:       "bullseye",
		Clients:  clients,
	})

	// Register Go 1.22.0 Alpine
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestGo1_22_0_Alpine,
		Language: Go,
		Version:  "1.22.0",
		OS:       "alpine",
		Clients:  clients,
	})
	// Register Go 1.22.0 Bullseye
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestGo1_22_0_Bullseye,
		Language: Go,
		Version:  "1.22.0",
		OS:       "bullseye",
		Clients:  clients,
	})

	// Register Go 1.23.0 Alpine
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestGo1_23_0_Alpine,
		Language: Go,
		Version:  "1.23.0",
		OS:       "alpine",
		Clients:  clients,
	})
	// Register Go 1.23.0 Bullseye
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestGo1_23_0_Bullseye,
		Language: Go,
		Version:  "1.23.0",
		OS:       "bullseye",
		Clients:  clients,
	})

	// Register Go 1.24.4 Alpine
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestGo1_24_4_Alpine,
		Language: Go,
		Version:  "1.24.4",
		OS:       "alpine",
		Clients:  clients,
	})
	// Register Go 1.24.4 Bullseye
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestGo1_24_4_Bullseye,
		Language: Go,
		Version:  "1.24.4",
		OS:       "bullseye",
		Clients:  clients,
	})

	// Register Go 1.25.1 Alpine
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestGo1_25_1_Alpine,
		Language: Go,
		Version:  "1.25.1",
		OS:       "alpine",
		Clients:  clients,
	})
	// Register Go 1.25.1 Bookworm
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestGo1_25_1_Bookworm,
		Language: Go,
		Version:  "1.25.1",
		OS:       "bookworm",
		Clients:  clients,
	})
	// Register Go 1.25.1 Trixie
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestGo1_25_1_Trixie,
		Language: Go,
		Version:  "1.25.1",
		OS:       "trixie",
		Clients:  clients,
	})
}
