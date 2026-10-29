// Package networkproxy follows explicit, environment, then enabled macOS system proxy settings.
package networkproxy

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"golang.org/x/net/http/httpproxy"
)

func Transport(explicit string) *http.Transport {
	settings := httpproxy.FromEnvironment()
	if explicit != "" {
		settings.HTTPProxy, settings.HTTPSProxy = explicit, explicit
	} else if runtime.GOOS == "darwin" && settings.HTTPProxy == "" && settings.HTTPSProxy == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if raw, err := exec.CommandContext(ctx, "/usr/sbin/scutil", "--proxy").Output(); err == nil {
			system := macSettings(string(raw))
			settings.HTTPProxy, settings.HTTPSProxy = system.HTTPProxy, system.HTTPSProxy
			settings.NoProxy = strings.Trim(settings.NoProxy+","+system.NoProxy, ",")
		}
	}
	proxy := settings.ProxyFunc()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = func(request *http.Request) (*url.URL, error) { return proxy(request.URL) }
	return transport
}

func macSettings(raw string) httpproxy.Config {
	values := map[string]string{}
	exceptions := []string{}
	inExceptions := false
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ExceptionsList :") {
			inExceptions = true
			continue
		}
		if line == "}" {
			inExceptions = false
			continue
		}
		pair := strings.SplitN(line, " : ", 2)
		if len(pair) != 2 {
			continue
		}
		if inExceptions {
			exceptions = append(exceptions, pair[1])
			continue
		}
		values[pair[0]] = pair[1]
	}
	address := func(prefix string) string {
		if values[prefix+"Enable"] != "1" || values[prefix+"Proxy"] == "" || values[prefix+"Port"] == "" {
			return ""
		}
		return "http://" + net.JoinHostPort(values[prefix+"Proxy"], values[prefix+"Port"])
	}
	return httpproxy.Config{HTTPProxy: address("HTTP"), HTTPSProxy: address("HTTPS"), NoProxy: strings.Join(exceptions, ",")}
}
