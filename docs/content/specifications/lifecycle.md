---
title: Lifecycle transitions
description: Versioned GRAMStack implementation contracts and conformance requirements.
---

Specification version **0.2.0** · [Specifications index](/specifications/) · [Download source](https://docs.gramstack.try-dough.com/static/specs/0.2.0/lifecycle.json)

```json
{
  "version": "0.2.0",
  "initial": "AUTHENTICATION_PENDING",
  "terminal": [
    "COMPLETED",
    "DELIVERY_UNKNOWN",
    "REJECTED",
    "DENIED",
    "WITHDRAWN",
    "EXPIRED",
    "FAILED",
    "CANCELLED"
  ],
  "transitions": {
    "AUTHENTICATION_PENDING": [
      "BINDING_PENDING",
      "FAILED",
      "DENIED",
      "EXPIRED",
      "CANCELLED"
    ],
    "BINDING_PENDING": [
      "CONSENT_PENDING",
      "FAILED",
      "DENIED",
      "EXPIRED",
      "CANCELLED"
    ],
    "CONSENT_PENDING": [
      "CONSENTED",
      "FAILED",
      "DENIED",
      "EXPIRED",
      "REJECTED",
      "CANCELLED"
    ],
    "CONSENTED": [
      "AUTHORIZED",
      "FAILED",
      "WITHDRAWN",
      "EXPIRED",
      "CANCELLED"
    ],
    "AUTHORIZED": [
      "REDEEMING",
      "FAILED",
      "WITHDRAWN",
      "EXPIRED",
      "CANCELLED"
    ],
    "REDEEMING": [
      "EVIDENCE_COMMITTED",
      "FAILED",
      "DELIVERY_UNKNOWN"
    ],
    "EVIDENCE_COMMITTED": [
      "RELEASE_ATTEMPTED",
      "FAILED",
      "DELIVERY_UNKNOWN"
    ],
    "RELEASE_ATTEMPTED": [
      "COMPLETED",
      "DELIVERY_UNKNOWN"
    ],
    "COMPLETED": [],
    "DELIVERY_UNKNOWN": [],
    "REJECTED": [],
    "DENIED": [],
    "WITHDRAWN": [],
    "EXPIRED": [],
    "FAILED": [],
    "CANCELLED": []
  }
}
```

[Repository source](https://github.com/theflywheel/GRAMStack/tree/main/specs/0.2.0/lifecycle.json).
