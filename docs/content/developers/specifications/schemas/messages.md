---
title: Message schemas
description: Versioned GRAMStack implementation contracts and conformance requirements.
---

Specification version **0.3.0** · [Specifications index](/developers/specifications) · [Download source](https://docs.gramstack.try-dough.com/static/specs/0.3.0/schemas.json)

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://docs.gramstack.try-dough.com/static/specs/0.3.0/schemas.json",
  "title": "GRAMStack 0.3.0 messages",
  "$defs": {
    "Error": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "error"
      ],
      "properties": {
        "error": {
          "type": "object",
          "additionalProperties": false,
          "required": [
            "code",
            "message",
            "request_id",
            "retryable"
          ],
          "properties": {
            "code": {
              "type": "string",
              "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
            },
            "message": {
              "type": "string",
              "minLength": 1
            },
            "request_id": {
              "type": "string",
              "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
            },
            "retryable": {
              "type": "boolean"
            }
          }
        }
      }
    },
    "AccessRequest": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "registry",
        "resource",
        "subject_hint",
        "fields",
        "purpose",
        "access",
        "recipient_terms"
      ],
      "properties": {
        "registry": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "resource": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "subject_hint": {
          "type": "object",
          "additionalProperties": false,
          "required": [
            "type",
            "value"
          ],
          "properties": {
            "type": {
              "type": "string",
              "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
            },
            "value": {
              "type": "string",
              "minLength": 1
            }
          }
        },
        "fields": {
          "type": "array",
          "items": {
            "type": "string",
            "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
          },
          "minItems": 1,
          "uniqueItems": true
        },
        "purpose": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "access": {
          "type": "object",
          "additionalProperties": false,
          "required": [
            "mode",
            "valid_for_seconds"
          ],
          "properties": {
            "mode": {
              "const": "single_use"
            },
            "valid_for_seconds": {
              "type": "integer",
              "minimum": 1,
              "maximum": 300
            }
          }
        },
        "recipient_terms": {
          "type": "object",
          "additionalProperties": false,
          "required": [
            "retention_policy_id"
          ],
          "properties": {
            "retention_policy_id": {
              "type": "string",
              "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
            }
          }
        }
      }
    },
    "RequestSnapshot": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "request_id",
        "consumer_id",
        "created_at",
        "expires_at",
        "recipient_terms_digest",
        "request"
      ],
      "properties": {
        "request_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "consumer_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "created_at": {
          "type": "string",
          "format": "date-time"
        },
        "expires_at": {
          "type": "string",
          "format": "date-time"
        },
        "recipient_terms_digest": {
          "type": "string",
          "pattern": "^sha256:[0-9a-f]{64}$"
        },
        "request": {
          "$ref": "#/$defs/AccessRequest"
        }
      }
    },
    "RequestAccepted": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "request_id",
        "status",
        "status_url",
        "interaction_url",
        "expires_at"
      ],
      "properties": {
        "request_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "status": {
          "const": "AUTHENTICATION_PENDING"
        },
        "status_url": {
          "type": "string",
          "format": "uri"
        },
        "interaction_url": {
          "type": "string",
          "format": "uri"
        },
        "expires_at": {
          "type": "string",
          "format": "date-time"
        }
      }
    },
    "RequestStatus": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "request_id",
        "status",
        "version",
        "next_action",
        "expires_at"
      ],
      "properties": {
        "request_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "status": {
          "enum": [
            "AUTHENTICATION_PENDING",
            "BINDING_PENDING",
            "CONSENT_PENDING",
            "CONSENTED",
            "AUTHORIZED",
            "REDEEMING",
            "EVIDENCE_COMMITTED",
            "RELEASE_ATTEMPTED",
            "COMPLETED",
            "DELIVERY_UNKNOWN",
            "REJECTED",
            "DENIED",
            "WITHDRAWN",
            "EXPIRED",
            "FAILED",
            "CANCELLED"
          ]
        },
        "version": {
          "type": "integer",
          "minimum": 1
        },
        "next_action": {
          "enum": [
            "authenticate",
            "verify_subject",
            "review_consent",
            "request_grant",
            "retrieve",
            "wait",
            "none"
          ]
        },
        "expires_at": {
          "type": "string",
          "format": "date-time"
        },
        "receipt_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        }
      }
    },
    "PrincipalContext": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "context_id",
        "issuer",
        "provider_subject",
        "authentication_transaction_id",
        "authenticated_at",
        "expires_at"
      ],
      "properties": {
        "context_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "issuer": {
          "type": "string",
          "format": "uri"
        },
        "provider_subject": {
          "type": "string",
          "minLength": 1
        },
        "authentication_transaction_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "authenticated_at": {
          "type": "string",
          "format": "date-time"
        },
        "expires_at": {
          "type": "string",
          "format": "date-time"
        }
      }
    },
    "VerifiedSubjectContext": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "context_id",
        "registry",
        "request_digest",
        "principal_context_ref",
        "source_mapping_ref",
        "verification_method",
        "verified_at",
        "expires_at"
      ],
      "properties": {
        "context_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "registry": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "request_digest": {
          "type": "string",
          "pattern": "^sha256:[0-9a-f]{64}$"
        },
        "principal_context_ref": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "source_mapping_ref": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "verification_method": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "verified_at": {
          "type": "string",
          "format": "date-time"
        },
        "expires_at": {
          "type": "string",
          "format": "date-time"
        }
      }
    },
    "ConsentRequest": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "snapshot",
        "request_digest",
        "subject_context_ref"
      ],
      "properties": {
        "snapshot": {
          "$ref": "#/$defs/RequestSnapshot"
        },
        "request_digest": {
          "type": "string",
          "pattern": "^sha256:[0-9a-f]{64}$"
        },
        "subject_context_ref": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        }
      }
    },
    "Notice": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "consent_request_id",
        "request_digest",
        "notice_digest",
        "notice_version",
        "language",
        "text",
        "expires_at"
      ],
      "properties": {
        "consent_request_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "request_digest": {
          "type": "string",
          "pattern": "^sha256:[0-9a-f]{64}$"
        },
        "notice_digest": {
          "type": "string",
          "pattern": "^sha256:[0-9a-f]{64}$"
        },
        "notice_version": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "language": {
          "type": "string",
          "pattern": "^[a-z]{2,3}(-[A-Za-z0-9]+)*$"
        },
        "text": {
          "type": "string",
          "minLength": 1
        },
        "expires_at": {
          "type": "string",
          "format": "date-time"
        }
      }
    },
    "Decision": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "decision",
        "request_digest",
        "notice_digest",
        "notice_version",
        "language"
      ],
      "properties": {
        "decision": {
          "enum": [
            "approve",
            "reject"
          ]
        },
        "request_digest": {
          "type": "string",
          "pattern": "^sha256:[0-9a-f]{64}$"
        },
        "notice_digest": {
          "type": "string",
          "pattern": "^sha256:[0-9a-f]{64}$"
        },
        "notice_version": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "language": {
          "type": "string",
          "minLength": 1
        }
      }
    },
    "ConsentStatus": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "consent_id",
        "status",
        "status_version",
        "use",
        "valid_until"
      ],
      "properties": {
        "consent_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "status": {
          "enum": [
            "PENDING",
            "APPROVAL_RECORDED",
            "ACTIVE",
            "REJECTED",
            "WITHDRAWN",
            "EXPIRED",
            "EXHAUSTED"
          ]
        },
        "status_version": {
          "type": "integer",
          "minimum": 1
        },
        "use": {
          "enum": [
            "AVAILABLE",
            "RESERVED"
          ]
        },
        "valid_until": {
          "type": "string",
          "format": "date-time"
        }
      }
    },
    "ConsentArtefact": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "consent_id",
        "schema_version",
        "issuer",
        "request_digest",
        "subject_context_digest",
        "consumer",
        "provider",
        "resource",
        "fields",
        "purpose",
        "frequency",
        "recipient_terms_version",
        "recipient_terms_digest",
        "notice_digest",
        "approved_at",
        "valid_until",
        "status_endpoint"
      ],
      "properties": {
        "consent_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "schema_version": {
          "const": "gramstack.consent.v1"
        },
        "issuer": {
          "type": "string",
          "format": "uri"
        },
        "request_digest": {
          "type": "string",
          "pattern": "^sha256:[0-9a-f]{64}$"
        },
        "subject_context_digest": {
          "type": "string",
          "pattern": "^sha256:[0-9a-f]{64}$"
        },
        "consumer": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "provider": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "resource": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "fields": {
          "type": "array",
          "items": {
            "type": "string",
            "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
          },
          "minItems": 1,
          "uniqueItems": true
        },
        "purpose": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "frequency": {
          "const": "ONCE"
        },
        "recipient_terms_version": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "recipient_terms_digest": {
          "type": "string",
          "pattern": "^sha256:[0-9a-f]{64}$"
        },
        "notice_digest": {
          "type": "string",
          "pattern": "^sha256:[0-9a-f]{64}$"
        },
        "approved_at": {
          "type": "string",
          "format": "date-time"
        },
        "valid_until": {
          "type": "string",
          "format": "date-time"
        },
        "status_endpoint": {
          "type": "string",
          "format": "uri"
        }
      }
    },
    "SignedEnvelope": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "jws"
      ],
      "properties": {
        "jws": {
          "type": "string",
          "pattern": "^[A-Za-z0-9_-]+\\.[A-Za-z0-9_-]+\\.[A-Za-z0-9_-]+$",
          "description": "Compact JWS. Shape validation does not verify cryptographic authenticity."
        }
      }
    },
    "ConsentEvent": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "event_id",
        "type",
        "consent_id",
        "access_request_id",
        "request_digest",
        "status_version",
        "occurred_at"
      ],
      "properties": {
        "event_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "type": {
          "enum": [
            "consent.active",
            "consent.rejected",
            "consent.withdrawn",
            "consent.expired",
            "consent.exhausted"
          ]
        },
        "consent_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "access_request_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "request_digest": {
          "type": "string",
          "pattern": "^sha256:[0-9a-f]{64}$"
        },
        "status_version": {
          "type": "integer",
          "minimum": 1
        },
        "occurred_at": {
          "type": "string",
          "format": "date-time"
        }
      }
    },
    "RetrievalGrant": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "iss",
        "aud",
        "sub",
        "client_id",
        "jti",
        "iat",
        "exp",
        "request_id",
        "request_digest",
        "resource",
        "fields",
        "purpose",
        "consent_ref",
        "subject_context_ref",
        "policy_version",
        "cnf"
      ],
      "properties": {
        "iss": {
          "type": "string",
          "format": "uri"
        },
        "aud": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "sub": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "client_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "jti": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "iat": {
          "type": "integer",
          "minimum": 0
        },
        "exp": {
          "type": "integer",
          "minimum": 1
        },
        "request_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "request_digest": {
          "type": "string",
          "pattern": "^sha256:[0-9a-f]{64}$"
        },
        "resource": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "fields": {
          "type": "array",
          "items": {
            "type": "string",
            "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
          },
          "minItems": 1,
          "uniqueItems": true
        },
        "purpose": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "consent_ref": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "subject_context_ref": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "policy_version": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "cnf": {
          "type": "object",
          "additionalProperties": false,
          "required": [
            "x5t#S256"
          ],
          "properties": {
            "x5t#S256": {
              "type": "string",
              "pattern": "^[A-Za-z0-9_-]{43}$"
            }
          }
        }
      }
    },
    "GrantResponse": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "grant",
        "expires_at",
        "token_type"
      ],
      "properties": {
        "grant": {
          "type": "string",
          "pattern": "^[A-Za-z0-9_-]+\\.[A-Za-z0-9_-]+\\.[A-Za-z0-9_-]+$",
          "description": "Compact JWS. Shape validation does not verify cryptographic authenticity."
        },
        "expires_at": {
          "type": "string",
          "format": "date-time"
        },
        "token_type": {
          "const": "Bearer"
        }
      }
    },
    "PresenterContext": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "certificate_thumbprint",
        "authenticated_client_id"
      ],
      "properties": {
        "certificate_thumbprint": {
          "type": "string",
          "pattern": "^[A-Za-z0-9_-]{43}$"
        },
        "authenticated_client_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        }
      }
    },
    "RedemptionRequest": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "attempt_id",
        "grant",
        "request_id",
        "request_digest",
        "presenter"
      ],
      "properties": {
        "attempt_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "grant": {
          "type": "string",
          "pattern": "^[A-Za-z0-9_-]+\\.[A-Za-z0-9_-]+\\.[A-Za-z0-9_-]+$",
          "description": "Compact JWS. Shape validation does not verify cryptographic authenticity."
        },
        "request_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "request_digest": {
          "type": "string",
          "pattern": "^sha256:[0-9a-f]{64}$"
        },
        "presenter": {
          "$ref": "#/$defs/PresenterContext"
        }
      }
    },
    "RedemptionResult": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "redemption_id",
        "attempt_id",
        "first_redemption",
        "reserved_at"
      ],
      "properties": {
        "redemption_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "attempt_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "first_redemption": {
          "type": "boolean"
        },
        "reserved_at": {
          "type": "string",
          "format": "date-time"
        }
      }
    },
    "RetrievalRequest": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "attempt_id",
        "snapshot",
        "request_digest",
        "grant",
        "presenter"
      ],
      "properties": {
        "attempt_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "snapshot": {
          "$ref": "#/$defs/RequestSnapshot"
        },
        "request_digest": {
          "type": "string",
          "pattern": "^sha256:[0-9a-f]{64}$"
        },
        "grant": {
          "type": "string",
          "pattern": "^[A-Za-z0-9_-]+\\.[A-Za-z0-9_-]+\\.[A-Za-z0-9_-]+$",
          "description": "Compact JWS. Shape validation does not verify cryptographic authenticity."
        },
        "presenter": {
          "$ref": "#/$defs/PresenterContext"
        }
      }
    },
    "MembershipData": {
      "type": "object",
      "additionalProperties": false,
      "required": [],
      "properties": {
        "membership_status": {
          "enum": [
            "active",
            "inactive",
            "suspended"
          ]
        },
        "savings_summary": {
          "type": "object",
          "additionalProperties": false,
          "required": [
            "amount",
            "currency"
          ],
          "properties": {
            "amount": {
              "type": "number",
              "minimum": 0,
              "description": "Source-reported INR rupees."
            },
            "currency": {
              "const": "INR"
            }
          }
        }
      },
      "minProperties": 1
    },
    "DataResponse": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "request_id",
        "data",
        "receipt_id"
      ],
      "properties": {
        "request_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "data": {
          "$ref": "#/$defs/MembershipData"
        },
        "receipt_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        }
      }
    },
    "EvidenceEvent": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "event_id",
        "request_id",
        "request_digest",
        "consent_id",
        "grant_jti",
        "adapter_id",
        "attempt_id",
        "redemption_id",
        "fields",
        "outcome",
        "occurred_at"
      ],
      "properties": {
        "event_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "request_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "request_digest": {
          "type": "string",
          "pattern": "^sha256:[0-9a-f]{64}$"
        },
        "consent_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "grant_jti": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "adapter_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "attempt_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "redemption_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "fields": {
          "type": "array",
          "items": {
            "type": "string",
            "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
          },
          "minItems": 1,
          "uniqueItems": true
        },
        "outcome": {
          "enum": [
            "release_ready",
            "failed",
            "delivery_unknown"
          ]
        },
        "response_commitment": {
          "type": "string",
          "pattern": "^sha256:[0-9a-f]{64}$"
        },
        "occurred_at": {
          "type": "string",
          "format": "date-time"
        }
      },
      "if": {
        "properties": {
          "outcome": {
            "const": "release_ready"
          }
        }
      },
      "then": {
        "required": [
          "response_commitment"
        ],
        "properties": {
          "response_commitment": {
            "type": "string",
            "pattern": "^sha256:[0-9a-f]{64}$"
          }
        }
      }
    },
    "EvidenceAcknowledgement": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "receipt_id",
        "evidence_digest",
        "accepted_at",
        "anchor_status"
      ],
      "properties": {
        "receipt_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "evidence_digest": {
          "type": "string",
          "pattern": "^sha256:[0-9a-f]{64}$"
        },
        "accepted_at": {
          "type": "string",
          "format": "date-time"
        },
        "anchor_status": {
          "enum": [
            "pending",
            "anchored"
          ]
        }
      }
    },
    "Receipt": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "request_id",
        "status",
        "consent_id",
        "receipt_ids"
      ],
      "properties": {
        "request_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "status": {
          "enum": [
            "AUTHENTICATION_PENDING",
            "BINDING_PENDING",
            "CONSENT_PENDING",
            "CONSENTED",
            "AUTHORIZED",
            "REDEEMING",
            "EVIDENCE_COMMITTED",
            "RELEASE_ATTEMPTED",
            "COMPLETED",
            "DELIVERY_UNKNOWN",
            "REJECTED",
            "DENIED",
            "WITHDRAWN",
            "EXPIRED",
            "FAILED"
          ]
        },
        "consent_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "receipt_ids": {
          "type": "array",
          "items": {
            "type": "string",
            "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
          },
          "uniqueItems": true
        }
      }
    },
    "AdapterManifest": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "adapter_id",
        "registry",
        "contract_version",
        "audience",
        "authentication",
        "signing_key_ids",
        "verification_methods",
        "capabilities",
        "resources"
      ],
      "properties": {
        "adapter_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "registry": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "contract_version": {
          "const": "0.3.0"
        },
        "audience": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "authentication": {
          "const": "mtls"
        },
        "signing_key_ids": {
          "type": "array",
          "items": {
            "type": "string",
            "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
          },
          "minItems": 1,
          "uniqueItems": true
        },
        "verification_methods": {
          "type": "array",
          "items": {
            "type": "string",
            "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
          },
          "minItems": 1,
          "uniqueItems": true
        },
        "capabilities": {
          "type": "array",
          "items": {
            "enum": [
              "atomic-redemption",
              "evidence-before-release"
            ]
          },
          "minItems": 2,
          "maxItems": 2,
          "uniqueItems": true
        },
        "resources": {
          "type": "array",
          "minItems": 1,
          "items": {
            "type": "object",
            "additionalProperties": false,
            "required": [
              "name",
              "version",
              "fields"
            ],
            "properties": {
              "name": {
                "type": "string",
                "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
              },
              "version": {
                "type": "string",
                "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
              },
              "fields": {
                "type": "array",
                "items": {
                  "type": "string",
                  "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
                },
                "minItems": 1,
                "uniqueItems": true
              }
            }
          }
        }
      }
    },
    "RegistryPolicy": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "registry",
        "version",
        "consumer_id",
        "purpose",
        "resource",
        "fields",
        "require_verified_subject",
        "require_human_consent",
        "max_grant_seconds",
        "max_disclosures"
      ],
      "properties": {
        "registry": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "version": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "consumer_id": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "purpose": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "resource": {
          "type": "string",
          "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
        },
        "fields": {
          "type": "array",
          "items": {
            "type": "string",
            "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
          },
          "minItems": 1,
          "uniqueItems": true
        },
        "require_verified_subject": {
          "const": true
        },
        "require_human_consent": {
          "const": true
        },
        "max_grant_seconds": {
          "type": "integer",
          "minimum": 1,
          "maximum": 300
        },
        "max_disclosures": {
          "const": 1
        }
      }
    }
  }
}
```

[Repository source](https://github.com/theflywheel/GRAMStack/tree/main/specs/0.3.0/schemas.json).
