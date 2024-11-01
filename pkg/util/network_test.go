package util

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

//IPv6 DNS
//1、腾讯 DNS：2402:4e00::
//2、阿里 DNS：2400:3200::1、2400:3200:baba::1
//3、百度：2400:da00::6666
//4、DNS.SB：2a09::、2a11::
//5、下一代互联网国家工程中心：240C::6666、240C::6644
//6、谷歌：2001:4860:4860::8888、2001:4860:4860::8844

func Test_IPv6DNS(t *testing.T) {
	tests := []struct {
		dns  []string
		want bool
	}{
		{
			dns:  []string{"2402:4e00::"},
			want: true,
		},
		{
			dns:  []string{"2400:3200::1", "2400:3200:baba::1"},
			want: true,
		},
		{
			dns:  []string{"2400:da00::6666"},
			want: true,
		},
		{
			dns:  []string{"2a09::", "2a11::"},
			want: true,
		},
		{
			dns:  []string{"240C::6666", "240C::6644"},
			want: true,
		},
		{
			dns:  []string{"2001:4860:4860::8888", "2001:4860:4860::8844"},
			want: true,
		},
	}
	for i, test := range tests {
		t.Run("Example_"+strconv.Itoa(i), func(t *testing.T) {
			for _, dns := range test.dns {
				isIPv6 := IsIPv6(dns)
				assert.Equal(t, test.want, isIPv6, "")
			}
		})
	}
}

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
