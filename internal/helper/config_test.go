package helper

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfig(t *testing.T) {
	for _, tt := range []struct {
		name        string
		params      *SPIFFEHelperConfigParams
		expectError string
	}{
		{
			name:   "no error",
			params: &SPIFFEHelperConfigParams{},
		},
		{
			name: "no error",
			params: &SPIFFEHelperConfigParams{
				IncludeIntermediateBundle: true,
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			//log, _ := test.NewNullLogger()
			helper, err := NewSPIFFEHelper(*tt.params)
			require.NoError(t, err)

			cfgStr := helper.Config
			require.NotNil(t, cfgStr)
		})
	}
}
