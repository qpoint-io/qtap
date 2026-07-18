package nodetls

import (
	"testing"
)

func TestSymAddrsFromVersion(t *testing.T) {
	tests := []struct {
		name                            string
		version                         string
		wantTLSWrapStreamListenerOffset int32
		wantErr                         bool
	}{
		{
			name:                            "v15.0.0",
			version:                         "15.0.0",
			wantTLSWrapStreamListenerOffset: 0x78,
		},
		{
			name:                            "v22.7.0 exact",
			version:                         "22.7.0",
			wantTLSWrapStreamListenerOffset: 0x80,
		},
		{
			name:                            "v22.8.0 uses v22.7 offsets",
			version:                         "22.8.0",
			wantTLSWrapStreamListenerOffset: 0x80,
		},
		{
			name:                            "v22.16.0 uses v22.7 offsets",
			version:                         "22.16.0",
			wantTLSWrapStreamListenerOffset: 0x80,
		},
		{
			name:                            "v22.19.0 uses v22.7 offsets (last before BaseObject change)",
			version:                         "22.19.0",
			wantTLSWrapStreamListenerOffset: 0x80,
		},
		{
			name:                            "v22.20.0 uses v23 offsets (BaseObject list_node_ added)",
			version:                         "22.20.0",
			wantTLSWrapStreamListenerOffset: 0x90,
		},
		{
			name:                            "v22.21.0 uses v23 offsets",
			version:                         "22.21.0",
			wantTLSWrapStreamListenerOffset: 0x90,
		},
		{
			name:                            "v22.22.0 uses v23 offsets",
			version:                         "22.22.0",
			wantTLSWrapStreamListenerOffset: 0x90,
		},
		{
			name:                            "v22.99.0 uses v23 offsets",
			version:                         "22.99.0",
			wantTLSWrapStreamListenerOffset: 0x90,
		},
		{
			name:                            "v23.0.0 uses v23 offsets",
			version:                         "23.0.0",
			wantTLSWrapStreamListenerOffset: 0x90,
		},
		{
			name:                            "v23.5.0 uses v23 offsets",
			version:                         "23.5.0",
			wantTLSWrapStreamListenerOffset: 0x90,
		},
		{
			name:                            "v24.0.0 uses v23 offsets (floor)",
			version:                         "24.0.0",
			wantTLSWrapStreamListenerOffset: 0x90,
		},
		{
			name:    "v10.0.0 too old",
			version: "10.0.0",
			wantErr: true,
		},
		{
			name:    "invalid version",
			version: "abc",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := symAddrsFromVersion(tt.version)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for version %s, got nil", tt.version)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for version %s: %v", tt.version, err)
			}
			if got.TLSWrapStreamListenerOffset != tt.wantTLSWrapStreamListenerOffset {
				t.Errorf("version %s: TLSWrapStreamListenerOffset = 0x%x, want 0x%x",
					tt.version, got.TLSWrapStreamListenerOffset, tt.wantTLSWrapStreamListenerOffset)
			}
		})
	}
}
