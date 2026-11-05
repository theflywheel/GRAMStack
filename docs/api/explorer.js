const selected = new URLSearchParams(window.location.search).get("service")
const catalogues = [
  ["gateway", "Gateway · public and browser"],
  ["gateway-private", "Gateway · private mTLS"],
  ["adapter", "LokOS adapter"],
  ["sakhicredit", "SakhiCredit product"],
  ["protocol", "Protocol · core 0.3.0"],
]
window.ui = SwaggerUIBundle({
  dom_id: "#swagger-ui",
  urls: catalogues.map(([id, name]) => ({ name, url: `/api/${id}.json` })),
  "urls.primaryName": catalogues.find(([id]) => id === selected)?.[1] ?? catalogues[0][1],
  presets: [SwaggerUIBundle.presets.apis, SwaggerUIStandalonePreset],
  layout: "StandaloneLayout",
  deepLinking: true,
  filter: true,
  docExpansion: "list",
  defaultModelsExpandDepth: -1,
  displayRequestDuration: false,
  supportedSubmitMethods: [],
  tryItOutEnabled: false,
  persistAuthorization: false,
  validatorUrl: null,
  queryConfigEnabled: false,
})
