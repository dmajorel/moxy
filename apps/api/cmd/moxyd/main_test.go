package main

import (
	"strings"
	"testing"
)

// lookupFrom turns a map into the os.LookupEnv shape, so that the test says
// what the environment holds without touching the environment of the process.
func lookupFrom(env map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := env[name]
		return value, ok
	}
}

func TestProxyEnvVars(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want []string
	}{
		{
			name: "empty environment",
			env:  nil,
		},
		{
			name: "https_proxy set",
			env:  map[string]string{"https_proxy": "http://proxy.invalid:3128"},
			want: []string{"https_proxy"},
		},
		{
			name: "several variables set",
			env: map[string]string{
				"HTTPS_PROXY": "http://proxy.invalid:3128",
				"ALL_PROXY":   "socks5://proxy.invalid:1080",
			},
			want: []string{"HTTPS_PROXY", "ALL_PROXY"},
		},
		{
			// An unset variable and one set to the empty string mean the same
			// thing to net/http, and must mean the same thing here.
			name: "set but empty",
			env:  map[string]string{"HTTPS_PROXY": "   "},
		},
		{
			// HTTP_PROXY would never have applied: node urls are https only.
			name: "http_proxy is not one of them",
			env:  map[string]string{"HTTP_PROXY": "http://proxy.invalid:3128"},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := proxyEnvVars(lookupFrom(tt.env))
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("proxyEnvVars() = %v, want %v", got, tt.want)
			}
		})
	}
}
