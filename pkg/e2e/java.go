package e2e

func RegisterJava() {
	clients := map[string]*ClientCapabilities{
		"default": {
			Name:          "default",
			HTTPProtocols: []HTTPProtocol{HTTPProtocolHTTP1_1, HTTPProtocolHTTP2_0},
		},
		"OkHttp": {
			Name:          "okhttp",
			HTTPProtocols: []HTTPProtocol{HTTPProtocolHTTP1_1, HTTPProtocolHTTP2_0},
		},
	}

	// Register Java 11 Corretto
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestJava11_Corretto,
		Language: Java,
		Version:  "11",
		OS:       "alpine",
		Clients:  clients,
	})

	// Register Java 17 Corretto
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestJava17_Corretto,
		Language: Java,
		Version:  "17",
		OS:       "alpine",
		Clients:  clients,
	})

	// Register Java 21 Corretto
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestJava21_Corretto,
		Language: Java,
		Version:  "21",
		OS:       "alpine",
		Clients:  clients,
	})

	// Register Java 21 Temurin JDK
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestJava21_Temurin_JDK,
		Language: Java,
		Version:  "21",
		OS:       "temurin",
		Clients:  clients,
	})

	// Register Java 21 Temurin JRE
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestJava21_Temurin_JRE,
		Language: Java,
		Version:  "21",
		OS:       "temurin",
		Clients:  clients,
	})
}
