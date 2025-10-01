package e2e

func RegisterRuby() {
	clients := map[string]*ClientCapabilities{
		"default": {
			Name:         "default",
			HTTPVersions: []string{"1.0", "1.1"},
		},
	}

	// Register Ruby 3.2.9 Alpine
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestRuby3_2_9_Alpine,
		Language: Ruby,
		Version:  "3.2.9",
		OS:       "alpine",
		Clients:  clients,
	})
	// Register Ruby 3.2.9 Bullseye
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestRuby3_2_9_Bullseye,
		Language: Ruby,
		Version:  "3.2.9",
		OS:       "bullseye",
		Clients:  clients,
	})

	// Register Ruby 3.3.9 Alpine
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestRuby3_3_9_Alpine,
		Language: Ruby,
		Version:  "3.3.9",
		OS:       "alpine",
		Clients:  clients,
	})
	// Register Ruby 3.3.9 Bullseye
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestRuby3_3_9_Bullseye,
		Language: Ruby,
		Version:  "3.3.9",
		OS:       "bullseye",
		Clients:  clients,
	})

	// Register Ruby 3.4.5 Alpine
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestRuby3_4_5_Alpine,
		Language: Ruby,
		Version:  "3.4.5",
		OS:       "alpine",
		Clients:  clients,
	})
	// Register Ruby 3.4.5 Bullseye
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestRuby3_4_5_Bullseye,
		Language: Ruby,
		Version:  "3.4.5",
		OS:       "bullseye",
		Clients:  clients,
	})
}
