package e2e

func RegisterNodeJS() {
	clients := map[string]*ClientCapabilities{
		"default": {
			Name:          "default",
			HTTPProtocols: []HTTPProtocol{HTTPProtocolHTTP1_0, HTTPProtocolHTTP1_1, HTTPProtocolHTTP2_0},
		},
	}

	// Register Node.js 18.20.0 Alpine
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestNodeJS18_20_0_Alpine,
		Language: NodeJS,
		Version:  "18.20.0",
		OS:       "alpine",
		Clients:  clients,
	})
	// Register Node.js 18.20.0 Bullseye
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestNodeJS18_20_0_Bullseye,
		Language: NodeJS,
		Version:  "18.20.0",
		OS:       "bullseye",
		Clients:  clients,
	})

	// Register Node.js 19.0.0 Alpine
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestNodeJS19_0_0_Alpine,
		Language: NodeJS,
		Version:  "19.0.0",
		OS:       "alpine",
		Clients:  clients,
	})
	// Register Node.js 19.0.0 Bullseye
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestNodeJS19_0_0_Bullseye,
		Language: NodeJS,
		Version:  "19.0.0",
		OS:       "bullseye",
		Clients:  clients,
	})

	// Register Node.js 22.16.0 Alpine
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestNodeJS22_16_0_Alpine,
		Language: NodeJS,
		Version:  "22.16.0",
		OS:       "alpine",
		Clients:  clients,
	})
	// Register Node.js 22.16.0 Bullseye
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestNodeJS22_16_0_Bullseye,
		Language: NodeJS,
		Version:  "22.16.0",
		OS:       "bullseye",
		Clients:  clients,
	})

	// Register Node.js 23.0.0 Alpine
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestNodeJS23_0_0_Alpine,
		Language: NodeJS,
		Version:  "23.0.0",
		OS:       "alpine",
		Clients:  clients,
	})
	// Register Node.js 23.0.0 Bullseye
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestNodeJS23_0_0_Bullseye,
		Language: NodeJS,
		Version:  "23.0.0",
		OS:       "bullseye",
		Clients:  clients,
	})

	// Register Node.js 24.5.0 Alpine
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestNodeJS24_5_0_Alpine,
		Language: NodeJS,
		Version:  "24.5.0",
		OS:       "alpine",
		Clients:  clients,
	})
	// Register Node.js 24.5.0 Bullseye
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestNodeJS24_5_0_Bullseye,
		Language: NodeJS,
		Version:  "24.5.0",
		OS:       "bullseye",
		Clients:  clients,
	})
}
