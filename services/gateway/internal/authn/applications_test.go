package authn

import (
	"bytes"
	"net/url"
	"strings"
	"testing"
)

func TestStandaloneApplicationBindings(t *testing.T) {
	service := &Service{
		hydra:              &provider{issuer: "https://hydra.example/"},
		applicationClients: map[string][]string{"app-one": {"https://product.example/callback"}},
	}
	query := url.Values{
		"client_id": {"app-one"}, "redirect_uri": {"https://product.example/callback"},
		"response_type": {"code"}, "scope": {"openid"}, "state": {strings.Repeat("s", 26)},
		"nonce": {strings.Repeat("n", 26)}, "code_challenge": {strings.Repeat("c", 43)}, "code_challenge_method": {"S256"},
	}
	request := hydraRequest{}
	request.Client.ID = "app-one"
	request.RequestURL = "https://hydra.example/oauth2/auth?" + query.Encode()
	if _, ok := service.applicationRequest(request); !ok {
		t.Fatal("enrolled application request rejected")
	}
	for _, change := range []func(url.Values){
		func(q url.Values) { q.Set("redirect_uri", "https://untrusted.example/callback") },
		func(q url.Values) { q.Set("scope", "openid gramstack.access") },
		func(q url.Values) { q.Set("code_challenge_method", "plain") },
		func(q url.Values) { q.Del("nonce") },
		func(q url.Values) { q.Add("state", strings.Repeat("x", 26)) },
		func(q url.Values) { q.Set("client_id", "consumer-one") },
	} {
		changed, _ := url.ParseQuery(query.Encode())
		change(changed)
		invalid := request
		invalid.RequestURL = "https://hydra.example/oauth2/auth?" + changed.Encode()
		if _, ok := service.applicationRequest(invalid); ok {
			t.Fatal("unbound application request accepted")
		}
	}
	request.Client.ID = "unregistered"
	if _, ok := service.applicationRequest(request); ok {
		t.Fatal("unenrolled application accepted")
	}
}
func TestApplicationSubjectsAreStableAndPairwise(t *testing.T) {
	service := &Service{applicationSubjectKey: bytes.Repeat([]byte{7}, 32)}
	subject := service.applicationSubject("app-one", "https://identity.example", "private-user")
	if !strings.HasPrefix(subject, "app_") || strings.Contains(subject, "private-user") || service.applicationSubject("app-one", "https://identity.example", "private-user") != subject {
		t.Fatal("unstable or exposed identity")
	}
	if service.applicationSubject("app-two", "https://identity.example", "private-user") == subject || service.applicationSubject("app-one", "https://other.example", "private-user") == subject {
		t.Fatal("identity crossed client or provider boundary")
	}
}
