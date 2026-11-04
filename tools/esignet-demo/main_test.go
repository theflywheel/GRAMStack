package main

import (
	"strings"
	"testing"
)

func TestOnboardingCannotTargetProductionOrRemoteServices(t *testing.T) {
	t.Setenv("GRAMSTACK_ENV", "production")
	if err := onboard("http://127.0.0.1:19999/auth/provider/callback", t.TempDir()); err == nil || !strings.Contains(err.Error(), "development") {
		t.Fatal("onboarding allowed outside development", err)
	}
	t.Setenv("GRAMSTACK_ENV", "development")
	for _, callback := range []string{
		"https://example.com/auth/provider/callback",
		"http://10.0.0.1/auth/provider/callback",
		"http://localhost/auth/provider/callback",
		"http://127.0.0.1/wrong-callback",
		"http://127.0.0.1/auth/provider/callback?redirect=remote",
		"http://user:secret@127.0.0.1/auth/provider/callback",
		"http://127.0.0.1/auth/provider/callback#fragment",
	} {
		t.Run(callback, func(t *testing.T) {
			if err := onboard(callback, t.TempDir()); err == nil {
				t.Fatal("unsafe callback accepted")
			}
		})
	}
	if err := onboard("http://127.0.0.1:19999/auth/provider/callback", ""); err == nil {
		t.Fatal("key output directory must be explicit")
	}
}

func TestConfigurableOriginsStayOnLoopback(t *testing.T) {
	for _, value := range []string{"127.0.0.1:80", "80/other", "80@remote.example", "+80", "080", "0", "65536"} {
		t.Setenv("ESIGNET_DEMO_API_PORT", value)
		if _, err := loopbackOrigin("ESIGNET_DEMO_API_PORT", "19080"); err == nil {
			t.Fatalf("unsafe port accepted: %q", value)
		}
	}
	t.Setenv("ESIGNET_DEMO_API_PORT", "29080")
	if origin, err := loopbackOrigin("ESIGNET_DEMO_API_PORT", "19080"); err != nil || origin != "http://127.0.0.1:29080" {
		t.Fatal(origin, err)
	}
}
