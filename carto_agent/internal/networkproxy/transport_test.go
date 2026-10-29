package networkproxy

import (
	"net/http"
	"testing"
)

func TestExplicitProxyPrecedesEnvironmentAndBypassesLocalhost(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://environment.example:3128")
	transport := Transport("http://configured.example:8080")
	request, _ := http.NewRequest(http.MethodGet, "https://example.org/data", nil)
	proxy, err := transport.Proxy(request)
	if err != nil || proxy.String() != "http://configured.example:8080" {
		t.Fatalf("%v %v", proxy, err)
	}
	request, _ = http.NewRequest(http.MethodGet, "http://127.0.0.1:50081/health", nil)
	proxy, err = transport.Proxy(request)
	if err != nil || proxy != nil {
		t.Fatalf("local request used proxy: %v %v", proxy, err)
	}
}

func TestMacSettingsOnlyUseEnabledProxy(t *testing.T) {
	settings := macSettings(`<dictionary> {
 ExceptionsList : <array> {
 0 : *.local
 1 : localhost
 }
 HTTPEnable : 0
 HTTPPort : 7890
 HTTPProxy : 127.0.0.1
 HTTPSEnable : 1
 HTTPSPort : 1080
 HTTPSProxy : ::1
}`)
	if settings.HTTPProxy != "" || settings.HTTPSProxy != "http://[::1]:1080" || settings.NoProxy != "*.local,localhost" {
		t.Fatal(settings)
	}
}
