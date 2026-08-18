package config

import "testing"

// The loopback allowance widens CORS, so the parse has to be strict: a
// substring check on "localhost" would admit every hostname below.
func TestIsLoopbackOrigin(t *testing.T) {
	allowed := []string{
		"http://localhost:5173",
		"http://localhost:5174",
		"http://localhost",
		"https://localhost:4173",
		"http://127.0.0.1:8080",
		"http://[::1]:5173",
	}
	for _, o := range allowed {
		if !IsLoopbackOrigin(o) {
			t.Errorf("IsLoopbackOrigin(%q) = false, want true", o)
		}
	}

	rejected := []string{
		"",
		"null",
		"http://localhost.attacker.example",
		"http://notlocalhost",
		"http://evil.example/localhost",
		"http://localhost@evil.example",
		"https://127.0.0.1.attacker.example",
		"http://10.0.0.1:5173",
		"http://192.168.4.130:5174",
		"file:///etc/passwd",
		"javascript:alert(1)",
		"http://[2001:db8::1]:5173",
	}
	for _, o := range rejected {
		if IsLoopbackOrigin(o) {
			t.Errorf("IsLoopbackOrigin(%q) = true, want false", o)
		}
	}
}

// The widening must never apply to a configured deployment.
func TestLoopbackAllowanceIsDemoOnlyAndNeverOverridesAnExplicitList(t *testing.T) {
	cases := []struct {
		name    string
		demo    bool
		origins string
		want    bool
	}{
		{"demo, nothing configured", true, "", true},
		{"demo, but an explicit list", true, "https://marketmate.example", false},
		{"live, nothing configured", false, "", false},
		{"live, explicit list", false, "https://marketmate.example", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ALLOWED_ORIGINS", tc.origins)
			if tc.demo {
				t.Setenv("DEMO_MODE", "true")
			}
			cfg, err := LoadConfig()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.AllowLoopbackOrigins != tc.want {
				t.Errorf("AllowLoopbackOrigins = %v, want %v", cfg.AllowLoopbackOrigins, tc.want)
			}
			if tc.origins != "" && (len(cfg.AllowedOrigins) != 1 || cfg.AllowedOrigins[0] != tc.origins) {
				t.Errorf("AllowedOrigins = %v, want exactly [%s]", cfg.AllowedOrigins, tc.origins)
			}
		})
	}
}
