package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	oidcadapter "github.com/theflywheel/GRAMStack/adapters/auth/oidc"
)

// fixtureSubject performs real OIDC verification solely for the fictional local
// fixture being provisioned. Registry mock bindings are operator-owned config;
// no production ownership assertion is manufactured by the gateway.
func fixtureSubject(profile oidcadapter.Profile, credentials oidcadapter.Credentials, callback, person, ui, api string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	adapter, err := oidcadapter.New(ctx, profile, credentials, callback, oidcadapter.Options{AllowLoopback: true, AllowDemo: true})
	if err != nil {
		return "", err
	}
	tx := oidcadapter.Transaction{ID: rand.Text(), State: rand.Text(), Nonce: rand.Text(), Verifier: rand.Text() + rand.Text()}
	location, err := adapter.Begin(tx)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, "GET", location, nil)
	if err != nil {
		return "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return "", errors.New("fixture authorization unavailable")
	}
	response.Body.Close()
	start, err := url.Parse(response.Header.Get("Location"))
	if err != nil || response.StatusCode != 302 || start.Scheme+"://"+start.Host != ui || start.User != nil || start.Path != "/signin" || start.Query().Get("executionId") == "" || start.Query().Get("authId") == "" {
		return "", errors.New("unexpected fixture authorization redirect")
	}
	post := func(path string, payload, output any) error {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		request, err := http.NewRequestWithContext(ctx, "POST", api+path, bytes.NewReader(raw))
		if err != nil {
			return err
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := client.Do(request)
		if err != nil {
			return errors.New("fixture authentication unavailable")
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			return errors.New("fixture authentication rejected")
		}
		if json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(output) != nil {
			return errors.New("invalid fixture authentication response")
		}
		return nil
	}
	type flowResult struct {
		Challenge string `json:"challengeToken"`
		Status    string `json:"flowStatus"`
		Assertion string `json:"assertion"`
		Data      struct {
			Inputs []struct {
				ID string `json:"identifier"`
			} `json:"inputs"`
			Actions []struct {
				Ref string `json:"ref"`
			} `json:"actions"`
		} `json:"data"`
	}
	var flow flowResult
	if err = post("/flow/execute", map[string]string{"executionId": start.Query().Get("executionId")}, &flow); err != nil {
		return "", err
	}
	for step := 0; step < 8 && flow.Status != "COMPLETE"; step++ {
		if flow.Status == "ERROR" || len(flow.Data.Actions) == 0 {
			return "", errors.New("fictional authentication did not complete")
		}
		inputs := map[string]string{}
		for _, input := range flow.Data.Inputs {
			switch input.ID {
			case "username":
				inputs[input.ID] = person
			case "otp":
				inputs[input.ID] = "111111"
			case "captcha_token":
				inputs[input.ID] = "local-demo-captcha-token"
			default:
				return "", errors.New("unsupported fictional authentication challenge")
			}
		}
		payload := map[string]any{"executionId": start.Query().Get("executionId"), "challengeToken": flow.Challenge, "action": flow.Data.Actions[0].Ref, "inputs": inputs}
		flow = flowResult{}
		if err = post("/flow/execute", payload, &flow); err != nil {
			return "", err
		}
	}
	if flow.Status != "COMPLETE" || flow.Assertion == "" {
		return "", errors.New("fictional authentication did not complete")
	}
	var completed struct {
		Redirect string `json:"redirect_uri"`
		Camel    string `json:"redirectUri"`
	}
	if err = post("/oauth2/auth/callback", map[string]string{"authId": start.Query().Get("authId"), "assertion": flow.Assertion}, &completed); err != nil {
		return "", err
	}
	target := completed.Redirect
	if target == "" {
		target = completed.Camel
	}
	result, err := url.Parse(target)
	expected, expectedErr := url.Parse(callback)
	if err != nil || expectedErr != nil || result.Scheme != expected.Scheme || result.Host != expected.Host || result.Path != expected.Path || result.Query().Get("state") != tx.State || result.Query().Get("code") == "" || result.Query().Get("error") != "" {
		return "", errors.New("invalid fictional callback binding")
	}
	principal, err := adapter.Complete(ctx, result.Query().Get("code"), tx)
	if err != nil {
		return "", err
	}
	return principal.Subject, nil
}
