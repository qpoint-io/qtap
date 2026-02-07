package tlsutils

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
)

type ClientHello struct {
	SNI     string
	Version TLSVersion
	ALPNs   []string
}

// CipherSuite represents a TLS cipher suite
type CipherSuite uint16

func (c CipherSuite) String() string {
	return tls.CipherSuiteName(uint16(c))
}

func (c CipherSuite) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.String())
}

// ServerHello contains the negotiated TLS parameters from the server
type ServerHello struct {
	Version     TLSVersion
	CipherSuite CipherSuite
	ALPN        string // single selected protocol, not a list
}

type TLSVersion uint16

// TLS version constants
const (
	VersionTLS10 TLSVersion = 0x0301
	VersionTLS11 TLSVersion = 0x0302
	VersionTLS12 TLSVersion = 0x0303
	VersionTLS13 TLSVersion = 0x0304
)

func (v TLSVersion) String() string {
	return tls.VersionName(uint16(v))
}

func (v TLSVersion) Float() float64 {
	switch v {
	case VersionTLS10:
		return 1.0
	case VersionTLS11:
		return 1.1
	case VersionTLS12:
		return 1.2
	case VersionTLS13:
		return 1.3
	default:
		return 0
	}
}

func (v TLSVersion) MarshalText() ([]byte, error) {
	return []byte(v.String()), nil
}

func (v TLSVersion) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.String())
}

// ParseClientHello parses a TLS client hello message from a byte slice.
func ParseClientHello(record []byte) (c *ClientHello, err error) {
	// Validate input
	if len(record) == 0 {
		return nil, errors.New("empty record")
	}

	// Create a packet from the TLS record bytes
	packet := gopacket.NewPacket(record, layers.LayerTypeTLS, gopacket.Default)

	// Check for decoding errors
	if packet.ErrorLayer() != nil {
		return nil, fmt.Errorf("packet decoding error: %w", packet.ErrorLayer().Error())
	}

	// Extract the TLS layer
	tlsLayer := packet.Layer(layers.LayerTypeTLS)
	if tlsLayer == nil {
		return nil, errors.New("no TLS layer found")
	}

	tls, ok := tlsLayer.(*layers.TLS)
	if !ok || tls == nil {
		return nil, errors.New("invalid TLS layer type")
	}

	// Look for handshake records
	for _, handshake := range tls.Handshake {
		// Check if this is a ClientHello (HandshakeType = 1)
		if handshake.ClientHello.HandshakeType == 1 {
			c = &ClientHello{
				Version: TLSVersion(handshake.ClientHello.ProtocolVersion),
				SNI:     string(handshake.ClientHello.SNI),
			}

			// Parse extensions for ALPN and supported versions
			if len(handshake.ClientHello.Extensions) > 0 {
				c.ALPNs = parseALPNFromExtensions(handshake.ClientHello.Extensions)
				if version := parseSupportedVersionsFromExtensions(handshake.ClientHello.Extensions); version != 0 {
					c.Version = TLSVersion(version)
				}
			}

			return c, nil
		}
	}

	return nil, errors.New("no ClientHello found in TLS handshake")
}

// parseALPNFromExtensions extracts ALPN protocols from TLS extensions
func parseALPNFromExtensions(extensions []byte) []string {
	if len(extensions) < 4 {
		return nil
	}

	offset := 0
	var protocols []string

	for offset < len(extensions) {
		// Need at least 4 bytes for extension header
		if offset+4 > len(extensions) {
			break
		}

		extType := uint16(extensions[offset])<<8 | uint16(extensions[offset+1])
		extLen := int(extensions[offset+2])<<8 | int(extensions[offset+3])
		offset += 4

		// Validate extension length
		if extLen < 0 || offset+extLen > len(extensions) {
			break
		}

		if extType == 16 { // ALPN extension
			// Parse ALPN data
			alpnData := extensions[offset : offset+extLen]
			if len(alpnData) >= 2 {
				// Read protocol name list length (2 bytes)
				protoListLen := int(alpnData[0])<<8 | int(alpnData[1])

				// Validate protocol list length
				if protoListLen < 0 || protoListLen+2 > len(alpnData) {
					break
				}

				alpnOffset := 2

				for alpnOffset < 2+protoListLen && alpnOffset < len(alpnData) {
					if alpnOffset+1 > len(alpnData) {
						break
					}

					protoLen := int(alpnData[alpnOffset])
					alpnOffset++

					// Validate protocol length
					if protoLen < 0 || alpnOffset+protoLen > len(alpnData) {
						break
					}

					// Extract protocol string
					if protoLen > 0 {
						protocol := string(alpnData[alpnOffset : alpnOffset+protoLen])
						protocols = append(protocols, protocol)
					}
					alpnOffset += protoLen
				}
			}
			break
		}

		offset += extLen
	}

	return protocols
}

// parseSupportedVersionsFromExtensions finds the highest supported TLS version from extensions
func parseSupportedVersionsFromExtensions(extensions []byte) uint16 {
	if len(extensions) < 4 {
		return 0
	}

	offset := 0

	for offset < len(extensions) {
		// Need at least 4 bytes for extension header
		if offset+4 > len(extensions) {
			break
		}

		extType := uint16(extensions[offset])<<8 | uint16(extensions[offset+1])
		extLen := int(extensions[offset+2])<<8 | int(extensions[offset+3])
		offset += 4

		// Validate extension length
		if extLen < 0 || offset+extLen > len(extensions) {
			break
		}

		if extType == 43 { // Supported Versions extension
			// Parse supported versions
			verData := extensions[offset : offset+extLen]
			if len(verData) >= 1 {
				versionsLen := int(verData[0])
				verOffset := 1

				// Validate versions list length
				if versionsLen < 2 || versionsLen%2 != 0 || verOffset+versionsLen > len(verData) {
					break
				}

				var highestVersion uint16
				for range versionsLen / 2 {
					if verOffset+2 > len(verData) {
						break
					}

					version := uint16(verData[verOffset])<<8 | uint16(verData[verOffset+1])
					// Validate version is within reasonable TLS version range
					if version >= 0x0301 && version <= 0x0304 && version > highestVersion {
						highestVersion = version
					}
					verOffset += 2
				}
				return highestVersion
			}
			break
		}

		offset += extLen
	}

	return 0
}

func (c *ClientHello) ControlValues() map[string]any {
	return map[string]any{
		"enabled": true,
		"version": c.Version.Float(),
		"sni":     c.SNI,
		"alpn":    c.ALPNs,
	}
}

// ParseServerHello parses a TLS ServerHello message from a byte slice.
// The input should be a complete TLS record starting with the record header.
func ParseServerHello(record []byte) (*ServerHello, error) {
	// Minimum size: 5 (record header) + 4 (handshake header) + 2 (version) + 32 (random) + 1 (session id len)
	const minSize = 44
	if len(record) < minSize {
		return nil, errors.New("record too short for ServerHello")
	}

	// Validate TLS record header
	// record[0] = content type (0x16 = handshake)
	// record[1:3] = version
	// record[3:5] = length
	if record[0] != 0x16 {
		return nil, errors.New("not a TLS handshake record")
	}

	recordLen := int(record[3])<<8 | int(record[4])
	if len(record) < 5+recordLen {
		return nil, errors.New("incomplete TLS record")
	}

	// Move past record header to handshake message
	handshake := record[5:]

	// Validate handshake header
	// handshake[0] = handshake type (0x02 = ServerHello)
	// handshake[1:4] = length (24-bit)
	if handshake[0] != 0x02 {
		return nil, fmt.Errorf("not a ServerHello (type=%d)", handshake[0])
	}

	handshakeLen := int(handshake[1])<<16 | int(handshake[2])<<8 | int(handshake[3])
	if len(handshake) < 4+handshakeLen {
		return nil, errors.New("incomplete ServerHello handshake")
	}

	// Parse ServerHello body (after handshake header)
	body := handshake[4:]

	// ServerHello structure:
	// - version: 2 bytes
	// - random: 32 bytes
	// - session_id_length: 1 byte
	// - session_id: variable
	// - cipher_suite: 2 bytes
	// - compression_method: 1 byte
	// - extensions_length: 2 bytes (optional)
	// - extensions: variable (optional)

	if len(body) < 35 { // 2 + 32 + 1 minimum
		return nil, errors.New("ServerHello body too short")
	}

	s := &ServerHello{}

	// Version (may be overridden by supported_versions extension for TLS 1.3)
	s.Version = TLSVersion(uint16(body[0])<<8 | uint16(body[1]))

	// Skip random (32 bytes)
	offset := 34

	// Session ID
	if offset >= len(body) {
		return nil, errors.New("unexpected end of ServerHello")
	}
	sessionIDLen := int(body[offset])
	offset++
	offset += sessionIDLen

	// Cipher suite (2 bytes)
	if offset+2 > len(body) {
		return nil, errors.New("unexpected end of ServerHello at cipher suite")
	}
	s.CipherSuite = CipherSuite(uint16(body[offset])<<8 | uint16(body[offset+1]))
	offset += 2

	// Compression method (1 byte)
	if offset >= len(body) {
		return nil, errors.New("unexpected end of ServerHello at compression")
	}
	offset++

	// Extensions (optional)
	if offset+2 <= len(body) {
		extLen := int(body[offset])<<8 | int(body[offset+1])
		offset += 2

		if offset+extLen <= len(body) {
			extensions := body[offset : offset+extLen]

			// Parse extensions for supported_versions (TLS 1.3) and ALPN
			s.parseServerHelloExtensions(extensions)
		}
	}

	return s, nil
}

// parseServerHelloExtensions parses extensions from ServerHello
func (s *ServerHello) parseServerHelloExtensions(extensions []byte) {
	offset := 0

	for offset+4 <= len(extensions) {
		extType := uint16(extensions[offset])<<8 | uint16(extensions[offset+1])
		extLen := int(extensions[offset+2])<<8 | int(extensions[offset+3])
		offset += 4

		if offset+extLen > len(extensions) {
			break
		}

		extData := extensions[offset : offset+extLen]

		switch extType {
		case 43: // supported_versions - contains actual negotiated version for TLS 1.3
			if len(extData) >= 2 {
				s.Version = TLSVersion(uint16(extData[0])<<8 | uint16(extData[1]))
			}
		case 16: // ALPN - contains single selected protocol
			// Format: 2 bytes list length, then 1 byte protocol length + protocol
			if len(extData) >= 4 {
				protoLen := int(extData[2])
				if len(extData) >= 3+protoLen {
					s.ALPN = string(extData[3 : 3+protoLen])
				}
			}
		}

		offset += extLen
	}
}

func (s *ServerHello) ControlValues() map[string]any {
	return map[string]any{
		"enabled":     true,
		"version":     s.Version.Float(),
		"cipherSuite": s.CipherSuite.String(),
		"alpn":        s.ALPN,
	}
}
