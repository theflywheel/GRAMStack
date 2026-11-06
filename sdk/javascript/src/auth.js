import { randomBytes,createHash,timingSafeEqual } from "node:crypto"
import { createLocalJWKSet,jwtVerify } from "jose"
import { GramstackError,basic,identifier,parseJSON,publicTLS,trustedURL,wire } from "./client.js"

export class AuthenticationError extends GramstackError {}
const random = () => randomBytes(32).toString("base64url")
function equal(left,right) {
  if (typeof left !== "string" || typeof right !== "string") return false
  const a=Buffer.from(left),b=Buffer.from(right)
  return a.length===b.length && timingSafeEqual(a,b)
}
export class MemoryLoginStore {
  #values = new Map()
  async saveLogin(state,binding,value) {
    for (const [key,entry] of this.#values) if (entry.value.expiresAt <= Date.now()) this.#values.delete(key)
    if (this.#values.size>=4096 || this.#values.has(state)) throw new AuthenticationError("Login state unavailable")
    this.#values.set(state,{binding,value})
  }
  async takeLogin(state,binding) {
    const entry=this.#values.get(state)
    if (!entry || !equal(entry.binding,binding) || entry.value.expiresAt<=Date.now()) throw new AuthenticationError("Login state unavailable")
    this.#values.delete(state)
    return entry.value
  }
}
export class AuthClient {
  #config
  #store
  #metadata
  #tls
  constructor(config,store,metadata) {
    this.#config={...config}
    this.#store=store
    this.#metadata=metadata
    this.#tls=publicTLS(config.ca)
  }
  static async create(config,store) {
    trustedURL(config.issuer,{allowLoopback:config.allowLoopback})
    trustedURL(config.redirectURL,{allowLoopback:config.allowLoopback})
    identifier(config.clientID)
    if (typeof store?.saveLogin !== "function" || typeof store?.takeLogin !== "function") throw new AuthenticationError("A one-use browser-bound login store is required")
    const response=await wire(config.issuer.replace(/\/$/,"")+"/.well-known/openid-configuration","GET",{},undefined,publicTLS(config.ca))
    if (response.status!==200) throw new AuthenticationError("OIDC discovery unavailable")
    const metadata=parseJSON(response.raw)
    if (metadata.issuer!==config.issuer) throw new AuthenticationError("Issuer mismatch")
    const issuer=new URL(config.issuer)
    for (const name of ["authorization_endpoint","token_endpoint","jwks_uri"]) {
      const endpoint=trustedURL(metadata[name],{allowLoopback:config.allowLoopback})
      if (endpoint.origin!==issuer.origin) throw new AuthenticationError("Untrusted OIDC endpoint")
    }
    return new AuthClient(config,store,metadata)
  }
  async begin(browserBinding) {
    if (typeof browserBinding!=="string" || browserBinding.length<26 || browserBinding.length>256) throw new AuthenticationError("Server-owned browser binding required")
    const transaction={state:random(),nonce:random(),verifier:random(),startedAt:Date.now(),expiresAt:Date.now()+300000}
    await this.#store.saveLogin(transaction.state,browserBinding,transaction)
    const query=new URLSearchParams({client_id:this.#config.clientID,redirect_uri:this.#config.redirectURL,response_type:"code",scope:"openid",state:transaction.state,nonce:transaction.nonce,
      code_challenge:createHash("sha256").update(transaction.verifier).digest("base64url"),code_challenge_method:"S256",prompt:"login",max_age:"0"})
    return this.#metadata.authorization_endpoint+"?"+query
  }
  async complete(browserBinding,callbackURL) {
    const actual=new URL(callbackURL),expected=new URL(this.#config.redirectURL)
    if (actual.origin!==expected.origin || actual.pathname!==expected.pathname || actual.hash || actual.username || actual.password) throw new AuthenticationError("Callback mismatch")
    const q=actual.searchParams
    if (q.getAll("state").length!==1 || q.getAll("code").length!==1 || q.has("error") || q.get("state").length<26 || q.get("state").length>256 || q.get("code").length<1 || q.get("code").length>8192) throw new AuthenticationError("Invalid callback")
    const transaction=await this.#store.takeLogin(q.get("state"),browserBinding)
    if (transaction.state!==q.get("state") || transaction.expiresAt<=Date.now()) throw new AuthenticationError("Expired or mismatched login state")
    const body=new URLSearchParams({grant_type:"authorization_code",code:q.get("code"),redirect_uri:this.#config.redirectURL,code_verifier:transaction.verifier})
    const headers={"Content-Type":"application/x-www-form-urlencoded"}
    if (this.#config.clientSecret) headers.Authorization=basic(this.#config.clientID,this.#config.clientSecret)
    else body.set("client_id",this.#config.clientID)
    const response=await wire(this.#metadata.token_endpoint,"POST",headers,body.toString(),this.#tls)
    if (response.status!==200) throw new AuthenticationError("Authorization code exchange failed")
    const tokens=parseJSON(response.raw)
    if (typeof tokens.id_token!=="string" || tokens.id_token.length<1 || tokens.id_token.length>32768) throw new AuthenticationError("ID token unavailable")
    try {
      const parts=tokens.id_token.split(".")
      if (parts.length!==3) throw new Error()
      parseJSON(Buffer.from(parts[0],"base64url"))
      parseJSON(Buffer.from(parts[1],"base64url"))
      const keyResponse=await wire(this.#metadata.jwks_uri,"GET",{},undefined,this.#tls)
      if (keyResponse.status!==200) throw new Error()
      const keys=parseJSON(keyResponse.raw)
      if (!Array.isArray(keys.keys) || keys.keys.length<1 || keys.keys.length>16 || new Set(keys.keys.map(key=>key.kid)).size!==keys.keys.length) throw new Error()
      const {payload,protectedHeader}=await jwtVerify(tokens.id_token,createLocalJWKSet(keys),{
        issuer:this.#config.issuer,audience:this.#config.clientID,algorithms:["RS256","ES256"],
        requiredClaims:["iss","sub","aud","exp","iat","nonce","auth_time"],
      })
      const now=Math.floor(Date.now()/1000)
      const audience=Array.isArray(payload.aud)?payload.aud:[payload.aud]
      if (audience.length!==1 || audience[0]!==this.#config.clientID || !equal(payload.nonce,transaction.nonce) ||
          typeof payload.sub!=="string" || payload.sub.length<1 || payload.sub.length>1024 ||
          (payload.azp!==undefined && payload.azp!==this.#config.clientID) ||
          ![payload.iat,payload.exp,payload.auth_time].every(Number.isSafeInteger) || payload.iat>now+30 ||
          payload.auth_time<Math.floor(transaction.startedAt/1000) || payload.auth_time>now+30 || now-payload.auth_time>300) throw new Error()
      if (payload.at_hash!==undefined) {
        const digest=createHash("sha256").update(tokens.access_token??"").digest().subarray(0,16).toString("base64url")
        if (!equal(payload.at_hash,digest)) throw new Error()
      }
      if (!["RS256","ES256"].includes(protectedHeader.alg)) throw new Error()
      return Object.freeze({issuer:payload.iss,subject:payload.sub,authenticated_at:new Date(payload.auth_time*1000).toISOString(),expires_at:new Date(payload.exp*1000).toISOString()})
    } catch { throw new AuthenticationError("ID token verification failed") }
  }
}
