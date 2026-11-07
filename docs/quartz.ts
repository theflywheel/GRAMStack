import { loadQuartzConfig, loadQuartzLayout } from "./quartz/plugins/loader/config-loader"

import { componentRegistry } from "./quartz/components/registry"

// Explicit reading order; chapter numbering stays inside Architecture.
componentRegistry.setOptionOverrides("@quartz-community/explorer", {
  sortFn: (a: { slug: string; displayName: string }, b: { slug: string; displayName: string }) => {
    const order = [
      "architecture/index", "implementation/index", "developers/index", "demos/index",
      "implementation/components", "implementation/authentication", "implementation/consent-and-retrieval",
      "implementation/persistence-and-evidence", "implementation/deployment", "implementation/verification",
      "developers/quickstart", "developers/sdks/index", "developers/adapters", "developers/api-reference/index", "developers/specifications/index",
      "developers/sdks/go", "developers/sdks/python", "developers/sdks/javascript",
      "developers/specifications/core/index", "developers/specifications/profiles/index", "developers/specifications/adapters/index", "developers/specifications/schemas/index", "developers/specifications/conformance",
      "demos/sakhicredit/try-it", "demos/sakhicredit/walkthrough", "demos/sakhicredit/capabilities",
    ]
    const left = order.indexOf(a.slug)
    const right = order.indexOf(b.slug)
    return (left < 0 ? 100 : left) - (right < 0 ? 100 : right) || a.displayName.localeCompare(b.displayName, undefined, { numeric: true })
  },
})

const config = await loadQuartzConfig()
export default config
export const layout = await loadQuartzLayout()
