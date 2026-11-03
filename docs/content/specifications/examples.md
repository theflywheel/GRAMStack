---
title: Contract examples
description: Versioned GRAMStack implementation contracts and conformance requirements.
---

Specification version **0.3.0** · [Specifications index](/specifications/) · [Download source](https://docs.gramstack.try-dough.com/static/specs/0.3.0/examples.json)

```json
{
  "description": "Fictional shape fixtures, not authentic signatures or cryptographic test vectors. Repeated a-digests are placeholders, not canonicalization vectors.",
  "cases": [
    {
      "name": "AccessRequest",
      "schema": "AccessRequest",
      "valid": true,
      "value": {
        "registry": "lokos",
        "resource": "shg.membership",
        "subject_hint": {
          "type": "member_reference",
          "value": "member-12345"
        },
        "fields": [
          "membership_status",
          "savings_summary"
        ],
        "purpose": "credit_eligibility",
        "access": {
          "mode": "single_use",
          "valid_for_seconds": 300
        },
        "recipient_terms": {
          "retention_policy_id": "sakhi-retention-v1"
        }
      }
    },
    {
      "name": "RequestSnapshot",
      "schema": "RequestSnapshot",
      "valid": true,
      "value": {
        "request_id": "req-1",
        "consumer_id": "sakhicredit",
        "created_at": "2026-11-01T10:00:00Z",
        "expires_at": "2026-11-01T10:05:00Z",
        "recipient_terms_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "request": {
          "registry": "lokos",
          "resource": "shg.membership",
          "subject_hint": {
            "type": "member_reference",
            "value": "member-12345"
          },
          "fields": [
            "membership_status",
            "savings_summary"
          ],
          "purpose": "credit_eligibility",
          "access": {
            "mode": "single_use",
            "valid_for_seconds": 300
          },
          "recipient_terms": {
            "retention_policy_id": "sakhi-retention-v1"
          }
        }
      }
    },
    {
      "name": "RequestAccepted",
      "schema": "RequestAccepted",
      "valid": true,
      "value": {
        "request_id": "req-1",
        "status": "AUTHENTICATION_PENDING",
        "status_url": "https://gateway.example.org/v1/access-requests/req-1",
        "interaction_url": "https://gateway.example.org/interactions/req-1",
        "expires_at": "2026-11-01T10:05:00Z"
      }
    },
    {
      "name": "RequestStatus",
      "schema": "RequestStatus",
      "valid": true,
      "value": {
        "request_id": "req-1",
        "status": "CONSENTED",
        "version": 4,
        "next_action": "request_grant",
        "expires_at": "2026-11-01T10:05:00Z"
      }
    },
    {
      "name": "PrincipalContext",
      "schema": "PrincipalContext",
      "valid": true,
      "value": {
        "context_id": "principal-1",
        "issuer": "https://idp.example.org",
        "provider_subject": "opaque-subject",
        "authentication_transaction_id": "auth-1",
        "authenticated_at": "2026-11-01T10:00:00Z",
        "expires_at": "2026-11-01T10:05:00Z"
      }
    },
    {
      "name": "VerifiedSubjectContext",
      "schema": "VerifiedSubjectContext",
      "valid": true,
      "value": {
        "context_id": "subject-context-1",
        "registry": "lokos",
        "request_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "principal_context_ref": "principal-1",
        "source_mapping_ref": "mapping-1",
        "verification_method": "registry-approved-demo",
        "verified_at": "2026-11-01T10:00:00Z",
        "expires_at": "2026-11-01T10:05:00Z"
      }
    },
    {
      "name": "ConsentRequest",
      "schema": "ConsentRequest",
      "valid": true,
      "value": {
        "snapshot": {
          "request_id": "req-1",
          "consumer_id": "sakhicredit",
          "created_at": "2026-11-01T10:00:00Z",
          "expires_at": "2026-11-01T10:05:00Z",
          "recipient_terms_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
          "request": {
            "registry": "lokos",
            "resource": "shg.membership",
            "subject_hint": {
              "type": "member_reference",
              "value": "member-12345"
            },
            "fields": [
              "membership_status",
              "savings_summary"
            ],
            "purpose": "credit_eligibility",
            "access": {
              "mode": "single_use",
              "valid_for_seconds": 300
            },
            "recipient_terms": {
              "retention_policy_id": "sakhi-retention-v1"
            }
          }
        },
        "request_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "subject_context_ref": "subject-context-1"
      }
    },
    {
      "name": "Notice",
      "schema": "Notice",
      "valid": true,
      "value": {
        "consent_request_id": "cr-1",
        "request_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "notice_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "notice_version": "notice-v1",
        "language": "en",
        "text": "Share membership status and savings summary with SakhiCredit once for credit eligibility. Valid for five minutes; recipient terms sakhi-retention-v1 apply. Withdraw through this interaction.",
        "expires_at": "2026-11-01T10:05:00Z"
      }
    },
    {
      "name": "Decision",
      "schema": "Decision",
      "valid": true,
      "value": {
        "request_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "notice_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "notice_version": "notice-v1",
        "language": "en",
        "decision": "approve"
      }
    },
    {
      "name": "ConsentStatus",
      "schema": "ConsentStatus",
      "valid": true,
      "value": {
        "consent_id": "consent-1",
        "status": "ACTIVE",
        "status_version": 2,
        "use": "AVAILABLE",
        "valid_until": "2026-11-01T10:05:00Z"
      }
    },
    {
      "name": "ConsentArtefact",
      "schema": "ConsentArtefact",
      "valid": true,
      "value": {
        "consent_id": "consent-1",
        "schema_version": "gramstack.consent.v1",
        "issuer": "https://consent.example.org",
        "request_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "subject_context_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "consumer": "sakhicredit",
        "provider": "lokos",
        "resource": "shg.membership",
        "fields": [
          "membership_status",
          "savings_summary"
        ],
        "purpose": "credit_eligibility",
        "frequency": "ONCE",
        "recipient_terms_version": "sakhi-retention-v1",
        "recipient_terms_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "notice_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "approved_at": "2026-11-01T10:00:00Z",
        "valid_until": "2026-11-01T10:05:00Z",
        "status_endpoint": "https://consent.example.org/internal/consents/consent-1/status"
      }
    },
    {
      "name": "SignedEnvelope",
      "schema": "SignedEnvelope",
      "valid": true,
      "value": {
        "jws": "ZXhhbXBsZQ.ZXhhbXBsZQ.ZXhhbXBsZQ"
      }
    },
    {
      "name": "ConsentEvent",
      "schema": "ConsentEvent",
      "valid": true,
      "value": {
        "event_id": "event-1",
        "type": "consent.active",
        "consent_id": "consent-1",
        "access_request_id": "req-1",
        "request_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "status_version": 2,
        "occurred_at": "2026-11-01T10:00:00Z"
      }
    },
    {
      "name": "RetrievalGrant",
      "schema": "RetrievalGrant",
      "valid": true,
      "value": {
        "iss": "https://authz.example.org",
        "aud": "urn:gramstack:adapter:lokos",
        "sub": "subject-context-1",
        "client_id": "sakhicredit",
        "jti": "grant-1",
        "iat": 1793527200,
        "exp": 1793527500,
        "request_id": "req-1",
        "request_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "resource": "shg.membership",
        "fields": [
          "membership_status",
          "savings_summary"
        ],
        "purpose": "credit_eligibility",
        "consent_ref": "consent-1",
        "subject_context_ref": "subject-context-1",
        "policy_version": "lokos-v1",
        "cnf": {
          "x5t#S256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
        }
      }
    },
    {
      "name": "GrantResponse",
      "schema": "GrantResponse",
      "valid": true,
      "value": {
        "grant": "ZXhhbXBsZQ.ZXhhbXBsZQ.ZXhhbXBsZQ",
        "expires_at": "2026-11-01T10:05:00Z",
        "token_type": "Bearer"
      }
    },
    {
      "name": "PresenterContext",
      "schema": "PresenterContext",
      "valid": true,
      "value": {
        "certificate_thumbprint": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "authenticated_client_id": "sakhicredit"
      }
    },
    {
      "name": "RedemptionResult",
      "schema": "RedemptionResult",
      "valid": true,
      "value": {
        "redemption_id": "redemption-1",
        "attempt_id": "attempt-1",
        "first_redemption": true,
        "reserved_at": "2026-11-01T10:00:00Z"
      }
    },
    {
      "name": "MembershipData",
      "schema": "MembershipData",
      "valid": true,
      "value": {
        "membership_status": "active",
        "savings_summary": {
          "amount": 12500,
          "currency": "INR"
        }
      }
    },
    {
      "name": "EvidenceEvent",
      "schema": "EvidenceEvent",
      "valid": true,
      "value": {
        "event_id": "evidence-1",
        "request_id": "req-1",
        "request_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "consent_id": "consent-1",
        "grant_jti": "grant-1",
        "adapter_id": "lokos-adapter",
        "attempt_id": "attempt-1",
        "redemption_id": "redemption-1",
        "fields": [
          "membership_status",
          "savings_summary"
        ],
        "outcome": "release_ready",
        "response_commitment": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "occurred_at": "2026-11-01T10:00:00Z"
      }
    },
    {
      "name": "EvidenceAcknowledgement",
      "schema": "EvidenceAcknowledgement",
      "valid": true,
      "value": {
        "receipt_id": "receipt-1",
        "evidence_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "accepted_at": "2026-11-01T10:00:00Z",
        "anchor_status": "pending"
      }
    },
    {
      "name": "Receipt",
      "schema": "Receipt",
      "valid": true,
      "value": {
        "request_id": "req-1",
        "status": "COMPLETED",
        "consent_id": "consent-1",
        "receipt_ids": [
          "receipt-1"
        ]
      }
    },
    {
      "name": "AdapterManifest",
      "schema": "AdapterManifest",
      "valid": true,
      "value": {
        "adapter_id": "lokos-adapter",
        "registry": "lokos",
        "contract_version": "0.3.0",
        "audience": "urn:gramstack:adapter:lokos",
        "authentication": "mtls",
        "signing_key_ids": [
          "lokos-key-1"
        ],
        "verification_methods": [
          "registry-approved-demo"
        ],
        "capabilities": [
          "atomic-redemption",
          "evidence-before-release"
        ],
        "resources": [
          {
            "name": "shg.membership",
            "version": "1",
            "fields": [
              "membership_status",
              "savings_summary"
            ]
          }
        ]
      }
    },
    {
      "name": "RegistryPolicy",
      "schema": "RegistryPolicy",
      "valid": true,
      "value": {
        "registry": "lokos",
        "version": "lokos-v1",
        "consumer_id": "sakhicredit",
        "purpose": "credit_eligibility",
        "resource": "shg.membership",
        "fields": [
          "membership_status",
          "savings_summary"
        ],
        "require_verified_subject": true,
        "require_human_consent": true,
        "max_grant_seconds": 300,
        "max_disclosures": 1
      }
    },
    {
      "name": "Error",
      "schema": "Error",
      "valid": true,
      "value": {
        "error": {
          "code": "grant_consumed",
          "message": "Disclosure already reserved.",
          "request_id": "req-1",
          "retryable": false
        }
      }
    },
    {
      "name": "RedemptionRequest",
      "schema": "RedemptionRequest",
      "valid": true,
      "value": {
        "attempt_id": "attempt-1",
        "grant": "ZXhhbXBsZQ.ZXhhbXBsZQ.ZXhhbXBsZQ",
        "request_id": "req-1",
        "request_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "presenter": {
          "certificate_thumbprint": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
          "authenticated_client_id": "sakhicredit"
        }
      }
    },
    {
      "name": "RetrievalRequest",
      "schema": "RetrievalRequest",
      "valid": true,
      "value": {
        "attempt_id": "attempt-1",
        "snapshot": {
          "request_id": "req-1",
          "consumer_id": "sakhicredit",
          "created_at": "2026-11-01T10:00:00Z",
          "expires_at": "2026-11-01T10:05:00Z",
          "recipient_terms_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
          "request": {
            "registry": "lokos",
            "resource": "shg.membership",
            "subject_hint": {
              "type": "member_reference",
              "value": "member-12345"
            },
            "fields": [
              "membership_status",
              "savings_summary"
            ],
            "purpose": "credit_eligibility",
            "access": {
              "mode": "single_use",
              "valid_for_seconds": 300
            },
            "recipient_terms": {
              "retention_policy_id": "sakhi-retention-v1"
            }
          }
        },
        "request_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "grant": "ZXhhbXBsZQ.ZXhhbXBsZQ.ZXhhbXBsZQ",
        "presenter": {
          "certificate_thumbprint": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
          "authenticated_client_id": "sakhicredit"
        }
      }
    },
    {
      "name": "DataResponse",
      "schema": "DataResponse",
      "valid": true,
      "value": {
        "request_id": "req-1",
        "data": {
          "membership_status": "active",
          "savings_summary": {
            "amount": 12500,
            "currency": "INR"
          }
        },
        "receipt_id": "receipt-1"
      }
    },
    {
      "name": "unknown-field",
      "schema": "AccessRequest",
      "valid": false,
      "value": {
        "registry": "lokos",
        "resource": "shg.membership",
        "subject_hint": {
          "type": "member_reference",
          "value": "member-12345"
        },
        "fields": [
          "membership_status",
          "savings_summary"
        ],
        "purpose": "credit_eligibility",
        "access": {
          "mode": "single_use",
          "valid_for_seconds": 300
        },
        "recipient_terms": {
          "retention_policy_id": "sakhi-retention-v1"
        },
        "extra": "forbidden"
      }
    },
    {
      "name": "duplicate-fields",
      "schema": "AccessRequest",
      "valid": false,
      "value": {
        "registry": "lokos",
        "resource": "shg.membership",
        "subject_hint": {
          "type": "member_reference",
          "value": "member-12345"
        },
        "fields": [
          "membership_status",
          "membership_status"
        ],
        "purpose": "credit_eligibility",
        "access": {
          "mode": "single_use",
          "valid_for_seconds": 300
        },
        "recipient_terms": {
          "retention_policy_id": "sakhi-retention-v1"
        }
      }
    },
    {
      "name": "multi-use",
      "schema": "AccessRequest",
      "valid": false,
      "value": {
        "registry": "lokos",
        "resource": "shg.membership",
        "subject_hint": {
          "type": "member_reference",
          "value": "member-12345"
        },
        "fields": [
          "membership_status",
          "savings_summary"
        ],
        "purpose": "credit_eligibility",
        "access": {
          "mode": "repeat",
          "valid_for_seconds": 300
        },
        "recipient_terms": {
          "retention_policy_id": "sakhi-retention-v1"
        }
      }
    },
    {
      "name": "bad-digest",
      "schema": "ConsentEvent",
      "valid": false,
      "value": {
        "event_id": "event-1",
        "type": "consent.active",
        "consent_id": "consent-1",
        "access_request_id": "req-1",
        "request_digest": "sha256:request-A",
        "status_version": 2,
        "occurred_at": "2026-11-01T10:00:00Z"
      }
    },
    {
      "name": "no-human-consent",
      "schema": "RegistryPolicy",
      "valid": false,
      "value": {
        "registry": "lokos",
        "version": "lokos-v1",
        "consumer_id": "sakhicredit",
        "purpose": "credit_eligibility",
        "resource": "shg.membership",
        "fields": [
          "membership_status",
          "savings_summary"
        ],
        "require_verified_subject": true,
        "require_human_consent": false,
        "max_grant_seconds": 300,
        "max_disclosures": 1
      }
    },
    {
      "name": "invalid-signature-shape",
      "schema": "SignedEnvelope",
      "valid": false,
      "value": {
        "jws": "not-a-jws"
      }
    },
    {
      "name": "bad-timestamp",
      "schema": "Notice",
      "valid": false,
      "value": {
        "consent_request_id": "cr-1",
        "request_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "notice_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "notice_version": "notice-v1",
        "language": "en",
        "text": "Share membership status and savings summary with SakhiCredit once for credit eligibility. Valid for five minutes; recipient terms sakhi-retention-v1 apply. Withdraw through this interaction.",
        "expires_at": "tomorrow"
      }
    },
    {
      "name": "unversioned-adapter",
      "schema": "AdapterManifest",
      "valid": false,
      "value": {
        "adapter_id": "lokos-adapter",
        "registry": "lokos",
        "contract_version": "latest",
        "audience": "urn:gramstack:adapter:lokos",
        "authentication": "mtls",
        "signing_key_ids": [
          "lokos-key-1"
        ],
        "verification_methods": [
          "registry-approved-demo"
        ],
        "capabilities": [
          "atomic-redemption",
          "evidence-before-release"
        ],
        "resources": [
          {
            "name": "shg.membership",
            "version": "1",
            "fields": [
              "membership_status",
              "savings_summary"
            ]
          }
        ]
      }
    },
    {
      "name": "failed-without-response",
      "schema": "EvidenceEvent",
      "valid": true,
      "value": {
        "event_id": "evidence-1",
        "request_id": "req-1",
        "request_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "consent_id": "consent-1",
        "grant_jti": "grant-1",
        "adapter_id": "lokos-adapter",
        "attempt_id": "attempt-1",
        "redemption_id": "redemption-1",
        "fields": [
          "membership_status",
          "savings_summary"
        ],
        "outcome": "failed",
        "occurred_at": "2026-11-01T10:00:00Z"
      }
    },
    {
      "name": "release-without-response-commitment",
      "schema": "EvidenceEvent",
      "valid": false,
      "value": {
        "event_id": "evidence-1",
        "request_id": "req-1",
        "request_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "consent_id": "consent-1",
        "grant_jti": "grant-1",
        "adapter_id": "lokos-adapter",
        "attempt_id": "attempt-1",
        "redemption_id": "redemption-1",
        "fields": [
          "membership_status",
          "savings_summary"
        ],
        "outcome": "release_ready",
        "occurred_at": "2026-11-01T10:00:00Z"
      }
    }
  ]
}
```

[Repository source](https://github.com/theflywheel/GRAMStack/tree/main/specs/0.3.0/examples.json).
