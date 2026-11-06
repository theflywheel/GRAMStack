/** Validate the first-party consent handoff returned by the application's backend. */
export function consentURL(interactionURL: string, gatewayOrigin: string): string;
/** Open a verified consent handoff. Requires a user gesture in the browser. */
export function openConsent(interactionURL: string, gatewayOrigin: string): Window | null;
