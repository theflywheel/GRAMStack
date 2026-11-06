export interface Config {
  publicOrigin: string;
  tokenURL: string;
  clientID: string;
  clientSecret: string;
  dataOrigin?: string;
  dataServerName?: string;
  ca?: string;
  certificate?: string;
  key?: string;
  allowLoopback?: boolean;
}
export interface AccessRequest {
  registry: string;
  resource: string;
  subject_hint: {type: string; value: string};
  fields: string[];
  purpose: string;
  access: {mode: "single_use"; valid_for_seconds: number};
  recipient_terms: {retention_policy_id: string};
}
export interface Accepted {
  readonly request_id: string;
  readonly status: "AUTHENTICATION_PENDING";
  readonly status_url: string;
  readonly interaction_url: string;
  readonly expires_at: string;
}
export type RequestState = "AUTHENTICATION_PENDING" | "BINDING_PENDING" | "CONSENT_PENDING" | "CONSENTED" | "AUTHORIZED" | "REDEEMING" | "EVIDENCE_COMMITTED" | "RELEASE_ATTEMPTED" | "COMPLETED" | "DELIVERY_UNKNOWN" | "REJECTED" | "DENIED" | "WITHDRAWN" | "EXPIRED" | "FAILED" | "CANCELLED";
export interface Status {
  readonly request_id: string;
  readonly status: RequestState;
  readonly version: number;
  readonly next_action: string;
  readonly expires_at: string;
}
declare const grantBrand: unique symbol;
export interface Grant {
  readonly [grantBrand]: true;
  readonly expires_at: string;
}
export interface Result {
  readonly request_id: string;
  readonly data: Record<string, unknown>;
  readonly receipt_id: string;
  /** Exact response bytes as UTF-8 JSON, for decimal-aware parsing. */
  readonly rawJSON: string;
}
export class GramstackError extends Error {}
export class TransportError extends GramstackError {}
export class AlreadyAttempted extends GramstackError {}
export class RemoteError extends GramstackError {readonly status: number}
export class AuthenticationError extends GramstackError {}
export class Client {
  constructor(config: Config);
  create(idempotencyKey: string, request: AccessRequest): Promise<Accepted>;
  status(requestID: string): Promise<Status>;
  cancel(requestID: string): Promise<Status>;
  issueGrant(requestID: string, idempotencyKey: string): Promise<Grant>;
  retrieve(grant: Grant, fields: string[]): Promise<Result>;
  historyURL(registry: string): string;
  waitForConsent(requestID: string, options: {timeout: number; interval?: number; signal?: AbortSignal}): Promise<Status>;
}
export interface AuthConfig {
  issuer: string;
  clientID: string;
  clientSecret?: string;
  redirectURL: string;
  ca?: string;
  allowLoopback?: boolean;
}
export interface LoginTransaction {
  state: string;
  nonce: string;
  verifier: string;
  /** Unix milliseconds. */
  startedAt: number;
  expiresAt: number;
}
export interface LoginStore {
  saveLogin(state: string, browserBinding: string, transaction: LoginTransaction): Promise<void>;
  /** Atomically consume only an unexpired transaction with this browser binding. */
  takeLogin(state: string, browserBinding: string): Promise<LoginTransaction>;
}
export interface Identity {
  readonly issuer: string;
  readonly subject: string;
  readonly authenticated_at: string;
  readonly expires_at: string;
}
export class AuthClient {
  private constructor();
  static create(config: AuthConfig, store: LoginStore): Promise<AuthClient>;
  begin(browserBinding: string): Promise<string>;
  complete(browserBinding: string, callbackURL: string): Promise<Identity>;
}
/** Explicit development-only, single-process store. */
export class MemoryLoginStore implements LoginStore {
  saveLogin(state: string, browserBinding: string, transaction: LoginTransaction): Promise<void>;
  takeLogin(state: string, browserBinding: string): Promise<LoginTransaction>;
}
