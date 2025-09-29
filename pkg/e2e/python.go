package e2e

func RegisterPython() {
	clients := map[string]*ClientCapabilities{
		"requests": {
			Name:         "requests",
			HTTPVersions: []string{"1.0", "1.1"},
		},
		"urllib3": {
			Name:         "urllib3",
			HTTPVersions: []string{"1.0", "1.1"},
		},
		"httpx": {
			Name:         "httpx",
			HTTPVersions: []string{"2"},
		},
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
