package e2e

func RegisterPython() {
	clients := map[string]*ClientCapabilities{
		"requests": {
			Name:          "requests",
			HTTPProtocols: []HTTPProtocol{HTTPProtocolHTTP1_0, HTTPProtocolHTTP1_1},
		},
		"urllib3": {
			Name:          "urllib3",
			HTTPProtocols: []HTTPProtocol{HTTPProtocolHTTP1_0, HTTPProtocolHTTP1_1},
		},
		"httpx": {
			Name:          "httpx",
			HTTPProtocols: []HTTPProtocol{HTTPProtocolHTTP1_0, HTTPProtocolHTTP1_1, HTTPProtocolHTTP2_0},
		},
		// "aiohttp": {
		// 	Name:          "aiohttp",
		// 	HTTPProtocols: []HTTPProtocol{HTTPProtocolHTTP1_1},
		// },
	}

	// Register Python 3.9 Alpine
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestPython3_9_0_Alpine,
		Language: Python,
		Version:  "3.10.0",
		OS:       "alpine",
		Clients:  clients,
	})
	// Register Python 3.9 Bullseye
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestPython3_9_0_Bullseye,
		Language: Python,
		Version:  "3.10.0",
		OS:       "bullseye",
		Clients:  clients,
	})

	// Register Python 3.10 Alpine
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestPython3_10_0_Alpine,
		Language: Python,
		Version:  "3.10.0",
		OS:       "alpine",
		Clients:  clients,
	})
	// Register Python 3.10 Bullseye
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestPython3_10_0_Bullseye,
		Language: Python,
		Version:  "3.10.0",
		OS:       "bullseye",
		Clients:  clients,
	})

	// Register Python 3.11 Alpine
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestPython3_11_0_Alpine,
		Language: Python,
		Version:  "3.11.0",
		OS:       "alpine",
		Clients:  clients,
	})
	// Register Python 3.11 Bullseye
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestPython3_11_0_Bullseye,
		Language: Python,
		Version:  "3.11.0",
		OS:       "bullseye",
		Clients:  clients,
	})

	// Register Python 3.12 Alpine
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestPython3_11_0_Alpine,
		Language: Python,
		Version:  "3.12.0",
		OS:       "alpine",
		Clients:  clients,
	})
	// Register Python 3.12 Bullseye
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestPython3_12_0_Bullseye,
		Language: Python,
		Version:  "3.12.0",
		OS:       "bullseye",
		Clients:  clients,
	})
}
