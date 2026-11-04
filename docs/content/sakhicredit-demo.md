---
title: SakhiCredit demo
description: Watch the live GRAMStack consent and data-sharing walkthrough.
---

Watch SakhiCredit request two fields, show an exact consent notice, and retrieve the approved details. The walkthrough also shows a refused request, consent history, receipt export, and a correction case queued for the registry.

<video controls playsinline preload="metadata" style="width:100%;height:auto" aria-label="SakhiCredit hosted demo">
  <source src="/media/sakhicredit-hosted.mp4" type="video/mp4">
</video>

<a href="https://docs.gramstack.try-dough.com/media/sakhicredit-hosted.mp4?download=1" download>Download the MP4</a> · [Try the live SakhiCredit demo](https://sakhicredit-b9b69819.up.railway.app/)

The 96-second walkthrough includes timed on-screen explanations, a capability report, and an audit-events chapter starting at 1:12. It uses fictional identities, one-time codes, and source records.


## Try the access audit

1. Start a new application and uncheck **Savings summary** so the request contains just membership status. Sign in using the fictional identity shown in SakhiCredit, verify membership, and approve the notice.
2. Return to SakhiCredit and tap **Get approved details**, then **View access audit in consent history**. Sign in again. The audit is available after a reload or a fresh session. [Open consent history](https://gramstack-gateway-b9b69819.up.railway.app/consent-history/lokos)
3. Find your completed request. The card shows recipient application `consumer-one` (SakhiCredit), field `membership_status`, the UTC response time, and the signed receipt reference. **1 of 1 authorized retrievals used** means the same consent cannot authorize another retrieval through GRAMStack.
4. Check the receipt's DeDi status. Anchoring runs asynchronously, so a new receipt can show **DeDi anchoring pending** before it shows **DeDi commitment verified**.
5. Decline a separate request and check its history. It should show **No completed disclosure is recorded for this request**. An uncertain response attempt has a separate **Delivery is unconfirmed** message.

### What happened behind the screen

When the adapter first reserved the approved use, the gateway committed `request.transitioned` → `REDEEMING`. The signed evidence, response outcome and DeDi commitment are tracked separately. The audit card shows the gateway's response time. The internal reservation records when the approval was used, which happens earlier. [Read the event-by-event architecture](08-state-and-evidence#approval-use-events-in-the-running-system)
