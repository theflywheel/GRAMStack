// Browser helpers carry no client secrets, retrieval grants or mTLS keys.
export function consentURL(interactionURL,gatewayOrigin) {
  const origin=new URL(gatewayOrigin)
  const target=new URL(interactionURL)
  if (origin.username || origin.password || origin.pathname!=="/" || origin.search || origin.hash || target.origin!==origin.origin || target.username || target.password || target.search || target.hash ||
      !/^\/interactions\/[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$/.test(target.pathname) ||
      (target.protocol!=="https:" && !(target.protocol==="http:" && ["127.0.0.1","[::1]"].includes(target.hostname)))) {
    throw new Error("Untrusted GRAMStack consent destination")
  }
  return target.href
}
export function openConsent(interactionURL,gatewayOrigin) {
  return window.open(consentURL(interactionURL,gatewayOrigin),"_blank","noopener,noreferrer")
}
