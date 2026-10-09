// Real admin/researcher browser flow. Keep passwords only in protected state.
import {
  readFileSync,
  writeFileSync,
  mkdirSync,
  existsSync,
  chmodSync,
} from "node:fs";
import { randomBytes } from "node:crypto";
const { chromium } = await import(
  process.env.MIST_PLAYWRIGHT_MODULE ??
    "/tmp/mist-browser-check/node_modules/playwright/index.mjs"
);
const base = process.env.MIST_SITE_URL ?? "http://100.73.139.66:8088";
const out =
  process.env.MIST_EVIDENCE_DIR ??
  "/home/utmist/mist-department-results-2026-10-09";
mkdirSync(out, { recursive: true, mode: 0o700 });
const creds = JSON.parse(
  readFileSync("/home/utmist/.config/mist/admin-bootstrap.json"),
);
const file = out + "/browser-private-state.json";
const state = existsSync(file) ? JSON.parse(readFileSync(file)) : {};
const report = {
  checks: {},
  browser_errors: [],
  started: new Date().toISOString(),
};
function save() {
  writeFileSync(file, JSON.stringify(state, null, 2), { mode: 0o600 });
  chmodSync(file, 0o600);
}
function check(ok, message) {
  if (!ok) throw new Error(message);
}
function pass(name) {
  report.checks[name] = true;
  writeFileSync(out + "/browser-results.json", JSON.stringify(report, null, 2));
  console.log("PASS " + name);
}
const browser = await chromium.launch({ headless: true });
const ctx = await browser.newContext({
  baseURL: base,
  viewport: { width: 1600, height: 1100 },
});
const page = await ctx.newPage();
page.on("pageerror", (e) => report.browser_errors.push(e.message));
async function pick(target, label, optionsOrValue, maybeValue) {
  const value = maybeValue === undefined ? optionsOrValue : maybeValue;
  await target.getByLabel(label, { exact: true }).click();
  await target
    .locator("[role=option][data-value=" + JSON.stringify(String(value)) + "]")
    .click();
}
async function login(target, email, password) {
  await target.goto("/jobs");
  await target.getByLabel("Email", { exact: true }).fill(email);
  await target.getByLabel("Password", { exact: true }).fill(password);
  await target.getByRole("button", { name: "Sign in", exact: true }).click();
  await target.getByRole("heading", { name: "Jobs", exact: true }).waitFor();
}
async function api(context, path, method = "GET", data, team, expected = 200) {
  let r;
  try {
    r = await context.request.fetch(base + path, {
      method,
      data,
      headers: { Origin: base, ...(team ? { "X-Mist-Team": team } : {}) },
    });
  } catch {
    throw new Error(
      method + " " + path + " transport failure; no credentials logged",
    );
  }
  check(
    r.status() === expected,
    `${path}: expected ${expected}, got ${r.status()}`,
  );
  return r.headers()["content-type"]?.includes("json") ? r.json() : r.body();
}
try {
  await login(page, creds.email, creds.password);
  pass("admin_browser_login");
  const account = (await api(ctx, "/api/session")).user;
  if (state.team && process.env.MIST_VERIFY_NEW_TEAM === "1") {
    await api(ctx, `/api/teams/${state.team.id}`, "PATCH", {
      disabled: true,
      name: "Previous browser verification",
    });
    delete state.team;
    save();
  }
  if (state.member && process.env.MIST_VERIFY_NEW_MEMBER === "1") {
    await api(ctx, "/auth/admin/ban-user", "POST", {
      userId: state.member.id,
      banReason: "Replaced browser verification fixture",
    });
    delete state.member;
    save();
  }
  if (!state.member) {
    const m = {
      name: "Browser verification researcher",
      email: `browser-${Date.now()}@mist.invalid`,
      password: randomBytes(20).toString("base64url"),
    };
    await page.goto("/profile");
    await page.getByRole("tab", { name: "Members", exact: true }).click();
    await page.getByRole("button", { name: "New member", exact: true }).click();
    await page.getByLabel("Name", { exact: true }).fill(m.name);
    await page.getByLabel("Email", { exact: true }).fill(m.email);
    await page.getByLabel("Initial password").fill(m.password);
    const response = page.waitForResponse((r) =>
      r.url().endsWith("/auth/admin/create-user"),
    );
    await page.getByRole("button", { name: "Add member", exact: true }).click();
    const created = await response;
    check(created.ok(), "UI account creation failed");
    m.id = (await created.json()).user.id;
    state.member = m;
    save();
  }
  await api(ctx, "/auth/admin/unban-user", "POST", { userId: state.member.id });
  pass("browser_account_creation");
  if (!state.team) {
    await page.goto("/teams");
    await page.getByRole("button", { name: "New team", exact: true }).click();
    await page.getByLabel("New team name").fill("Browser verification");
    await page.getByLabel("Dataset storage (GiB)", { exact: true }).fill("1");
    await page.getByLabel("Model storage (GiB)", { exact: true }).fill("1");
    const response = page.waitForResponse(
      (r) => r.url().endsWith("/api/teams") && r.request().method() === "POST",
    );
    await page
      .getByRole("button", { name: "Create team", exact: true })
      .click();
    const created = await response;
    check(created.ok(), "UI team creation failed");
    state.team = await created.json();
    save();
  }
  const team = state.team.id;
  await api(ctx, `/api/teams/${team}`, "PATCH", { disabled: false });
  await api(ctx, `/api/teams/${team}/members`, "POST", {
    user_id: account.id,
    common_writer: true,
  });
  await page.goto("/teams");
  await pick(page, "Workspace", { exact: true }, team);
  await page
    .getByRole("button", { name: "Manage Browser verification", exact: true })
    .click();
  const section = page.getByRole("dialog", {
    name: "Team settings",
    exact: true,
  });
  await pick(section, "Existing Mist account", state.member.id);
  await section.getByLabel("Can write common folder").check();
  await section
    .getByRole("button", { name: "Add/update teammate", exact: true })
    .click();
  await section.locator("li").filter({ hasText: state.member.email }).waitFor();
  await page.getByRole("tab", { name: "Limits", exact: true }).click();
  await section.getByLabel("Total CPU cores").fill("4");
  await section.getByLabel("Total memory (for example 16Gi)").fill("8Gi");
  await section
    .getByLabel("Maximum job runtime (seconds)", { exact: true })
    .fill("120");
  await section
    .getByRole("button", { name: "Save limits", exact: true })
    .click();
  await section
    .getByRole("status")
    .filter({ hasText: "Team limits saved." })
    .waitFor();
  pass("browser_team_creation_membership_common_write_and_policy");
  await page
    .getByRole("button", { name: "Close Team settings", exact: true })
    .click();
  await page.goto("/datasets");
  await page.getByRole("button", { name: "Add dataset", exact: true }).click();
  await page
    .getByLabel("Dataset name (optional)")
    .fill("Browser-uploaded dataset");
  await pick(page, "Upload folder", "common");
  await page.getByLabel("File or ZIP archive").setInputFiles({
    name: "ui.txt",
    mimeType: "text/plain",
    buffer: Buffer.from("BROWSER_UPLOADED_DATA"),
  });
  await page
    .getByRole("button", { name: "Upload dataset", exact: true })
    .click({ timeout: 120000 });
  await page
    .getByRole("status")
    .filter({ hasText: "is ready to use" })
    .waitFor({ timeout: 60000 });
  const ds = (
    await api(ctx, "/api/datasets", "GET", undefined, team)
  ).datasets.find((d) => d.name === "Browser-uploaded dataset");
  check(ds, "Browser dataset missing");
  state.dataset = ds;
  save();
  await page.locator(".mist-folder-details > summary").click();
  await pick(page, "Browse common or teammate files", team + ":common");
  await page
    .getByRole("link", { name: "Download", exact: true })
    .first()
    .waitFor();
  const downloaded = page.waitForEvent("download");
  await page
    .getByRole("link", { name: "Download", exact: true })
    .first()
    .click();
  check(
    (await (await downloaded).failure()) === null,
    "Browser dataset download failed",
  );
  await page.screenshot({
    path: out + "/final-datasets-desktop.png",
    fullPage: true,
  });
  pass("browser_upload_common_folder_browse_and_download");
  const memberCtx = await browser.newContext({
      baseURL: base,
      viewport: { width: 1440, height: 1000 },
    }),
    member = await memberCtx.newPage();
  member.on("pageerror", (e) => report.browser_errors.push(e.message));
  await login(member, state.member.email, state.member.password);
  await pick(member, "Workspace", { exact: true }, team);
  await member.getByRole("button", { name: "New job", exact: true }).click();
  const jobName = "Browser container experiment " + Date.now();
  await member.waitForFunction(
    () => document.querySelector("#job-timeout")?.value === "120",
  );
  pass("job_defaults_follow_loaded_team_policy");
  await member.getByLabel("Name", { exact: true }).fill(jobName);
  await member.getByLabel("Container image").fill("python:3.11-slim");
  await pick(member, "Dataset (optional)", ds.id);
  await member
    .getByLabel("Python script", { exact: true })
    .fill(
      "from pathlib import Path\nassert Path('/inputs/ui.txt').read_text()=='BROWSER_UPLOADED_DATA'\nPath('/outputs/browser-result.txt').write_text('BROWSER_JOB_PASSED')\nprint('BROWSER_JOB_PASSED',flush=True)\n",
    );
  await member.getByText("Resources and environment", { exact: true }).click();
  await member.getByLabel("Deadline (seconds)").fill("120");
  const submission = member.waitForResponse(
    (r) => r.url().endsWith("/api/jobs") && r.request().method() === "POST",
  );
  await member.getByRole("button", { name: "Submit job", exact: true }).click();
  const submitted = await submission;
  check(submitted.ok(), "Browser job submission failed: " + submitted.status() + " " + (submitted.ok() ? "" : await submitted.text()));
  const id = (await submitted.json()).job_id;
  report.job_id = id;
  for (let i = 0; i < 100; i++) {
    const j = await api(memberCtx, `/api/jobs/${id}`, "GET", undefined, team);
    if (j.job_state === "Success") break;
    check(
      !["Failure", "Cancelled"].includes(j.job_state),
      "Browser container experiment failed",
    );
    await new Promise((r) => setTimeout(r, 2000));
  }
  check(
    (await api(memberCtx, `/api/jobs/${id}`, "GET", undefined, team))
      .job_state === "Success",
    "Browser job did not finish",
  );
  await member
    .getByRole("button", { name: "Show details for " + jobName, exact: true })
    .click();
  await member.getByRole("button", { name: "Logs", exact: true }).click();
  await member.getByText("BROWSER_JOB_PASSED", { exact: false }).waitFor();
  await member.getByRole("button", { name: "Files", exact: true }).click();
  const output = member.waitForEvent("download");
  await member
    .getByRole("link", { name: "browser-result.txt", exact: true })
    .click();
  await (await output).saveAs(out + "/browser-result.txt");
  pass("regular_browser_selfservice_image_submit_expand_logs_result_download");
  // A regular account has no administrator controls and cannot reset accounts.
  await member.goto("/teams");
  check(
    (await member
      .getByRole("button", { name: "Create team", exact: true })
      .count()) === 0,
    "Regular user sees team administration",
  );
  await api(
    memberCtx,
    "/auth/admin/set-user-password",
    "POST",
    { userId: account.id, newPassword: randomBytes(20).toString("base64url") },
    undefined,
    403,
  );
  pass("regular_browser_admin_controls_denied");
  // Admin resets a test account's password in the same portal.
  await page.goto("/profile");
  await page.getByRole("tab", { name: "Members", exact: true }).click();
  await page
    .getByRole("button", { name: "Reset password", exact: true })
    .click();
  await pick(page, "Account to reset", state.member.id);
  const next = randomBytes(20).toString("base64url");
  await page.getByLabel("New temporary password").fill(next);
  await page
    .getByRole("button", {
      name: "Reset password and sign out sessions",
      exact: true,
    })
    .click();
  await page
    .getByRole("status")
    .filter({ hasText: "Password reset and existing sessions signed out." })
    .waitFor();
  await api(memberCtx, "/api/jobs", "GET", undefined, team, 401);
  state.member.password = next;
  save();
  await memberCtx.close();
  const reloginCtx = await browser.newContext({ baseURL: base }),
    relogin = await reloginCtx.newPage();
  await login(relogin, state.member.email, next);
  await reloginCtx.close();
  pass("browser_password_reset_revokes_sessions_new_login_works");
  await page
    .getByRole("button", { name: "Close Reset password", exact: true })
    .click();
  await page.goto("/jobs");
  await pick(page, "Workspace", { exact: true }, team);
  await page
    .getByRole("button", { name: "Show details for " + jobName, exact: true })
    .click();
  await page.screenshot({
    path: out + "/final-jobs-desktop.png",
    fullPage: true,
  });
  await page
    .getByRole("button", { name: "Hide details for " + jobName, exact: true })
    .click();
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({
    path: out + "/final-jobs-mobile.png",
    fullPage: true,
  });
  check(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
    "Mobile viewport overflows horizontally",
  );
  await page.setViewportSize({ width: 1600, height: 1100 });
  await page.getByRole("button", { name: "New job", exact: true }).click();
  await page.getByRole("radio", { name: /Tenstorrent/ }).check();
  await page.waitForFunction(() =>
    document
      .querySelector('input[name="compute"]:checked')
      ?.closest("label")
      ?.classList.contains("is-selected"),
  );
  await page
    .getByRole("radio", { name: "Training check", exact: true })
    .check();
  await page.screenshot({
    path: out + "/final-job-composer.png",
    fullPage: true,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  check(
    await page
      .getByRole("dialog")
      .evaluate((el) => el.scrollWidth <= el.clientWidth),
    "Mobile job composer overflows horizontally",
  );
  await page.screenshot({
    path: out + "/final-job-composer-mobile.png",
    fullPage: false,
  });
  await page
    .getByRole("button", { name: "Close New job", exact: true })
    .click();
  await page.setViewportSize({ width: 1600, height: 1100 });
  pass("responsive_job_dialog_and_device_slider");
  await pick(page, "Workspace", { exact: true }, "");
  await page.waitForFunction(
    () => !document.querySelector('[aria-label="Next page"]')?.disabled,
  );
  check(
    (await page.getByRole("button", { name: /Show details for/ }).count()) <=
      10,
    "History exceeds page size",
  );
  check(
    await page
      .getByRole("button", { name: "Next page", exact: true })
      .isEnabled(),
    "Preserved history has no next page",
  );
  await page.getByRole("button", { name: "Next page", exact: true }).click();
  await page.getByRole("status").filter({ hasText: "Page 2 of" }).waitFor();
  await page
    .getByRole("button", { name: "Previous page", exact: true })
    .click();
  await page.getByRole("status").filter({ hasText: "Page 1 of" }).waitFor();
  pass("history_pagination_next_previous_real_legacy_jobs");
  await pick(page, "Workspace", { exact: true }, team);
  await page.goto("/dashboard");
  await page.screenshot({ path: out + "/final-overview.png", fullPage: true });
  pass("desktop_mobile_layout_no_overflow_or_browser_errors");
  await api(ctx, `/api/teams/${team}`, "PATCH", { disabled: true });
  await api(ctx, "/auth/admin/ban-user", "POST", {
    userId: state.member.id,
    banReason: "Browser verification complete",
  });
  const pilot = JSON.parse(readFileSync(out + "/private-state.json")).a.id;
  await pick(page, "Workspace", { exact: true }, pilot);
  check(report.browser_errors.length === 0, report.browser_errors.join("; "));
  report.finished = new Date().toISOString();
  writeFileSync(out + "/browser-results.json", JSON.stringify(report, null, 2));
  console.log(JSON.stringify(report, null, 2));
} catch (e) {
  report.error = e.message;
  writeFileSync(out + "/browser-results.json", JSON.stringify(report, null, 2));
  throw e;
} finally {
  await browser.close();
}
