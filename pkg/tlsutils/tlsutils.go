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
