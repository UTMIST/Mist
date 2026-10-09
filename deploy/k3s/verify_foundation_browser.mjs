// Requires Playwright + its Chromium browser, installed outside the app.
// MIST_PLAYWRIGHT_MODULE may point to an existing Playwright index.mjs.
import fs from "node:fs";
import path from "node:path";

const { chromium } = await import(
  process.env.MIST_PLAYWRIGHT_MODULE || "playwright"
);
const artifacts = process.argv[2];
if (!artifacts)
  throw new Error(
    "Usage: node verify_foundation_browser.mjs ARTIFACT_DIRECTORY",
  );
fs.mkdirSync(artifacts, { recursive: true });
const url = process.env.MIST_WEB_URL || "http://127.0.0.1:3001";
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: { width: 1440, height: 1080 } });
const errors = [];
page.on("pageerror", (error) => errors.push(error.message));
const evidence = {
  started_at: new Date().toISOString(),
  jobs: {},
  page_errors: errors,
};
const run = `foundation-${Date.now()}`;

async function api(method, route) {
  const response = await page.request.fetch(url + "/api" + route, { method });
  if (!response.ok()) throw new Error(`${route}: ${await response.text()}`);
  return response.json();
}
async function waitFor(check, description, timeout = 240000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    const value = await check();
    if (value) return value;
    await page.waitForTimeout(1000);
  }
  throw new Error(`Timed out: ${description}`);
}
async function finished(id, desired = "Success") {
  const job = await waitFor(async () => {
    const value = await api("GET", "/jobs/" + id);
    if (["Success", "Failure", "Cancelled"].includes(value.job_state)) {
      if (value.job_state !== desired) throw new Error(JSON.stringify(value));
      return value;
    }
  }, id);
  const { logs } = await api("GET", "/jobs/" + id + "/logs");
  fs.writeFileSync(path.join(artifacts, id + ".log"), logs);
  return { job, logs };
}
function card(name) {
  return page
    .getByRole("heading", { name, exact: true })
    .locator("..")
    .locator("..");
}
async function submit(label) {
  const name = `${run}-${label}`;
  await page.getByLabel("Name", { exact: true }).fill(name);
  const response = page.waitForResponse(
    (response) =>
      response.url().endsWith("/api/jobs") &&
      response.request().method() === "POST",
  );
  await page.getByRole("button", { name: "Submit job", exact: true }).click();
  const result = await response;
  if (!result.ok()) throw new Error(await result.text());
  const id = (await result.json()).job_id;
  console.log(`Submitted through browser: ${label} ${id}`);
  evidence.jobs[label] = { id, name };
  return { id, name };
}
async function completed(entry, desired, marker) {
  const result = await finished(entry.id, desired);
  const label =
    desired === "Success"
      ? "Completed"
      : desired === "Failure"
        ? "Failed"
        : "Cancelled";
  await card(entry.name)
    .getByText(label, { exact: true })
    .waitFor({ timeout: 15000 });
  if (!result.logs.includes(marker))
    throw new Error("Missing log marker: " + marker);
  await card(entry.name)
    .getByRole("button", { name: "Logs", exact: true })
    .click();
  await page
    .locator("pre")
    .filter({ hasText: marker })
    .waitFor({ timeout: 15000 });
  await page.getByRole("button", { name: "Close", exact: true }).click();
  return result;
}

try {
  await page.goto(url + "/jobs");
  await page.getByRole("button", { name: "Submit job", exact: true }).waitFor();
  await waitFor(
    () =>
      page.getByRole("button", { name: "Submit job", exact: true }).isEnabled(),
    "catalog ready",
  );
  evidence.hardware_before = await api("GET", "/hardware");

  await page.getByLabel("Workload", { exact: true }).selectOption("container");
  await page
    .getByLabel("Container image", { exact: true })
    .fill("mist-training:cpu-v1");
  await page.getByText("Resources and environment", { exact: true }).click();
  await page
    .getByLabel("Environment variables (NAME=value, one per line)", {
      exact: true,
    })
    .fill("GREETING=browser environment preserved");
  const cpu = await submit("packaged-cpu");
  Object.assign(
    evidence.jobs["packaged-cpu"],
    await completed(cpu, "Success", "PACKAGED_TRAINING_PASSED backend=cpu"),
  );
  if (
    !evidence.jobs["packaged-cpu"].logs.includes('"cwd": "/app"') ||
    !evidence.jobs["packaged-cpu"].logs.includes(
      "browser environment preserved",
    )
  )
    throw new Error("Image defaults or environment were lost");
  console.log("CPU packaged entrypoint, WORKDIR, training, and logs passed");

  await page.getByLabel("Compute", { exact: true }).selectOption("tenstorrent");
  await page.getByLabel("Boards (two chips each)", { exact: true }).fill("2");
  await page
    .getByLabel("Workload", { exact: true })
    .selectOption("training-smoke");
  const tt = await submit("tt-two-boards");
  Object.assign(
    evidence.jobs["tt-two-boards"],
    await completed(
      tt,
      "Success",
      "TRAINING_PASSED backend=tenstorrent devices=4",
    ),
  );
  // DRA allocation details can disappear once a completed pod releases its claim.
  if (
    evidence.jobs["tt-two-boards"].job.node !== "utmist-tt" ||
    evidence.jobs["tt-two-boards"].job.device_count !== 2
  )
    throw new Error("Incorrect TT placement/allocation");
  console.log("Tenstorrent two-board/four-chip training passed");

  await page.getByLabel("Compute", { exact: true }).selectOption("nvidia");
  await page.getByLabel("GPUs", { exact: true }).fill("2");
  await page.getByLabel("Workload", { exact: true }).selectOption("container");
  await page
    .getByLabel("Container image", { exact: true })
    .fill("mist-training:nvidia-v1");
  await page
    .getByLabel("Arguments (one per line)", { exact: true })
    .fill("--backend\nnvidia\n--devices\n2");
  const nvidia = await submit("packaged-two-gpus");
  Object.assign(
    evidence.jobs["packaged-two-gpus"],
    await completed(
      nvidia,
      "Success",
      "PACKAGED_TRAINING_PASSED backend=nvidia",
    ),
  );
  if (evidence.jobs["packaged-two-gpus"].job.node !== "utmist-z1opa08")
    throw new Error("Incorrect NVIDIA placement");
  console.log("Both NVIDIA GPUs trained in a packaged image");

  await page.getByLabel("Compute", { exact: true }).selectOption("cpu");
  await page.getByLabel("Workload", { exact: true }).selectOption("script");
  await page
    .getByLabel("Python script", { exact: true })
    .fill(
      "import sys\nprint('BROWSER_EXPECTED_FAILURE', flush=True)\nsys.exit(17)\n",
    );
  const failed = await submit("failure");
  Object.assign(
    evidence.jobs.failure,
    await completed(failed, "Failure", "BROWSER_EXPECTED_FAILURE"),
  );
  if (evidence.jobs.failure.job.exit_code !== 17)
    throw new Error("Missing failed exit code");

  await page
    .getByLabel("Python script", { exact: true })
    .fill(
      "import time\nprint('BROWSER_CANCEL_PRESERVED', flush=True)\ntime.sleep(120)\n",
    );
  const cancelled = await submit("cancel");
  await waitFor(
    async () =>
      (await api("GET", "/jobs/" + cancelled.id + "/logs")).logs.includes(
        "BROWSER_CANCEL_PRESERVED",
      ),
    "cancel job running",
  );
  await card(cancelled.name)
    .getByRole("button", { name: "Cancel", exact: true })
    .click();
  Object.assign(
    evidence.jobs.cancel,
    await completed(cancelled, "Cancelled", "BROWSER_CANCEL_PRESERVED"),
  );
  console.log("Browser failure and cancellation with retained logs passed");

  await page.evaluate(() => window.scrollTo(0, 0));
  await page.screenshot({ path: path.join(artifacts, "jobs-browser.png") });
  await card(nvidia.name)
    .getByRole("button", { name: "Logs", exact: true })
    .click();
  await page
    .locator("pre")
    .filter({ hasText: "PACKAGED_TRAINING_PASSED backend=nvidia" })
    .waitFor();
  await page
    .locator("pre")
    .screenshot({ path: path.join(artifacts, "nvidia-training-browser.png") });
  await page.goto(url + "/machines");
  await page
    .getByRole("rowheader", { name: "Tenstorrent boards", exact: true })
    .waitFor();
  await page.screenshot({ path: path.join(artifacts, "machines-browser.png") });
  evidence.hardware_after = await api("GET", "/hardware");
  if (errors.length) throw new Error("Browser errors: " + errors.join("\n"));
  evidence.completed_at = new Date().toISOString();
  console.log("FOUNDATION_BROWSER_PASSED");
} catch (error) {
  evidence.error = String(error);
  await page
    .screenshot({ path: path.join(artifacts, "browser-failure.png") })
    .catch(() => {});
  throw error;
} finally {
  fs.writeFileSync(
    path.join(artifacts, "browser.json"),
    JSON.stringify(evidence, null, 2) + "\n",
  );
  await browser.close();
}
