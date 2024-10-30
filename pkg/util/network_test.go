package util

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_ParseDHCPOptions(t *testing.T) {
	tests := []struct {
		dhcpOptions string
		wantMap     map[string]string
	}{
		{
			dhcpOptions: "lease_time=3600, router={ 10.0.0.1, 10.0.0.2},  server_id=169.254.0.254, server_mac=00:00:00:2E:2F:B8, classless_static_route={30.0.0.0/24,10.0.0.10, 0.0.0.0/0,10.0.0.1}",
			wantMap: map[string]string{
				"lease_time":             "3600",
				"router":                 "10.0.0.1,10.0.0.2",
				"server_id":              "169.254.0.254",
				"server_mac":             "00:00:00:2E:2F:B8",
				"classless_static_route": "30.0.0.0/24,10.0.0.10,0.0.0.0/0,10.0.0.1",
			},
		},
		{
			dhcpOptions: "lease_time=3600, router={ 10.0.0.1; 10.0.0.2},  server_id=169.254.0.254, server_mac=00:00:00:2E:2F:B8, classless_static_route={30.0.0.0/24;10.0.0.10; 0.0.0.0/0;10.0.0.1}",
			wantMap: map[string]string{
				"lease_time":             "3600",
				"router":                 "10.0.0.1,10.0.0.2",
				"server_id":              "169.254.0.254",
				"server_mac":             "00:00:00:2E:2F:B8",
				"classless_static_route": "30.0.0.0/24,10.0.0.10,0.0.0.0/0,10.0.0.1",
			},
		},
	}

	for i, test := range tests {
		t.Run("Example_"+strconv.Itoa(i), func(t *testing.T) {
			test.dhcpOptions = strings.ReplaceAll(test.dhcpOptions, " ", "")
			options := ParseDHCPOptions(test.dhcpOptions)
			assert.Equal(t, test.wantMap, options, "")
		})
	}

}
