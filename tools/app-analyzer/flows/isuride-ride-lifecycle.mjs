// ISURIDE: drive one ride through MATCHING -> ENROUTE -> PICKUP -> CARRYING -> ARRIVED -> COMPLETED.
// The client UI stays open (its notification polling is recorded); the chair side and the
// write APIs are called with fetch() *inside the page* so they show up in the network log too.
// Requires persona.vars.chairToken (chair_session) and optionally persona.vars.paymentToken.
export default async function (page, { persona, cfg, step, api: apiWithTimeout }) {
  const vars = persona.vars ?? {};
  // `api` from the crawler has a timeout (cfg.apiTimeoutMs); the inline fallback is for running this flow elsewhere
  const api = apiWithTimeout ?? ((method, path, body) =>
    page.evaluate(
      async ({ method, path, body }) => {
        const r = await fetch(path, { method, headers: { "content-type": "application/json" }, body: body ? JSON.stringify(body) : undefined });
        let json = null;
        try { json = await r.json(); } catch {}
        return { status: r.status, json };
      },
      { method, path, body },
    ));
  const until = async (label, fn, timeoutMs = 20000, every = 300) => {
    const t0 = Date.now();
    while (Date.now() - t0 < timeoutMs) {
      const v = await fn();
      if (v) return v;
      await page.waitForTimeout(every);
    }
    throw new Error(`timeout waiting for ${label}`);
  };
  let pickup = { latitude: 0, longitude: 0 };
  let dest = { latitude: 0, longitude: 6 };

  await step("open client app (user)");
  await page.goto(new URL("/client", cfg.baseURL).toString(), { waitUntil: "domcontentloaded" });
  await page.waitForTimeout(1500);

  if (vars.chairToken) {
    await page.context().addCookies([{ name: "chair_session", value: vars.chairToken, url: cfg.baseURL }]);
  }
  if (vars.paymentToken) {
    await step("register payment method");
    await api("POST", "/api/app/payment-methods", { token: vars.paymentToken });
  }

  await step("chair: activate + report coordinate");
  console.error("  activity:", JSON.stringify(await api("POST", "/api/chair/activity", { is_active: true })));
  console.error("  coordinate:", JSON.stringify(await api("POST", "/api/chair/coordinate", pickup)));

  await step("user: check current ride");
  const cur = (await api("GET", "/api/app/notification")).json?.data;
  let rideId;
  if (cur && cur.status !== "COMPLETED") {
    console.error("  resuming in-progress ride:", JSON.stringify(cur).slice(0, 200));
    rideId = cur.ride_id;
    pickup = cur.pickup_coordinate;
    dest = cur.destination_coordinate;
  } else {
    await step("user: estimated fare + request ride");
    console.error("  fare:", JSON.stringify(await api("POST", "/api/app/rides/estimated-fare", { pickup_coordinate: pickup, destination_coordinate: dest })));
    const created = await api("POST", "/api/app/rides", { pickup_coordinate: pickup, destination_coordinate: dest });
    console.error("  ride:", JSON.stringify(created));
    rideId = created.json?.ride_id;
    if (!rideId) throw new Error(`ride not created: ${JSON.stringify(created)}`);
  }

  await step("wait for matching (server-side matcher)");
  await until("chair notification MATCHING", async () => {
    const n = await api("GET", "/api/chair/notification");
    return n.json?.data?.ride_id === rideId ? n.json.data : null;
  });

  const status = async () => (await api("GET", "/api/app/notification")).json?.data?.status;
  let st = await status();
  console.error("  status now:", st);
  if (st === "MATCHING") {
    await step("chair: ENROUTE");
    console.error("  enroute:", JSON.stringify(await api("POST", `/api/chair/rides/${rideId}/status`, { status: "ENROUTE" })));
    await page.waitForTimeout(1200);
    st = await status();
  }
  if (st === "ENROUTE") {
    await step("chair: arrive at pickup -> PICKUP");
    await api("POST", "/api/chair/coordinate", pickup);
    await until("user sees PICKUP", async () => (await status()) === "PICKUP");
    await page.waitForTimeout(1200);
    st = await status();
  }
  if (st === "PICKUP") {
    await step("chair: CARRYING");
    console.error("  carrying:", JSON.stringify(await api("POST", `/api/chair/rides/${rideId}/status`, { status: "CARRYING" })));
    await page.waitForTimeout(1200);
    st = await status();
  }
  if (st === "CARRYING") {
    await step("chair: move to destination -> ARRIVED");
    const steps = 4;
    for (let i = 1; i <= steps; i++) {
      const c = { latitude: Math.round(pickup.latitude + ((dest.latitude - pickup.latitude) * i) / steps), longitude: Math.round(pickup.longitude + ((dest.longitude - pickup.longitude) * i) / steps) };
      await api("POST", "/api/chair/coordinate", c);
      await page.waitForTimeout(200);
    }
    await api("POST", "/api/chair/coordinate", dest);
    await until("user sees ARRIVED", async () => (await status()) === "ARRIVED");
  }
  await page.waitForTimeout(1200);
  await step("user: evaluate -> COMPLETED");
  console.error("  evaluation:", JSON.stringify(await api("POST", `/api/app/rides/${rideId}/evaluation`, { evaluation: 5 })));
  await until("user sees COMPLETED", async () => (await api("GET", "/api/app/notification")).json?.data?.status === "COMPLETED", 10000).catch((e) => console.error("  ", e.message));
  await step("history after ride");
  console.error("  rides:", JSON.stringify(await api("GET", "/api/app/rides")).slice(0, 300));
  await page.waitForTimeout(1500);
}
