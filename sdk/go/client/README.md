# GRAMStack Go client

Independent application SDK module. Configure the registered consumer and trusted
endpoints on the backend.

```go
// import gramstack "github.com/theflywheel/GRAMStack/sdk/go/client"
client, err := gramstack.New(gramstack.Config{
    PublicOrigin: gatewayOrigin,
    TokenURL: tokenURL,
    ClientID: clientID,
    ClientSecret: clientSecret,
    DataOrigin: privateDataOrigin,
    DataServerName: dataTLSName,
    CA: caPEM, Certificate: clientCertPEM, Key: clientKeyPEM,
})
if err != nil { return err }

accepted, err := client.Create(ctx, "application-482", gramstack.AccessRequest{
    Registry: "lokos", Resource: "shg.membership",
    SubjectHint: gramstack.SubjectHint{Type: "member_reference", Value: memberReference},
    Fields: []string{"membership_status"}, Purpose: "credit_eligibility",
    Access: gramstack.Access{Mode: "single_use", ValidForSeconds: 300},
    RecipientTerms: gramstack.RecipientTerms{RetentionPolicyID: "terms-v1"},
})
if err != nil { return err }
// Present accepted.InteractionURL, then observe client.Status or WaitForConsent.
// Check that the resulting state is CONSENTED before issuing a grant.
grant, err := client.IssueGrant(ctx, accepted.RequestID, "application-482-grant")
if err != nil { return err }
result, err := client.Retrieve(ctx, grant, []string{"membership_status"})
```

Keep the grant on the backend. A handle is locally one-attempt even when copied
or used concurrently. Applications retain their own durable attempt state across
restarts. Transport failures leave delivery uncertain.

## Standalone sign-in

`NewAuth(ctx, AuthConfig, LoginStore)` creates a pinned OIDC client.
`Login(w,r)` starts browser-bound PKCE sign-in; `Callback(w,r)` consumes the
one-use state and returns a verified application-scoped `Identity`.

The store implements `SaveLogin` and `TakeLogin`; see SakhiCredit's
`internal/application/login.go` for shared encrypted PostgreSQL storage.
`NewMemoryLoginStore()` is an explicit development-only store.

```go
auth, err := gramstack.NewAuth(ctx, gramstack.AuthConfig{
    Issuer: hydraIssuer, ClientID: applicationClientID,
    ClientSecret: applicationClientSecret,
    RedirectURL: applicationOrigin + "/auth/gramstack/callback",
}, loginStore)
```

Enroll that exact client and callback with the operator first. The application's
session is created only after the callback returns a verified identity.
