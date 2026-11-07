---
title: HTTP API specification
description: Versioned GRAMStack implementation contracts and conformance requirements.
---

Specification version **0.3.0** · [Specifications index](/developers/specifications) · [Download source](https://docs.gramstack.try-dough.com/static/specs/0.3.0/openapi.json)

```json
{
  "openapi": "3.1.0",
  "info": {
    "title": "GRAMStack contracts",
    "version": "0.3.0",
    "description": "Paths belong to the component named on each operation. Internal routes require authenticated service transport and private routing. See protocol.md for behavioral requirements."
  },
  "paths": {
    "/v1/access-requests": {
      "post": {
        "operationId": "createAccessRequest",
        "summary": "createAccessRequest",
        "tags": [
          "Gateway"
        ],
        "description": "Gateway. Creates immutable snapshot; asynchronous human journey follows.",
        "security": [
          {
            "consumerBearer": []
          }
        ],
        "responses": {
          "202": {
            "description": "Successful operation",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/RequestAccepted"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "default": {
            "description": "Protocol error; see protocol.md for status mapping.",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "400": {
            "description": "Invalid input",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "401": {
            "description": "Authentication failed",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "403": {
            "description": "Forbidden",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "404": {
            "description": "Unknown or non-owned object",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "409": {
            "description": "State or idempotency conflict",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "410": {
            "description": "Expired",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "429": {
            "description": "Rate limited; Retry-After applies",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "503": {
            "description": "Dependency unavailable",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          }
        },
        "parameters": [
          {
            "name": "Idempotency-Key",
            "in": "header",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
            }
          }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "$ref": "./schemas.json#/$defs/AccessRequest"
              }
            }
          }
        }
      }
    },
    "/v1/access-requests/{id}": {
      "get": {
        "operationId": "getAccessRequest",
        "summary": "getAccessRequest",
        "tags": [
          "Gateway"
        ],
        "description": "Gateway. Only owning consumer; status metadata without business data.",
        "security": [
          {
            "consumerBearer": []
          }
        ],
        "responses": {
          "200": {
            "description": "Successful operation",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/RequestStatus"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "default": {
            "description": "Protocol error; see protocol.md for status mapping.",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "400": {
            "description": "Invalid input",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "401": {
            "description": "Authentication failed",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "403": {
            "description": "Forbidden",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "404": {
            "description": "Unknown or non-owned object",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "409": {
            "description": "State or idempotency conflict",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "410": {
            "description": "Expired",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "429": {
            "description": "Rate limited; Retry-After applies",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "503": {
            "description": "Dependency unavailable",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          }
        },
        "parameters": [
          {
            "name": "id",
            "in": "path",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
            }
          }
        ]
      }
    },
    "/v1/access-requests/{id}/grant": {
      "post": {
        "operationId": "issueGrant",
        "summary": "issueGrant",
        "tags": [
          "Gateway"
        ],
        "description": "Gateway. Explicit idempotent issuance after active consent and policy checks.",
        "security": [
          {
            "consumerBearer": []
          }
        ],
        "responses": {
          "200": {
            "description": "Successful operation",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/GrantResponse"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "default": {
            "description": "Protocol error; see protocol.md for status mapping.",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "400": {
            "description": "Invalid input",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "401": {
            "description": "Authentication failed",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "403": {
            "description": "Forbidden",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "404": {
            "description": "Unknown or non-owned object",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "409": {
            "description": "State or idempotency conflict",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "410": {
            "description": "Expired",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "429": {
            "description": "Rate limited; Retry-After applies",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "503": {
            "description": "Dependency unavailable",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          }
        },
        "parameters": [
          {
            "name": "id",
            "in": "path",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
            }
          },
          {
            "name": "Idempotency-Key",
            "in": "header",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
            }
          }
        ]
      }
    },
    "/v1/access-requests/{id}/data": {
      "get": {
        "operationId": "retrieveData",
        "summary": "retrieveData",
        "tags": [
          "Gateway"
        ],
        "description": "Gateway. Consumes one disclosure. No prefetch, caching or automatic retry; Cache-Control: no-store. Only requested authorized fields may be present.",
        "security": [
          {
            "retrievalGrant": [],
            "presenterMTLS": []
          }
        ],
        "responses": {
          "200": {
            "description": "Successful operation",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/DataResponse"
                }
              }
            },
            "headers": {
              "Cache-Control": {
                "required": true,
                "schema": {
                  "const": "no-store"
                }
              },
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "default": {
            "description": "Protocol error; see protocol.md for status mapping.",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "400": {
            "description": "Invalid input",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "401": {
            "description": "Authentication failed",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "403": {
            "description": "Forbidden",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "404": {
            "description": "Unknown or non-owned object",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "409": {
            "description": "State or idempotency conflict",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "410": {
            "description": "Expired",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "429": {
            "description": "Rate limited; Retry-After applies",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "503": {
            "description": "Dependency unavailable",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          }
        },
        "parameters": [
          {
            "name": "id",
            "in": "path",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
            }
          }
        ]
      }
    },
    "/interactions/{id}": {
      "get": {
        "operationId": "getInteraction",
        "summary": "getInteraction",
        "tags": [
          "Gateway browser handlers"
        ],
        "description": "Gateway browser handlers. Authenticated owner view; unauthenticated users enter the configured IdP flow.",
        "security": [
          {
            "browserSession": []
          }
        ],
        "responses": {
          "200": {
            "description": "Successful operation",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/RequestStatus"
                }
              }
            }
          },
          "default": {
            "description": "Protocol error; see protocol.md for status mapping.",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "400": {
            "description": "Invalid input",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "401": {
            "description": "Authentication failed",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "403": {
            "description": "Forbidden",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "404": {
            "description": "Unknown or non-owned object",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "409": {
            "description": "State or idempotency conflict",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "410": {
            "description": "Expired",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "429": {
            "description": "Rate limited; Retry-After applies",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "503": {
            "description": "Dependency unavailable",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          }
        },
        "parameters": [
          {
            "name": "id",
            "in": "path",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
            }
          }
        ]
      }
    },
    "/interactions/{id}/consent": {
      "get": {
        "operationId": "getNotice",
        "summary": "getNotice",
        "tags": [
          "Gateway browser handlers"
        ],
        "description": "Gateway browser handlers. ",
        "security": [
          {
            "browserSession": []
          }
        ],
        "responses": {
          "200": {
            "description": "Successful operation",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Notice"
                }
              }
            }
          },
          "default": {
            "description": "Protocol error; see protocol.md for status mapping.",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "400": {
            "description": "Invalid input",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "401": {
            "description": "Authentication failed",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "403": {
            "description": "Forbidden",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "404": {
            "description": "Unknown or non-owned object",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "409": {
            "description": "State or idempotency conflict",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "410": {
            "description": "Expired",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "429": {
            "description": "Rate limited; Retry-After applies",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "503": {
            "description": "Dependency unavailable",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          }
        },
        "parameters": [
          {
            "name": "id",
            "in": "path",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
            }
          }
        ]
      }
    },
    "/interactions/{id}/decision": {
      "post": {
        "operationId": "recordDecision",
        "summary": "recordDecision",
        "tags": [
          "Gateway browser handlers"
        ],
        "description": "Gateway browser handlers. ",
        "security": [
          {
            "browserSession": []
          }
        ],
        "responses": {
          "200": {
            "description": "Successful operation",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/ConsentStatus"
                }
              }
            }
          },
          "default": {
            "description": "Protocol error; see protocol.md for status mapping.",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "400": {
            "description": "Invalid input",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "401": {
            "description": "Authentication failed",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "403": {
            "description": "Forbidden",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "404": {
            "description": "Unknown or non-owned object",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "409": {
            "description": "State or idempotency conflict",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "410": {
            "description": "Expired",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "429": {
            "description": "Rate limited; Retry-After applies",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "503": {
            "description": "Dependency unavailable",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          }
        },
        "parameters": [
          {
            "name": "id",
            "in": "path",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
            }
          },
          {
            "name": "X-CSRF-Token",
            "in": "header",
            "required": true,
            "schema": {
              "type": "string",
              "minLength": 1
            }
          }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "$ref": "./schemas.json#/$defs/Decision"
              }
            }
          }
        }
      }
    },
    "/consents/{id}/withdrawal": {
      "post": {
        "operationId": "withdrawConsent",
        "summary": "withdrawConsent",
        "tags": [
          "Gateway browser handlers"
        ],
        "description": "Gateway browser handlers. Owner session and applicable reauthentication; atomically ordered with redemption.",
        "security": [
          {
            "browserSession": []
          }
        ],
        "responses": {
          "200": {
            "description": "Successful operation",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/ConsentStatus"
                }
              }
            }
          },
          "default": {
            "description": "Protocol error; see protocol.md for status mapping.",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "400": {
            "description": "Invalid input",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "401": {
            "description": "Authentication failed",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "403": {
            "description": "Forbidden",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "404": {
            "description": "Unknown or non-owned object",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "409": {
            "description": "State or idempotency conflict",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "410": {
            "description": "Expired",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "429": {
            "description": "Rate limited; Retry-After applies",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "503": {
            "description": "Dependency unavailable",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          }
        },
        "parameters": [
          {
            "name": "id",
            "in": "path",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
            }
          },
          {
            "name": "X-CSRF-Token",
            "in": "header",
            "required": true,
            "schema": {
              "type": "string",
              "minLength": 1
            }
          }
        ]
      }
    },
    "/interactions/{id}/receipt": {
      "get": {
        "operationId": "getReceipt",
        "summary": "getReceipt",
        "tags": [
          "Gateway browser handlers"
        ],
        "description": "Gateway browser handlers. ",
        "security": [
          {
            "browserSession": []
          }
        ],
        "responses": {
          "200": {
            "description": "Successful operation",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Receipt"
                }
              }
            }
          },
          "default": {
            "description": "Protocol error; see protocol.md for status mapping.",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "400": {
            "description": "Invalid input",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "401": {
            "description": "Authentication failed",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "403": {
            "description": "Forbidden",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "404": {
            "description": "Unknown or non-owned object",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "409": {
            "description": "State or idempotency conflict",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "410": {
            "description": "Expired",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "429": {
            "description": "Rate limited; Retry-After applies",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "503": {
            "description": "Dependency unavailable",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          }
        },
        "parameters": [
          {
            "name": "id",
            "in": "path",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
            }
          }
        ]
      }
    },
    "/internal/consent-requests": {
      "post": {
        "operationId": "createConsentRequest",
        "summary": "createConsentRequest",
        "tags": [
          "Consent module"
        ],
        "description": "Consent module. ",
        "security": [
          {
            "serviceMTLS": []
          }
        ],
        "responses": {
          "201": {
            "description": "Successful operation",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Notice"
                }
              }
            }
          },
          "default": {
            "description": "Protocol error; see protocol.md for status mapping.",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "400": {
            "description": "Invalid input",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "401": {
            "description": "Authentication failed",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "403": {
            "description": "Forbidden",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "404": {
            "description": "Unknown or non-owned object",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "409": {
            "description": "State or idempotency conflict",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "410": {
            "description": "Expired",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "429": {
            "description": "Rate limited; Retry-After applies",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "503": {
            "description": "Dependency unavailable",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          }
        },
        "parameters": [
          {
            "name": "Idempotency-Key",
            "in": "header",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
            }
          }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "$ref": "./schemas.json#/$defs/ConsentRequest"
              }
            }
          }
        }
      }
    },
    "/internal/consent-requests/{id}": {
      "get": {
        "operationId": "getConsentRequest",
        "summary": "getConsentRequest",
        "tags": [
          "Consent module"
        ],
        "description": "Consent module. ",
        "security": [
          {
            "serviceMTLS": []
          }
        ],
        "responses": {
          "200": {
            "description": "Successful operation",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Notice"
                }
              }
            }
          },
          "default": {
            "description": "Protocol error; see protocol.md for status mapping.",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "400": {
            "description": "Invalid input",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "401": {
            "description": "Authentication failed",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "403": {
            "description": "Forbidden",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "404": {
            "description": "Unknown or non-owned object",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "409": {
            "description": "State or idempotency conflict",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "410": {
            "description": "Expired",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "429": {
            "description": "Rate limited; Retry-After applies",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "503": {
            "description": "Dependency unavailable",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          }
        },
        "parameters": [
          {
            "name": "id",
            "in": "path",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
            }
          }
        ]
      }
    },
    "/internal/consents/{id}/artefact": {
      "get": {
        "operationId": "getConsentArtefact",
        "summary": "getConsentArtefact",
        "tags": [
          "Consent module"
        ],
        "description": "Consent module. ",
        "security": [
          {
            "serviceMTLS": []
          }
        ],
        "responses": {
          "200": {
            "description": "Successful operation",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/SignedEnvelope"
                }
              }
            }
          },
          "default": {
            "description": "Protocol error; see protocol.md for status mapping.",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "400": {
            "description": "Invalid input",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "401": {
            "description": "Authentication failed",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "403": {
            "description": "Forbidden",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "404": {
            "description": "Unknown or non-owned object",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "409": {
            "description": "State or idempotency conflict",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "410": {
            "description": "Expired",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "429": {
            "description": "Rate limited; Retry-After applies",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "503": {
            "description": "Dependency unavailable",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          }
        },
        "parameters": [
          {
            "name": "id",
            "in": "path",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
            }
          }
        ]
      }
    },
    "/internal/consents/{id}/status": {
      "get": {
        "operationId": "getConsentStatus",
        "summary": "getConsentStatus",
        "tags": [
          "Consent module"
        ],
        "description": "Consent module. ",
        "security": [
          {
            "serviceMTLS": []
          }
        ],
        "responses": {
          "200": {
            "description": "Successful operation",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/ConsentStatus"
                }
              }
            }
          },
          "default": {
            "description": "Protocol error; see protocol.md for status mapping.",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "400": {
            "description": "Invalid input",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "401": {
            "description": "Authentication failed",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "403": {
            "description": "Forbidden",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "404": {
            "description": "Unknown or non-owned object",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "409": {
            "description": "State or idempotency conflict",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "410": {
            "description": "Expired",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "429": {
            "description": "Rate limited; Retry-After applies",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "503": {
            "description": "Dependency unavailable",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          }
        },
        "parameters": [
          {
            "name": "id",
            "in": "path",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
            }
          }
        ]
      }
    },
    "/internal/consent-events": {
      "post": {
        "operationId": "receiveConsentEvent",
        "summary": "receiveConsentEvent",
        "tags": [
          "Gateway event receiver"
        ],
        "description": "Gateway event receiver. Verify JWS, stable event_id and monotonic status_version. Ack echoes decoded event metadata.",
        "security": [
          {
            "serviceMTLS": []
          }
        ],
        "responses": {
          "202": {
            "description": "Successful operation",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/ConsentEvent"
                }
              }
            }
          },
          "default": {
            "description": "Protocol error; see protocol.md for status mapping.",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "400": {
            "description": "Invalid input",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "401": {
            "description": "Authentication failed",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "403": {
            "description": "Forbidden",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "404": {
            "description": "Unknown or non-owned object",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "409": {
            "description": "State or idempotency conflict",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "410": {
            "description": "Expired",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "429": {
            "description": "Rate limited; Retry-After applies",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "503": {
            "description": "Dependency unavailable",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          }
        },
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "$ref": "./schemas.json#/$defs/SignedEnvelope"
              }
            }
          }
        }
      }
    },
    "/internal/v1/grants/redeem": {
      "post": {
        "operationId": "redeemGrant",
        "summary": "redeemGrant",
        "tags": [
          "AuthZ enforcement"
        ],
        "description": "AuthZ enforcement. Caller must own grant audience; atomic consent/status/use transaction.",
        "security": [
          {
            "serviceMTLS": []
          }
        ],
        "responses": {
          "200": {
            "description": "Successful operation",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/RedemptionResult"
                }
              }
            }
          },
          "default": {
            "description": "Protocol error; see protocol.md for status mapping.",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "400": {
            "description": "Invalid input",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "401": {
            "description": "Authentication failed",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "403": {
            "description": "Forbidden",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "404": {
            "description": "Unknown or non-owned object",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "409": {
            "description": "State or idempotency conflict",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "410": {
            "description": "Expired",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "429": {
            "description": "Rate limited; Retry-After applies",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "503": {
            "description": "Dependency unavailable",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          }
        },
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "$ref": "./schemas.json#/$defs/RedemptionRequest"
              }
            }
          }
        }
      }
    },
    "/internal/v1/retrievals": {
      "post": {
        "operationId": "retrieveFromAdapter",
        "summary": "retrieveFromAdapter",
        "tags": [
          "Registry adapter"
        ],
        "description": "Registry adapter. Authenticate gateway; independently verify and redeem grant; fetch once; durable evidence before release.",
        "security": [
          {
            "serviceMTLS": []
          }
        ],
        "responses": {
          "200": {
            "description": "Successful operation",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/DataResponse"
                }
              }
            }
          },
          "default": {
            "description": "Protocol error; see protocol.md for status mapping.",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "400": {
            "description": "Invalid input",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "401": {
            "description": "Authentication failed",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "403": {
            "description": "Forbidden",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "404": {
            "description": "Unknown or non-owned object",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "409": {
            "description": "State or idempotency conflict",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "410": {
            "description": "Expired",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "429": {
            "description": "Rate limited; Retry-After applies",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "503": {
            "description": "Dependency unavailable",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          }
        },
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "$ref": "./schemas.json#/$defs/RetrievalRequest"
              }
            }
          }
        }
      }
    },
    "/internal/v1/evidence": {
      "post": {
        "operationId": "recordEvidence",
        "summary": "recordEvidence",
        "tags": [
          "Evidence custodian"
        ],
        "description": "Evidence custodian. Verify approved adapter JWS; exact duplicate event returns same receipt, conflicting event returns 409.",
        "security": [
          {
            "serviceMTLS": []
          }
        ],
        "responses": {
          "201": {
            "description": "Successful operation",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/EvidenceAcknowledgement"
                }
              }
            }
          },
          "default": {
            "description": "Protocol error; see protocol.md for status mapping.",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "400": {
            "description": "Invalid input",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "401": {
            "description": "Authentication failed",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "403": {
            "description": "Forbidden",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "404": {
            "description": "Unknown or non-owned object",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "409": {
            "description": "State or idempotency conflict",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "410": {
            "description": "Expired",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "429": {
            "description": "Rate limited; Retry-After applies",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "503": {
            "description": "Dependency unavailable",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          }
        },
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "$ref": "./schemas.json#/$defs/SignedEnvelope"
              }
            }
          }
        }
      }
    },
    "/.well-known/gramstack-adapter": {
      "get": {
        "operationId": "getAdapterManifest",
        "summary": "getAdapterManifest",
        "tags": [
          "Registry adapter"
        ],
        "description": "Registry adapter. Manifest must match approved operator descriptor; discovery does not establish trust.",
        "security": [],
        "responses": {
          "200": {
            "description": "Successful operation",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/AdapterManifest"
                }
              }
            }
          },
          "default": {
            "description": "Protocol error; see protocol.md for status mapping.",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "400": {
            "description": "Invalid input",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "401": {
            "description": "Authentication failed",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "403": {
            "description": "Forbidden",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "404": {
            "description": "Unknown or non-owned object",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "409": {
            "description": "State or idempotency conflict",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "410": {
            "description": "Expired",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "429": {
            "description": "Rate limited; Retry-After applies",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          },
          "503": {
            "description": "Dependency unavailable",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            }
          }
        }
      }
    },
    "/v1/access-requests/{id}/cancel": {
      "post": {
        "operationId": "cancelAccessRequest",
        "summary": "Cancel an unadmitted access request",
        "tags": [
          "Gateway"
        ],
        "description": "Gateway. Owning consumer only. Idempotent cancellation under shared request/consent locks. Returns CANCELLED; 409 if admission has already occurred or the terminal state cannot be cancelled. Does not recall prior disclosure.",
        "security": [
          {
            "consumerBearer": []
          }
        ],
        "responses": {
          "200": {
            "description": "Successful operation",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/RequestStatus"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "default": {
            "description": "Protocol error; see protocol.md for status mapping.",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "400": {
            "description": "Invalid input",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "401": {
            "description": "Authentication failed",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "403": {
            "description": "Forbidden",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "404": {
            "description": "Unknown or non-owned object",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "409": {
            "description": "State or idempotency conflict",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "410": {
            "description": "Expired",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "429": {
            "description": "Rate limited; Retry-After applies",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          },
          "503": {
            "description": "Dependency unavailable",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "./schemas.json#/$defs/Error"
                }
              }
            },
            "headers": {
              "Gramstack-Contract-Version": {
                "description": "Implemented core request lifecycle contract. Independent profiles retain their own version headers.",
                "schema": {
                  "type": "string",
                  "const": "0.3.0"
                }
              }
            }
          }
        },
        "parameters": [
          {
            "name": "id",
            "in": "path",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
            }
          }
        ]
      }
    }
  },
  "components": {
    "securitySchemes": {
      "consumerBearer": {
        "type": "http",
        "scheme": "bearer",
        "description": "Hydra consumer token, validated audience and registered client."
      },
      "browserSession": {
        "type": "apiKey",
        "in": "cookie",
        "name": "gramstack_session"
      },
      "serviceMTLS": {
        "type": "mutualTLS"
      },
      "presenterMTLS": {
        "type": "mutualTLS",
        "description": "Consumer certificate must match grant cnf thumbprint."
      },
      "retrievalGrant": {
        "type": "http",
        "scheme": "bearer",
        "bearerFormat": "JWT"
      }
    }
  }
}
```

[Repository source](https://github.com/theflywheel/GRAMStack/tree/main/specs/0.3.0/openapi.json).
