package e2e

func RegisterPHP() {
	clients := map[string]*ClientCapabilities{
		"guzzle": {
			Name:          "guzzle",
			HTTPProtocols: []HTTPProtocol{HTTPProtocolHTTP1_1, HTTPProtocolHTTP2_0},
		},
		"curl": {
			Name:          "curl",
			HTTPProtocols: []HTTPProtocol{HTTPProtocolHTTP1_1, HTTPProtocolHTTP2_0},
		},
	}

	// Register PHP 8.1 Alpine
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestPHP8_1_0_Alpine,
		Language: PHP,
		Version:  "8.1",
		OS:       "alpine",
		Clients:  clients,
	})
	// Register PHP 8.1 Bullseye
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestPHP8_1_0_Bullseye,
		Language: PHP,
		Version:  "8.1",
		OS:       "bullseye",
		Clients:  clients,
	})

	// Register PHP 8.2 Alpine
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestPHP8_2_0_Alpine,
		Language: PHP,
		Version:  "8.2",
		OS:       "alpine",
		Clients:  clients,
	})
	// Register PHP 8.2 Bullseye
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestPHP8_2_0_Bullseye,
		Language: PHP,
		Version:  "8.2",
		OS:       "bullseye",
		Clients:  clients,
	})

	// Register PHP 8.3 Alpine
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestPHP8_3_0_Alpine,
		Language: PHP,
		Version:  "8.3",
		OS:       "alpine",
		Clients:  clients,
	})
	// Register PHP 8.3 Bullseye
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestPHP8_3_0_Bullseye,
		Language: PHP,
		Version:  "8.3",
		OS:       "bullseye",
		Clients:  clients,
	})
}
