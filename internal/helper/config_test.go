package helper

import (
	"testing"

	"github.com/hashicorp/hcl/v2/hclsimple"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSPIFFEHelper(t *testing.T) {
	tests := []struct {
		name                      string
		params                    SPIFFEHelperConfigParams
		expectedHCLSubstrings     map[string]string // Key: expected field, Value: expected HCL representation
		expectError               bool
		expectedErrorMsgSubstring string
	}{
		{
			name: "default params",
			params: SPIFFEHelperConfigParams{
				AgentAddress: "/tmp/agent.sock",
				CertPath:     "/mnt/certs",
			},
			expectedHCLSubstrings: map[string]string{
				"AgentAddress":             `agent_address = "/tmp/agent.sock"`,
				"CertDir":                  `cert_dir = "/mnt/certs"`,
				"AddIntermediatesToBundle": `add_intermediates_to_bundle = false`,
				"DaemonMode":               `daemon_mode = true`,
				"IncludeFederatedDomains":  `include_federated_domains = true`,
				"SVIDFilename":             `svid_file_name = "tls.crt"`,
				"SVIDKeyFilename":          `svid_key_file_name = "tls.key"`,
				"SVIDBundleFilename":       `svid_bundle_file_name = "ca.pem"`,
				"HealthCheckEnabled":       `listener_enabled = true`,
			},
			expectError: false,
		},
		{
			name: "with inc intermediate bundle",
			params: SPIFFEHelperConfigParams{
				AgentAddress:              "unix:///tmp/spire-agent/public/api.sock",
				CertPath:                  "/mnt/certs",
				IncludeIntermediateBundle: true,
			},
			expectedHCLSubstrings: map[string]string{
				"AgentAddress":             `agent_address = "unix:///tmp/spire-agent/public/api.sock"`,
				"CertDir":                  `cert_dir = "/etc/workload-certs"`,
				"AddIntermediatesToBundle": `add_intermediates_to_bundle = true`,
			},
			expectError: false,
		},
		{
			name: "empty params", // Check defaults or expected behavior for empty strings
			params: SPIFFEHelperConfigParams{
				AgentAddress: "",
				CertPath:     "",
			},
			expectedHCLSubstrings: map[string]string{
				"AgentAddress":             `agent_address = ""`,
				"CertDir":                  `cert_dir = ""`,
				"AddIntermediatesToBundle": `add_intermediates_to_bundle = false`,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			helper, err := NewSPIFFEHelper(tt.params)

			if tt.expectError {
				require.Error(t, err)
				if tt.expectedErrorMsgSubstring != "" {
					require.Contains(t, err.Error(), tt.expectedErrorMsgSubstring)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, helper)
			require.NotEmpty(t, helper.Config)

			// Parse the generated HCL string back into the SPIFFEHelperConfig struct
			var decodedCfg SPIFFEHelperConfig
			// The filename "config.hcl" is nominal for hclsimple.Decode when parsing from bytes.
			err = hclsimple.Decode("config.hcl", []byte(helper.Config), nil, &decodedCfg)
			require.NoError(t, err, "Failed to decode generated HCL config: %s", helper.Config)

			// --- Assertions based on input params ---
			assert.Equal(t, tt.params.AgentAddress, decodedCfg.AgentAddress)
			assert.Equal(t, tt.params.CertPath, decodedCfg.CertDir)
			assert.Equal(t, tt.params.IncludeIntermediateBundle, decodedCfg.AddIntermediatesToBundle)

			// --- Assertions for default values set by NewSPIFFEHelper ---
			require.NotNil(t, decodedCfg.DaemonMode)
			assert.True(t, *decodedCfg.DaemonMode)
			assert.True(t, decodedCfg.IncludeFederatedDomains)

			assert.Equal(t, "tls.crt", decodedCfg.SVIDFilename)
			assert.Equal(t, "tls.key", decodedCfg.SVIDKeyFilename)
			assert.Equal(t, "ca.pem", decodedCfg.SVIDBundleFilename)

			assert.True(t, decodedCfg.HealthCheck.ListenerEnabled)
		})
	}
}
