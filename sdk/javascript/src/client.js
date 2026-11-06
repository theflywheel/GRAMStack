import http from "node:http"
import https from "node:https"
import { rootCertificates } from "node:tls"
import { visit } from "jsonc-parser"

export const VERSION = "0.3.0"
const ID = /^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$/
const STATES = new Set(["AUTHENTICATION_PENDING","BINDING_PENDING","CONSENT_PENDING","CONSENTED","AUTHORIZED","REDEEMING","EVIDENCE_COMMITTED","RELEASE_ATTEMPTED","COMPLETED","DELIVERY_UNKNOWN","REJECTED","DENIED","WITHDRAWN","EXPIRED","FAILED","CANCELLED"])
const TERMINAL = new Set(["COMPLETED","DELIVERY_UNKNOWN","REJECTED","DENIED","WITHDRAWN","EXPIRED","FAILED","CANCELLED"])
const grants = new WeakMap()

export class GramstackError extends Error {}
export class TransportError extends GramstackError {}
export class AlreadyAttempted extends GramstackError {}
export class RemoteError extends GramstackError {
  constructor(status) {
    super(`GRAMStack operation failed (HTTP ${status})`)
    this.status = status
  }
}
export function identifier(value) {
  if (typeof value !== "string" || !ID.test(value)) throw new GramstackError("Invalid GRAMStack identifier")
  return value
}
export function trustedURL(raw, {allowLoopback = false, origin = false} = {}) {
  if (typeof raw !== "string") throw new GramstackError("Pinned HTTPS endpoint required")
  let value
  try { value = new URL(raw) } catch { throw new GramstackError("Pinned HTTPS endpoint required") }
  const hostname = value.hostname.replace(/^\[|\]$/g, "")
  if (value.username || value.password || value.search || value.hash || raw.includes("?") || raw.includes("#") || (origin && value.pathname !== "/") ||
      (value.protocol !== "https:" && !(allowLoopback && value.protocol === "http:" && (hostname === "::1" || /^127\.(\d{1,3}\.){2}\d{1,3}$/.test(hostname))))) {
    throw new GramstackError("Pinned HTTPS endpoint required")
  }
  if (origin && raw.endsWith("/")) throw new GramstackError("Configure the origin without a trailing slash")
  return value
}
export function parseJSON(raw) {
  const text = new TextDecoder("utf-8", {fatal:true}).decode(raw)
  const objects = []
  let duplicate = false
  let invalid = false
  visit(text, {
    onObjectBegin() { objects.push(new Set()) },
    onObjectProperty(key) {
      const keys = objects.at(-1)
      if (keys.has(key)) duplicate = true
      keys.add(key)
    },
    onObjectEnd() { objects.pop() },
    onError() { invalid = true },
  }, {disallowComments:true,allowTrailingComma:false})
  if (duplicate || invalid) throw new GramstackError("Invalid or duplicate JSON properties")
  try { return JSON.parse(text) } catch { throw new GramstackError("Invalid GRAMStack JSON response") }
}
export function shape(value, required, optional = []) {
  if (value === null || typeof value !== "object" || Array.isArray(value) ||
      required.some(key => !Object.hasOwn(value,key)) ||
      Object.keys(value).some(key => !required.includes(key) && !optional.includes(key))) {
    throw new GramstackError("Invalid GRAMStack response shape")
  }
  return value
}
export function timestamp(value) {
  if (typeof value !== "string" || !/(Z|[+-]\d\d:\d\d)$/.test(value) || !Number.isFinite(Date.parse(value))) throw new GramstackError("Invalid timestamp")
  return value
}
export function fieldsValid(fields) {
  if (!Array.isArray(fields) || fields.length < 1 || fields.length > 128) throw new GramstackError("Explicit fields required")
  fields.forEach(identifier)
  if (new Set(fields).size !== fields.length) throw new GramstackError("Fields must be unique")
  return fields
}
export function wire(rawURL, method, headers, body, tls = {}, timeout = 10000) {
  return new Promise((resolve, reject) => {
    const url = new URL(rawURL)
    const transport = url.protocol === "https:" ? https : http
    // agent:false opens a fresh connection. No redirect or retry library participates.
    const request = transport.request(url, {method,headers,agent:false,minVersion:"TLSv1.3",...tls}, response => {
      const chunks = []
      let size = 0
      response.on("data", chunk => {
        size += chunk.length
        if (size > 65536) {
          response.destroy()
          reject(new TransportError("GRAMStack response exceeded its limit"))
        } else chunks.push(chunk)
      })
      response.on("end", () => resolve({status:response.statusCode,headers:response.headers,raw:Buffer.concat(chunks)}))
      response.on("error", () => reject(new TransportError("GRAMStack transport did not complete")))
    })
    const deadline = setTimeout(() => request.destroy(),timeout)
    request.on("close",() => clearTimeout(deadline))
    request.on("error", () => reject(new TransportError("GRAMStack transport did not complete")))
    request.end(body)
  })
}
export function basic(clientID, secret) {
  const encode = value => new URLSearchParams({v:value}).toString().slice(2)
  return "Basic " + Buffer.from(encode(clientID)+":"+encode(secret)).toString("base64")
}
export function publicTLS(ca) {
  return ca ? {ca:[...rootCertificates,ca]} : {}
}
export class Client {
  #config
  #publicTLS
  #privateTLS
  #token = ""
  #tokenUntil = 0
  #tokenPromise

  constructor(config) {
    trustedURL(config.publicOrigin,{allowLoopback:config.allowLoopback,origin:true})
    trustedURL(config.tokenURL,{allowLoopback:config.allowLoopback})
    identifier(config.clientID)
    if (typeof config.clientSecret !== "string" || !config.clientSecret) throw new GramstackError("Consumer credentials required")
    this.#config = {...config}
    this.#publicTLS = publicTLS(config.ca)
    if (config.dataOrigin) {
      trustedURL(config.dataOrigin,{origin:true})
      if (!config.ca || !config.certificate || !config.key || !config.dataServerName) throw new GramstackError("Pinned retrieval mTLS configuration required")
      this.#privateTLS = {ca:config.ca,cert:config.certificate,key:config.key,servername:config.dataServerName}
    }
  }
  async #accessToken() {
    if (this.#token && Date.now() < this.#tokenUntil - 10000) return this.#token
    if (this.#tokenPromise) return this.#tokenPromise
    this.#tokenPromise = this.#fetchToken()
    try { return await this.#tokenPromise } finally { this.#tokenPromise = undefined }
  }
  async #fetchToken() {
    const response = await wire(this.#config.tokenURL,"POST",{
      Authorization:basic(this.#config.clientID,this.#config.clientSecret),
      "Content-Type":"application/x-www-form-urlencoded",
    },new URLSearchParams({grant_type:"client_credentials",scope:"gramstack.access",audience:"gramstack"}).toString(),this.#publicTLS)
    if (response.status !== 200) throw new RemoteError(response.status)
    const value = parseJSON(response.raw)
    if (typeof value?.access_token !== "string" || value.access_token.length < 1 || value.access_token.length > 32768 ||
        String(value.token_type).toLowerCase() !== "bearer" || !Number.isSafeInteger(value.expires_in) || value.expires_in < 1 || value.expires_in > 86400) throw new GramstackError("Invalid OAuth token response")
    this.#token = value.access_token
    this.#tokenUntil = Date.now()+value.expires_in*1000
    return this.#token
  }
  async #call(method,path,key,body) {
    const payload = body === undefined ? undefined : JSON.stringify(body)
    const token = await this.#accessToken()
    const headers = {Authorization:"Bearer "+token,"Gramstack-Contract-Version":VERSION}
    if (key) headers["Idempotency-Key"] = identifier(key)
    if (body !== undefined) headers["Content-Type"] = "application/json"
    const response = await wire(this.#config.publicOrigin+path,method,headers,payload,this.#publicTLS)
    if (response.status < 200 || response.status >= 300) {
      if (response.status === 401 && this.#token === token) this.#token = ""
      throw new RemoteError(response.status)
    }
    if (response.headers["gramstack-contract-version"] !== VERSION) throw new GramstackError("Unsupported contract version")
    return parseJSON(response.raw)
  }
  async create(idempotencyKey, request) {
    shape(request,["registry","resource","subject_hint","fields","purpose","access","recipient_terms"])
    for (const name of ["registry","resource","purpose"]) identifier(request[name])
    shape(request.subject_hint,["type","value"])
    identifier(request.subject_hint.type)
    if (typeof request.subject_hint.value !== "string" || request.subject_hint.value.length < 1 || request.subject_hint.value.length > 1024) throw new GramstackError("Invalid subject hint")
    fieldsValid(request.fields)
    shape(request.access,["mode","valid_for_seconds"])
    if (request.access.mode !== "single_use" || !Number.isInteger(request.access.valid_for_seconds) || request.access.valid_for_seconds < 1 || request.access.valid_for_seconds > 300) throw new GramstackError("Unsupported access mode")
    shape(request.recipient_terms,["retention_policy_id"])
    identifier(request.recipient_terms.retention_policy_id)
    const value = await this.#call("POST","/v1/access-requests",identifier(idempotencyKey),request)
    shape(value,["request_id","status","status_url","interaction_url","expires_at"])
    identifier(value.request_id)
    timestamp(value.expires_at)
    if (value.status !== "AUTHENTICATION_PENDING" || value.status_url !== this.#config.publicOrigin+"/v1/access-requests/"+value.request_id ||
        value.interaction_url !== this.#config.publicOrigin+"/interactions/"+value.request_id) throw new GramstackError("Request locator mismatch")
    return Object.freeze(value)
  }
  async #status(id,cancel) {
    identifier(id)
    const value = await this.#call(cancel ? "POST" : "GET","/v1/access-requests/"+id+(cancel ? "/cancel" : ""))
    shape(value,["request_id","status","version","next_action","expires_at"])
    timestamp(value.expires_at)
    if (value.request_id !== id || !STATES.has(value.status) || !Number.isSafeInteger(value.version) || value.version < 1 || typeof value.next_action !== "string") throw new GramstackError("Invalid request status")
    return Object.freeze(value)
  }
  status(id) { return this.#status(id,false) }
  cancel(id) { return this.#status(id,true) }
  async issueGrant(id,idempotencyKey) {
    identifier(id)
    const value = await this.#call("POST","/v1/access-requests/"+id+"/grant",identifier(idempotencyKey))
    shape(value,["grant","expires_at","token_type"])
    timestamp(value.expires_at)
    if (typeof value.grant !== "string" || value.grant.length < 1 || value.grant.length > 32768 || value.token_type !== "Bearer" || Date.parse(value.expires_at) <= Date.now()) throw new GramstackError("Invalid retrieval grant")
    const handle = Object.freeze({expires_at:value.expires_at})
    grants.set(handle,{token:value.grant,id,client:this,attempted:false})
    return handle
  }
  async retrieve(grant,fields) {
    fields = [...fieldsValid(fields)]
    const authority = grants.get(grant)
    if (!authority || authority.client !== this || !this.#privateTLS || Date.parse(grant.expires_at) <= Date.now()) throw new GramstackError("A fresh grant and configured mTLS client are required")
    if (authority.attempted) throw new AlreadyAttempted("Retrieval was already attempted; inspect request status")
    authority.attempted = true
    const response = await wire(this.#config.dataOrigin+"/v1/access-requests/"+authority.id+"/data","GET",{Authorization:"Bearer "+authority.token},undefined,this.#privateTLS,30000)
    if (response.status !== 200) throw new RemoteError(response.status)
    if (response.headers["gramstack-contract-version"] !== VERSION) throw new GramstackError("Unsupported contract version")
    const value = shape(parseJSON(response.raw),["request_id","data","receipt_id"])
    identifier(value.receipt_id)
    if (value.request_id !== authority.id || value.data === null || typeof value.data !== "object" || Array.isArray(value.data) ||
        Object.keys(value.data).length !== fields.length || fields.some(field => !Object.hasOwn(value.data,field))) throw new GramstackError("Disclosure did not match exact request and fields")
    // rawJSON preserves numeric representations for applications using a decimal-aware decoder.
    return Object.freeze({...value,rawJSON:response.raw.toString("utf8")})
  }
  historyURL(registry) { return this.#config.publicOrigin+"/consent-history/"+identifier(registry) }
  async waitForConsent(id,{timeout,interval = 1000,signal} = {}) {
    if (!Number.isFinite(timeout) || timeout <= 0 || !Number.isFinite(interval) || interval < 1000) throw new GramstackError("A timeout and polling interval >= 1000ms are required")
    const deadline = Date.now()+timeout
    while (true) {
      signal?.throwIfAborted()
      const value = await this.status(id)
      if (TERMINAL.has(value.status) || ["CONSENTED","AUTHORIZED"].includes(value.status)) return value
      const remaining = deadline-Date.now()
      if (remaining <= 0) throw new GramstackError("Consent wait timed out")
      await new Promise((resolve,reject) => {
        const cancel = () => { clearTimeout(timer); reject(signal.reason) }
        const timer = setTimeout(() => {signal?.removeEventListener("abort",cancel);resolve()},Math.min(interval,remaining))
        signal?.addEventListener("abort",cancel,{once:true})
      })
    }
  }
}
