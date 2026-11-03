const $ = (id) => document.getElementById(id);
const terminal = new Set([
  "COMPLETE",
  "UNKNOWN",
  "BLOCKED",
  "EXPIRED",
  "CANCELLED",
]);
let app = null,
  csrf = "",
  busy = false,
  timer,
  expiryTimer,
  delay = 2000,
  pendingCreate = null,
  unavailable = false,
  operation = "",
  needsOutcomeCheck = false;
const storedID = () => {
  try {
    return sessionStorage.getItem("sakhi.application") || "";
  } catch {
    return "";
  }
};
function remember(id) {
  try {
    id
      ? sessionStorage.setItem("sakhi.application", id)
      : sessionStorage.removeItem("sakhi.application");
  } catch {}
}
function show(id, visible) {
  $(id).hidden = !visible;
}
function notice(message = "") {
  $("connection-text").textContent = message;
  show("connection-message", !!message);
}
function link(id, value) {
  if (!value) {
    $(id).removeAttribute("href");
    return false;
  }
  try {
    const url = new URL(value);
    if (
      !["https:", "http:"].includes(url.protocol) ||
      url.username ||
      url.password
    )
      throw new Error();
    $(id).href = url.href;
    return true;
  } catch {
    $(id).removeAttribute("href");
    return false;
  }
}
async function api(path, method = "GET", input, key) {
  const headers = { Accept: "application/json" };
  if (method !== "GET") headers["X-CSRF-Token"] = csrf;
  if (input !== undefined) headers["Content-Type"] = "application/json";
  if (key) headers["Idempotency-Key"] = key;
  let response, value;
  try {
    response = await fetch(path, {
      method,
      headers,
      credentials: "same-origin",
      cache: "no-store",
      body: input === undefined ? undefined : JSON.stringify(input),
      signal: AbortSignal.timeout(42000),
    });
    value = await response.json();
  } catch {
    throw { code: "CONNECTION_INTERRUPTED" };
  }
  if (!response.ok)
    throw {
      code: value.error?.code || value.error,
      status: response.status,
      application: value.application,
    };
  return value;
}
function adopt(value) {
  const previous = app?.id || storedID();
  app = value;
  unavailable = false;
  needsOutcomeCheck = false;
  remember(app.id);
  render();
  if (previous !== app.id) $("progress-title").focus();
}
function render() {
  clearTimeout(expiryTimer);
  if (app) {
    const id = app.id,
      remaining = new Date(app.expires_at).getTime() - Date.now();
    expiryTimer = setTimeout(
      () => {
        if (app?.id !== id) return;
        failure({ status: 404 });
        $("membership-value").textContent = "";
        $("savings-value").textContent = "";
        $("receipt-reference").textContent = "";
      },
      Math.max(0, remaining),
    );
  }
  const hasApplication = !!app || !!storedID();
  show("new-application", !hasApplication);
  show("application-progress", hasApplication);
  $("start").disabled = busy || !csrf;
  $("start").textContent = pendingCreate
    ? "Retry the same application →"
    : "Start application →";
  $("member-reference").disabled = busy || !!pendingCreate;
  $("include-savings").disabled = busy || !!pendingCreate;
  for (const id of ["retrieve", "refresh", "cancel", "new"])
    $(id).disabled = busy;
  $("application-progress").setAttribute("aria-busy", String(busy));
  if (!app) {
    for (const id of ["membership-value", "savings-value", "receipt-reference"])
      $(id).textContent = "";
    show("new", unavailable);
    return;
  }
  const state = app.state,
    stopped = terminal.has(state) && state !== "COMPLETE";
  let title, copy, badge;
  switch (state) {
    case "CONNECTING":
      title = "Connecting your application";
      copy =
        "Your application is saved. We’re connecting the sharing request; retrying uses the same request.";
      badge = "Connecting";
      break;
    case "WAITING":
      title =
        app.platform_status === "CONSENT_PENDING"
          ? "Your sharing decision is next"
          : "Confirm your identity and record";
      copy =
        "Open the sharing request to sign in, verify your membership record, and review the exact notice. You can approve or decline.";
      badge = "Awaiting permission";
      break;
    case "READY":
      title = "Your permission is in place";
      copy =
        "You can now fetch the approved details for this credit review. Permission and scope are checked again before access.";
      badge = "Ready to share";
      break;
    case "FETCHING":
      title = "Bringing your approved details together";
      copy =
        "The one-use request is in progress. You can reload this page to check its saved outcome.";
      badge = "Sharing in progress";
      break;
    case "COMPLETE":
      title = "Your details are ready for review";
      copy =
        "Here’s the information shared for your SakhiCredit application, with your permission.";
      badge = "Details received";
      break;
    case "CANCELLING":
      title = "Stopping this sharing request";
      copy =
        "No new retrieval can start from this application. We’re confirming the cancellation with the platform.";
      badge = "Cancellation pending";
      break;
    case "CANCELLED":
      title = "Application cancelled";
      copy =
        "This sharing request has been cancelled. Start a new application if you want to request details again.";
      badge = "Cancelled";
      break;
    case "EXPIRED":
      title = "The sharing request has expired";
      copy =
        "This application can no longer fetch your details. A new application needs a fresh sharing decision.";
      badge = "Expired";
      break;
    case "BLOCKED":
      title =
        app.platform_status === "REJECTED"
          ? "Sharing declined"
          : app.platform_status === "WITHDRAWN"
            ? "Sharing withdrawn"
            : "Details could not be shared";
      copy =
        "No details were added to this application. To make a different request, start a new application and review its notice.";
      badge = "Sharing stopped";
      break;
    default:
      title = "We couldn’t confirm the result";
      copy =
        "No details are available here. To avoid sharing twice, this application will not fetch again. You can start a new application.";
      badge = "Result unconfirmed";
  }
  if (busy && operation === "retrieve" && !terminal.has(state)) {
    title = "Requesting your approved details";
    copy =
      "Checking current permission and bringing together only the approved fields. This operation will not be retried automatically.";
    badge = "Request in progress";
  }
  $("progress-title").textContent = title;
  $("progress-copy").textContent = copy;
  $("state-badge").textContent = badge;
  $("state-badge").classList.toggle("stopped", stopped);
  const progress =
    state === "COMPLETE" ? 4 : ["READY", "FETCHING"].includes(state) ? 3 : 2;
  for (const [index, id] of [
    "step-application",
    "step-permission",
    "step-details",
  ].entries()) {
    const step = index + 1;
    $(id).classList.toggle("done", step < progress);
    $(id).classList.toggle("current", step === progress && !stopped);
    $(id).removeAttribute("aria-current");
    if (step === progress && !stopped)
      $(id).setAttribute("aria-current", "step");
  }
  show("waiting-note", state === "WAITING");
  show("review", state === "WAITING" && link("review", app.interaction_url));
  show("retrieve", state === "READY" && !needsOutcomeCheck);
  show(
    "refresh",
    ["CONNECTING", "WAITING", "CANCELLING", "FETCHING"].includes(state),
  );
  $("refresh").textContent =
    state === "CONNECTING" ? "Retry connection" : "Check progress";
  show("cancel", ["WAITING", "READY"].includes(state));
  show("new", terminal.has(state));
  show("sharing-reference", !!app.request_id);
  $("application-reference").textContent = app.id;
  $("request-reference").textContent = app.request_id || "";
  $("retention-until").textContent = new Date(app.expires_at).toLocaleString(
    undefined,
    { dateStyle: "medium", timeStyle: "short" },
  );
  const result = state === "COMPLETE" && app.result;
  show("approved-result", !!result);
  show("receipt-reference-row", !!result);
  show(
    "consent-receipt",
    ["READY", "COMPLETE"].includes(state) &&
      link(
        "consent-receipt",
        app.interaction_url ? `${app.interaction_url}/receipt` : "",
      ),
  );
  if (result) {
    const names = {
      active: "Active member",
      inactive: "Inactive member",
      suspended: "Suspended",
    };
    $("membership-value").textContent =
      names[result.data.membership_status] || result.data.membership_status;
    const savings = result.data.savings_summary;
    show("savings-tile", !!savings);
    if (savings) {
      const n = savings.amount;
      $("savings-value").textContent =
        n > 0 && n < 1e-20
          ? `${n} INR`
          : new Intl.NumberFormat("en-IN", {
              style: "currency",
              currency: "INR",
              minimumFractionDigits: 2,
              maximumFractionDigits: 20,
            }).format(n);
    }
    $("receipt-reference").textContent = result.receipt_id;
  }
}
function failure(error) {
  if (error.application) adopt(error.application);
  if (operation === "retrieve") needsOutcomeCheck = true;
  if (error.status === 404 || error.status === 401) {
    clearTimeout(timer);
    clearTimeout(expiryTimer);
    for (const id of ["membership-value", "savings-value", "receipt-reference"])
      $(id).textContent = "";
    unavailable = true;
    app = null;
    show("application-progress", true);
    show("new-application", false);
    $("progress-title").textContent =
      error.status === 401
        ? "Your browser session has ended"
        : "This application is no longer available";
    $("progress-copy").textContent =
      "Applications are available only to the browser that created them, for a limited time. Start a new application to continue.";
    for (const id of [
      "review",
      "retrieve",
      "refresh",
      "cancel",
      "sharing-reference",
      "approved-result",
    ])
      show(id, false);
    show("new", true);
    notice("Your previous result cannot be opened from this session.");
    return;
  }
  const message =
    error.code === "INVALID_INPUT"
      ? "Check the member reference and try again."
      : error.code === "PLATFORM_DECLINED"
        ? "The sharing request cannot proceed. Check the application status below."
        : error.status === 403
          ? "Your session could not verify this action. Reload the page and try again."
          : "We’re having trouble connecting. Your saved application has not been replaced. Try again.";
  notice(message);
}
async function action(fn, kind = "refresh") {
  if (busy) return;
  clearTimeout(timer);
  busy = true;
  operation = kind;
  render();
  try {
    await fn();
    delay = 2000;
    notice();
  } catch (error) {
    delay = Math.min(delay * 2, 30000);
    failure(error);
  } finally {
    busy = false;
    operation = "";
    if (!unavailable) render();
    schedule();
  }
}
async function check() {
  const id = app?.id || storedID();
  if (!id || unavailable) return;
  let value = await api(`/api/applications/${encodeURIComponent(id)}`);
  if (["CONNECTING", "WAITING", "READY", "CANCELLING"].includes(value.state))
    value = await api(
      `/api/applications/${encodeURIComponent(id)}/refresh`,
      "POST",
    );
  adopt(value);
}
function schedule() {
  clearTimeout(timer);
  if (unavailable || !storedID() || (app && terminal.has(app.state))) return;
  timer = setTimeout(() => {
    if (document.visibilityState === "visible") action(check);
    else schedule();
  }, delay);
}
async function initialize() {
  const session = await api("/api/session");
  csrf = session.csrf_token;
  if (storedID()) await check();
  else render();
}
async function submitPending() {
  adopt(
    await api(
      "/api/applications",
      "POST",
      pendingCreate.input,
      pendingCreate.key,
    ),
  );
  pendingCreate = null;
}
$("application-form").addEventListener("submit", (event) => {
  event.preventDefault();
  if (!pendingCreate)
    pendingCreate = {
      key: crypto.randomUUID(),
      input: {
        member_reference: $("member-reference").value.trim(),
        include_savings: $("include-savings").checked,
      },
    };
  action(submitPending);
});
$("retrieve").addEventListener("click", () =>
  action(
    async () =>
      adopt(await api(`/api/applications/${app.id}/retrieve`, "POST")),
    "retrieve",
  ),
);
$("cancel").addEventListener("click", () =>
  action(async () =>
    adopt(await api(`/api/applications/${app.id}/cancel`, "POST")),
  ),
);
$("refresh").addEventListener("click", () => action(check));
$("retry").addEventListener("click", () =>
  action(async () => {
    if (!csrf) await initialize();
    else if (pendingCreate && !storedID()) await submitPending();
    else if (storedID() && !unavailable) await check();
    else if (!unavailable) render();
  }),
);
$("new").addEventListener("click", () => {
  remember("");
  app = null;
  pendingCreate = null;
  unavailable = false;
  notice();
  action(initialize);
  $("page-title").focus();
});
document.addEventListener("visibilitychange", () => {
  if (app && Date.now() >= new Date(app.expires_at).getTime()) {
    failure({ status: 404 });
    return;
  }
  if (
    document.visibilityState === "visible" &&
    storedID() &&
    !unavailable &&
    !busy &&
    (!app || !terminal.has(app.state))
  )
    action(check);
});
render();
action(initialize);
