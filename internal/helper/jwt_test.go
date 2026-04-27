package helper

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseJWTConfigFromAnnotations(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		expected    []SPIFFEHelperJWTConfig
	}{
		{
			name: "basic JWT config",
			annotations: map[string]string{
				"spiffe.io/helper-jwt-audience": "sts.amazonaws.com",
				"spiffe.io/helper-jwt-filename": "tokens/token",
			},
			expected: []SPIFFEHelperJWTConfig{
				{
					JWTAudience:       "sts.amazonaws.com",
					JWTExtraAudiences: []string{},
					JWTSVIDFilename:   "tokens/token",
				},
			},
		},
		{
			name: "JWT config with extra audiences",
			annotations: map[string]string{
				"spiffe.io/helper-jwt-audience":        "sts.amazonaws.com",
				"spiffe.io/helper-jwt-filename":        "tokens/aws",
				"spiffe.io/helper-jwt-extra-audiences": "audience1, audience2",
			},
			expected: []SPIFFEHelperJWTConfig{
				{
					JWTAudience:       "sts.amazonaws.com",
					JWTSVIDFilename:   "tokens/aws",
					JWTExtraAudiences: []string{"audience1", "audience2"},
				},
			},
		},
		{
			name: "default filename when not specified",
			annotations: map[string]string{
				"spiffe.io/helper-jwt-audience": "sts.amazonaws.com",
			},
			expected: []SPIFFEHelperJWTConfig{
				{
					JWTAudience:       "sts.amazonaws.com",
					JWTExtraAudiences: []string{},
					JWTSVIDFilename:   "tokens/token",
				},
			},
		},
		{
			name:        "no JWT config when audience missing",
			annotations: map[string]string{},
			expected:    nil, // Changed from []SPIFFEHelperJWTConfig{}
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseJWTConfigFromAnnotations(tt.annotations)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseJWTSVIDFileModeFromAnnotations(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		expected    int
		expectErr   bool
	}{
		{
			name:        "default when missing",
			annotations: map[string]string{},
			expected:    600,
			expectErr:   false,
		},
		{
			name: "custom mode",
			annotations: map[string]string{
				"spiffe.io/helper-jwt-svid-file-mode": "644",
			},
			expected:  644,
			expectErr: false,
		},
		{
			name: "reject non-integer",
			annotations: map[string]string{
				"spiffe.io/helper-jwt-svid-file-mode": "abc",
			},
			expected:  0,
			expectErr: true,
		},
		{
			name: "reject non-positive",
			annotations: map[string]string{
				"spiffe.io/helper-jwt-svid-file-mode": "0",
			},
			expected:  0,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode, err := ParseJWTSVIDFileModeFromAnnotations(tt.annotations)
			if tt.expectErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, mode)
		})
	}
}
