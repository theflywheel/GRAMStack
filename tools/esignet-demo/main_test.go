package main

import (
	"net/url"
	"os"
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

func TestRailwayOnboardingPinsPrivateTargetsAndCallback(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRAMSTACK_ENV", "production")
	if _, err := railwayDemoTarget("", dir); err == nil {
		t.Fatal("production mode accepted")
	}
	t.Setenv("GRAMSTACK_ENV", "demo")
	if _, err := railwayDemoTarget("https://other.example/auth/provider/callback", dir); err == nil {
		t.Fatal("callback override accepted")
	}
	t.Setenv("ESIGNET_DEMO_API_PORT", "1234")
	target, err := railwayDemoTarget("", dir)
	if err != nil {
		t.Fatal(err)
	}
	if target.loopback || target.api != "http://esignet.railway.internal:8080" ||
		target.mock != "http://esignet-mock.railway.internal:8082" ||
		target.issuer != "https://gramstack-esignet-b9b69819.up.railway.app" ||
		target.callback != "https://gramstack-gateway-b9b69819.up.railway.app/auth/provider/callback" {
		t.Fatal("Railway destination boundary changed")
	}
}

func TestHostedFixtureOriginWithExplicitHTTPSPort(t *testing.T) {
	const origin = "https://identity.example"
	for _, tc := range []struct {
		location string
		want     bool
	}{
		{origin + "/signin", true},
		{origin + ":443/signin", true},
		{origin + ":8443/signin", false},
		{"http://identity.example:443/signin", false},
		{"https://other.example:443/signin", false},
	} {
		location, err := url.Parse(tc.location)
		if err != nil || sameFixtureOrigin(location, origin) != tc.want {
			t.Errorf("origin validation for %q did not match %t", tc.location, tc.want)
		}
	}
}
