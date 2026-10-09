// Run from the repo root. Playwright is supplied externally; credentials and
// cookies stay in a protected local state file, never in the public report.
import {
  readFileSync,
  writeFileSync,
  mkdirSync,
  existsSync,
  chmodSync,
} from "node:fs";
import { randomBytes } from "node:crypto";
import { execFileSync } from "node:child_process";
const { chromium } = await import(
  process.env.MIST_PLAYWRIGHT_MODULE ??
    "/tmp/mist-browser-check/node_modules/playwright/index.mjs"
);
const base = process.env.MIST_SITE_URL ?? "http://100.73.139.66:8088";
const out =
  process.env.MIST_EVIDENCE_DIR ??
  "/home/utmist/mist-department-results-2026-10-09";
mkdirSync(out, { recursive: true, mode: 0o700 });
const statePath = out + "/private-state.json";
const credentials = JSON.parse(
  readFileSync("/home/utmist/.config/mist/admin-bootstrap.json"),
);
const browser = await chromium.launch({ headless: true });
const admin = await browser.newContext({
  baseURL: base,
  viewport: { width: 1440, height: 1000 },
});
const errors = [];
const state = existsSync(statePath) ? JSON.parse(readFileSync(statePath)) : {};
const report = {
  started: new Date().toISOString(),
  site: base,
  checks: {},
  jobs: [],
  teams: [],
};
function save() {
  writeFileSync(statePath, JSON.stringify(state, null, 2), { mode: 0o600 });
  chmodSync(statePath, 0o600);
}
function check(ok, message) {
  if (!ok) throw new Error(message);
}
function passed(name) {
  report.checks[name] = true;
  writeFileSync(out + "/results.json", JSON.stringify(report, null, 2));
  console.log("PASS " + name);
}
async function call(
  ctx,
  path,
  method = "GET",
  data,
  team = "",
  expected = 200,
) {
  let r;
  for (let attempt = 0; attempt < (method === "GET" ? 4 : 1); attempt++) {
    try {
      r = await ctx.request.fetch(base + path, {
        method,
        data,
        timeout: 15000,
        headers: { Origin: base, ...(team ? { "X-Mist-Team": team } : {}) },
      });
      if (method === "GET" && [502, 503].includes(r.status()) && attempt < 3) {
        await new Promise((resolve) => setTimeout(resolve, 1500));
        continue;
      }
      break;
    } catch {
      if (method === "GET" && attempt < 3) {
        await new Promise((resolve) => setTimeout(resolve, 1500));
        continue;
      }
      throw new Error(
        `${method} ${path}: transport interrupted; no credentials logged`,
      );
    }
  }

  check(
    r.status() === expected,
    `${method} ${path} expected ${expected}, got ${r.status()}: ${(await r.text()).slice(0, 1500)}`,
  );
  return path.includes("/files/download")
    ? r.body()
    : r.headers()["content-type"]?.includes("json")
      ? r.json()
      : r.body();
}
async function pick(target, label, optionsOrValue, maybeValue) {
 const value = maybeValue === undefined ? optionsOrValue : maybeValue
 await target.getByLabel(label, {exact:true}).click()
 await target.locator('[role=option][data-value='+JSON.stringify(String(value))+']').click()
}
async function login(ctx, email, password) {
  await call(ctx, "/auth/sign-in/email", "POST", { email, password });
}
async function waitReady(team, ctx = admin) {
  for (let i = 0; i < 60; i++) {
    const r = await ctx.request.get(base + "/api/storage", {
      headers: { "X-Mist-Team": team },
    });
    if (r.ok()) return r.json();
    await new Promise((r) => setTimeout(r, 1000));
  }
  throw new Error("Team storage never ready " + team);
}
async function submit(ctx, team, name, data = {}) {
  const result = await call(
    ctx,
    "/api/jobs",
    "POST",
    {
      name,
      type: "command",
      accelerator: "cpu",
      command: ["python", "-c", "print('JOB_PASSED',flush=True)"],
      timeout_seconds: 180,
      ...data,
    },
    team,
    201,
  );
  report.jobs.push({ id: result.job_id, name, team });
  return result.job_id;
}
async function waitJob(ctx, team, id, want = "Success", seconds = 600) {
  for (let i = 0; i < seconds; i += 2) {
    const j = await call(ctx, "/api/jobs/" + id, "GET", undefined, team);
    if (j.job_state === want) return j;
    if (["Success", "Failure", "Cancelled"].includes(j.job_state)) {
      const logs = await call(
        ctx,
        "/api/jobs/" + id + "/logs",
        "GET",
        undefined,
        team,
      );
      throw new Error(
        `${id} ${j.job_state}: ${JSON.stringify(logs).slice(-3000)}`,
      );
    }
    await new Promise((r) => setTimeout(r, 2000));
  }
  throw new Error("Job timed out " + id);
}
async function terminalJob(ctx, team, id) {
  const j = await waitJob(ctx, team, id);
  const logs = await call(
    ctx,
    "/api/jobs/" + id + "/logs",
    "GET",
    undefined,
    team,
  );
  writeFileSync(out + "/" + id + ".log", logs.logs);
  return { j, logs: logs.logs };
}
async function upload(ctx, team, name, text, scope) {
  return call(
    ctx,
    `/api/datasets?filename=${encodeURIComponent(name)}${scope ? "&scope=" + scope : ""}`,
    "POST",
    Buffer.from(text),
    team,
    201,
  );
}
const kube = (...args) =>
  execFileSync("k3s", ["kubectl", ...args], {
    env: { ...process.env, KUBECONFIG: "/home/utmist/.kube/config" },
    encoding: "utf8",
  });
try {
  await login(admin, credentials.email, credentials.password);
  const account = (await call(admin, "/api/session")).user;
  state.adminID = account.id;
  const legacyBefore = await call(admin, "/api/jobs");
  report.legacy_job_count_before = legacyBefore.count;
  if (!state.a) {
    state.a = await call(
      admin,
      "/api/teams",
      "POST",
      { name: "Research pilot", storage_gib: 1 },
      "",
      201,
    );
    save();
  }
  if (!state.b) {
    state.b = await call(
      admin,
      "/api/teams",
      "POST",
      { name: "Verification isolation", storage_gib: 1 },
      "",
      201,
    );
    save();
  }
  const A = state.a.id,
    B = state.b.id;
  report.teams = [A, B];
  // Only unfinished fixtures created by this verifier are cancelled on rerun.
  for (const team of [A, B]) {
    const jobs = JSON.parse(
      kube("get", "jobs", "-n", "mist-" + team, "-o", "json"),
    ).items;
    for (const job of jobs) {
      if (
        job.metadata.annotations?.["mist.io/cancelled-at"] ||
        job.status?.conditions?.some(
          (c) => ["Complete", "Failed"].includes(c.type) && c.status === "True",
        )
      )
        continue;
      kube(
        "patch",
        "job",
        job.metadata.name,
        "-n",
        job.metadata.namespace,
        "--type=merge",
        "-p",
        JSON.stringify({
          spec: { suspend: true },
          metadata: {
            annotations: {
              "mist.io/cancelled-at": new Date().toISOString(),
              "mist.io/stop-reason": "Previous verification fixture cancelled",
            },
          },
        }),
      );
    }
  }

  await call(
    admin,
    `/api/teams/${A}`,
    "PATCH",
    { disabled: false, policy: { ...state.a.policy, concurrent: 1 } },
    "",
  );
  await call(admin, `/api/teams/${B}`, "PATCH", { disabled: false }, "");
  await call(admin, `/api/teams/${A}/members`, "POST", {
    user_id: account.id,
    common_writer: true,
  });
  for (const [key, name] of [
    ["alice", "Researcher A"],
    ["mate", "Teammate A"],
    ["bob", "Researcher B"],
  ]) {
    if (!state[key]) {
      const password = randomBytes(20).toString("base64url"),
        email = `${key}-${Date.now()}@mist.invalid`;
      const result = await call(admin, "/auth/admin/create-user", "POST", {
        name,
        email,
        password,
        role: "user",
      });
      state[key] = { id: result.user.id, name, email, password };
      save();
    }
    await call(admin, "/auth/admin/unban-user", "POST", {
      userId: state[key].id,
    });
    await call(admin, `/api/teams/${key === "bob" ? B : A}/members`, "POST", {
      user_id: state[key].id,
      common_writer: false,
    });
  }
  const alice = await browser.newContext({ baseURL: base }),
    mate = await browser.newContext({ baseURL: base }),
    bob = await browser.newContext({ baseURL: base });
  await Promise.all([
    login(alice, state.alice.email, state.alice.password),
    login(mate, state.mate.email, state.mate.password),
    login(bob, state.bob.email, state.bob.password),
  ]);
  await Promise.all([waitReady(A), waitReady(B, bob)]);
  passed("team_filesystems_ready");
  const capacity = await call(admin, "/api/storage", "GET", undefined, A);
  check(
    capacity.capacity_bytes < 1.1 * 1024 ** 3,
    "Team sees parent disk capacity",
  );
  report.storage = capacity;
  const ds = await upload(alice, A, "member.txt", "TEAM_A_MEMBER_DATA");
  const common = await upload(
    admin,
    A,
    "common.txt",
    "TEAM_A_COMMON_DATA",
    "common",
  );
  check(
    (await call(mate, "/api/datasets", "GET", undefined, A)).datasets.some(
      (d) => d.id === ds.id,
    ),
    "Teammate cannot read member data",
  );
  await call(mate, `/api/datasets/${ds.id}`, "DELETE", undefined, A, 403);
  await call(
    alice,
    "/api/datasets?filename=no.txt&scope=common",
    "POST",
    Buffer.from("bad"),
    A,
    403,
  );
  await call(bob, `/api/datasets/${ds.id}`, "GET", undefined, B, 404);
  await call(bob, "/api/datasets", "GET", undefined, A, 403);
  await call(bob, "/api/teams", "POST", { name: "unauthorized" }, "", 403);
  passed("team_read_write_and_cross_team_denial");
  await call(admin, `/api/teams/${A}/grants`, "POST", {
    target_team: B,
    scope: state.alice.id,
  });
  const grant = (await call(admin, "/api/teams")).teams
    .find((t) => t.id === A)
    .grants.find((g) => g.scope === state.alice.id && g.target_team === B);
  const shared = await call(
    bob,
    `/api/datasets/${ds.id}/files/download?path=member.txt`,
    "GET",
    undefined,
    B,
  );
  check(shared.toString() === "TEAM_A_MEMBER_DATA", "Shared download mismatch");
  await call(bob, `/api/datasets/${common.id}`, "GET", undefined, B, 404);
  await call(bob, `/api/datasets/${ds.id}`, "DELETE", undefined, B, 403);
  const sharedID = await submit(bob, B, "Read-only shared input", {
    dataset_id: ds.id,
    script:
      "from pathlib import Path\nassert Path('/inputs/member.txt').read_text()=='TEAM_A_MEMBER_DATA'\ntry:\n Path('/inputs/bad').write_text('bad')\n raise AssertionError('writable shared input')\nexcept OSError: pass\nPath('/outputs/shared.json').write_text('ok')\nprint('SHARED_INPUT_PASSED',flush=True)",
    command: undefined,
  });
  await terminalJob(bob, B, sharedID);
  const revokedID = await submit(bob, B, "Revoke shared input", {
    dataset_id: ds.id,
    command: ["python", "-c", "import time; time.sleep(180)"],
  });
  await waitJob(bob, B, revokedID, "InProgress", 90);
  await call(admin, `/api/teams/${A}/grants/${grant.id}`, "DELETE");
  await waitJob(bob, B, revokedID, "Cancelled", 60);
  await call(bob, `/api/datasets/${ds.id}/files`, "GET", undefined, B, 404);
  passed("scoped_readonly_sharing_and_running_job_revocation");
  // Durable FIFO queue survives an API/controller rollout, and teams each get a turn.
  const running = await submit(alice, A, "Queue holds capacity", {
    command: [
      "python",
      "-c",
      'import time; print("RUNNING",flush=True); time.sleep(180)',
    ],
  });
  await waitJob(alice, A, running, "InProgress", 90);
  const queued = await submit(alice, A, "Queue survives restart");
  const other = await submit(bob, B, "Other team gets a turn");
  await terminalJob(bob, B, other);
  check(
    (await call(alice, "/api/jobs/" + queued, "GET", undefined, A))
      .job_state === "Scheduled",
    "Team concurrency bypassed",
  );
  kube("rollout", "restart", "deployment/mist-api", "-n", "mist-system");
  kube(
    "rollout",
    "status",
    "deployment/mist-api",
    "-n",
    "mist-system",
    "--timeout=120s",
  );
  check(
    (await call(alice, "/api/jobs/" + queued, "GET", undefined, A))
      .job_state === "Scheduled",
    "Queue lost on restart",
  );
  await call(alice, `/api/jobs/${running}/cancel`, "POST", undefined, A);
  await terminalJob(alice, A, queued);
  passed("durable_fifo_team_concurrency_fair_turns_and_cancel");
  for (const body of [
    { cpu: "9" },
    { memory: "64Gi" },
    { device_count: 3, accelerator: "nvidia" },
    { timeout_seconds: 86401 },
    { image: "evil.invalid/app:v1" },
    { storage_scope: state.mate.id },
  ]) {
    await call(
      alice,
      "/api/jobs",
      "POST",
      { command: ["true"], ...body },
      A,
      body.storage_scope ? 403 : 400,
    );
  }
  await call(alice, "/api/jobs", "POST", { command: ["true"] }, "", 403);
  passed("policy_limits_and_legacy_bypass_denied");
  // Network probes run inside an ordinary job, under the actual k3s policy.
  const isolation = await submit(alice, A, "Workload network isolation", {
    script: `import socket,os\nfrom pathlib import Path\nassert not Path('/var/run/secrets/kubernetes.io/serviceaccount/token').exists()\nfor host,port in [('10.43.1.116',3000),('10.0.0.112',2049),('10.0.0.175',6443),('100.95.175.37',22)]:\n s=socket.socket();s.settimeout(2)\n try:\n  s.connect((host,port))\n  raise AssertionError('private network reachable '+host)\n except (TimeoutError,ConnectionRefusedError,OSError): pass\n finally: s.close()\nassert socket.gethostbyname('github.com')\nPath('/outputs/isolation.txt').write_text('private network and credentials denied')\nprint('ISOLATION_PASSED',flush=True)`,
    command: undefined,
  });
  check(
    (await terminalJob(alice, A, isolation)).logs.includes("ISOLATION_PASSED"),
    "Missing isolation marker",
  );
  passed("live_container_private_network_and_credentials_denied");
  // The full-volume writer cleans its own test file; it never touches real data.
  const quota = await submit(bob, B, "Filesystem capacity enforcement", {
    script: `import errno,json\nfrom pathlib import Path\np=Path('/outputs/quota-probe.bin');size=0\ntry:\n with p.open('wb',buffering=0) as f:\n  for i in range(2048):\n   f.write(b'x'*1048576);size+=1048576\n raise AssertionError('2GiB fit in a 1GiB team filesystem')\nexcept OSError as e:\n assert e.errno in (errno.ENOSPC,errno.EDQUOT),e\n print('STORAGE_QUOTA_PASSED bytes='+str(size),flush=True)\nfinally:\n p.unlink(missing_ok=True)\nPath('/outputs/quota.json').write_text(json.dumps({'bytes_before_limit':size}))`,
    command: undefined,
  });
  check(
    (await terminalJob(bob, B, quota)).logs.includes("STORAGE_QUOTA_PASSED"),
    "Quota not enforced",
  );
  passed("training_writes_obey_filesystem_capacity");
  const matrix = (n, m, p) =>
    Array.from({ length: n }, (_, i) =>
      Array.from({ length: m }, (_, j) => Math.sin((i + 1) * (j + 2) + p)),
    );
  const multiply = (x, w) =>
    x.map((row) =>
      w[0].map((_, k) => row.reduce((sum, v, j) => sum + v * w[j][k], 0)),
    );
  const x = matrix(128, 32, 0.1),
    vx = matrix(64, 32, 1.2),
    truth = matrix(32, 32, 2.3).map((row) => row.map((v) => v * 0.1));
  const regression = await upload(
    admin,
    A,
    "regression.json",
    JSON.stringify({
      x,
      y: multiply(x, truth),
      validation_x: vx,
      validation_y: multiply(vx, truth),
    }),
    "common",
  );
  state.dataset = regression;
  save();
  const training = readFileSync("deploy/k3s/examples/train_shared.py", "utf8");
  for (const backend of ["nvidia", "tenstorrent"]) {
    const script = training.replace(
      "args = parser.parse_args()",
      `args = parser.parse_args(['${backend}','--expected-devices','2','--output','/outputs'])`,
    );
    const id = await submit(admin, A, "Department training " + backend, {
      accelerator: backend,
      device_count: backend === "nvidia" ? 2 : 1,
      script,
      command: undefined,
      dataset_id: regression.id,
      storage_scope: "common",
      timeout_seconds: 600,
    });
    const { j, logs } = await terminalJob(admin, A, id);
    check(
      logs.includes("TRAINING_PASSED") &&
        logs.includes("DATASET_LOADED file=/inputs/regression.json"),
      "Training did not use uploaded data",
    );
    const files = (
      await call(admin, `/api/jobs/${id}/files`, "GET", undefined, A)
    ).files;
    check(files.length > 0, "No persisted model outputs");
    for (const f of files) {
      const data = await call(
        admin,
        `/api/jobs/${id}/files/download?path=${encodeURIComponent(f.path)}`,
        "GET",
        undefined,
        A,
      );
      check(data.length === f.size, "Downloaded model size mismatch");
      writeFileSync(out + `/${backend}-${f.path.replaceAll("/", "_")}`, data);
    }
    report.jobs.find((e) => e.id === id).node = j.node;
    passed(backend + "_real_training_uploaded_data_and_downloaded_weights");
  }
  // Deactivation is authoritative for jobs, rather than waiting for sessions to expire.
  const banJob = await submit(bob, B, "Account deactivation releases job", {
    command: ["python", "-c", "import time; time.sleep(180)"],
  });
  await waitJob(bob, B, banJob, "InProgress", 90);
  await call(admin, "/auth/admin/ban-user", "POST", {
    userId: state.bob.id,
    banReason: "Verification complete",
  });
  await call(bob, "/api/jobs", "GET", undefined, B, 401);
  await call(admin, `/api/teams/${B}/members`, "POST", {
    user_id: account.id,
    common_writer: true,
  });
  await waitJob(admin, B, banJob, "Cancelled", 60);
  passed("account_deactivation_stops_running_jobs");
  const page = await admin.newPage();
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto("/teams");
  await page.getByRole("heading", { name: "Teams", exact: true }).waitFor();
  await pick(page, "Workspace", { exact: true }, A);
  await page.screenshot({ path: out + "/teams-admin.png", fullPage: true });
  await page.goto("/jobs");
  await page
    .getByRole("heading", { name: "Job history", exact: true })
    .waitFor();
  await page
    .getByRole("button", { name: /Show details for Department training/ })
    .first()
    .click();
  await page
    .getByRole("button", { name: "Files", exact: true })
    .first()
    .click();
  await page.getByRole("heading", { name: "Saved files" }).waitFor();
  await page.screenshot({ path: out + "/jobs-expanded.png", fullPage: true });
  passed("browser_rows_expand_and_saved_files_open");
  await page
    .getByRole("button", { name: /Hide details for Department training/ })
    .first()
    .click();
  await page.screenshot({ path: out + "/jobs-rows.png", fullPage: true });
  await page.goto("/datasets");
  await page.getByRole("heading", { name: /Team folders/ }).waitFor();
  await page.screenshot({ path: out + "/team-folders.png", fullPage: true });
  passed("browser_admin_workspace_and_shared_folders");
  const before = state.a.policy.storage_gib;
  await call(
    admin,
    `/api/teams/${A}`,
    "PATCH",
    { policy: { ...state.a.policy, storage_gib: before + 1 } },
    "",
  );
  state.a.policy.storage_gib = before + 1;
  save();
  const grown = await waitReady(A);
  check(
    grown.capacity_bytes > capacity.capacity_bytes,
    "Storage growth did not resize filesystem",
  );
  passed("online_team_storage_growth");
  await call(
    admin,
    `/api/teams/${A}`,
    "PATCH",
    { policy: { ...state.a.policy, storage_gib: 1 } },
    "",
    400,
  );
  await call(admin, `/api/teams/${B}`, "PATCH", { disabled: true });
  await call(admin, "/auth/admin/ban-user", "POST", {
    userId: state.alice.id,
    banReason: "Verification complete",
  });
  await call(admin, "/auth/admin/ban-user", "POST", {
    userId: state.mate.id,
    banReason: "Verification complete",
  });
  for (const member of [state.alice, state.mate])
    await call(admin, `/api/teams/${A}/members/${member.id}`, "DELETE");
  const legacyAfter = await call(admin, "/api/jobs");
  check(legacyAfter.count === legacyBefore.count, "Legacy job history changed");
  passed("legacy_history_preserved");
  check(errors.length === 0, errors.join("; "));
  report.browser_errors = errors;
  report.finished = new Date().toISOString();
  writeFileSync(out + "/results.json", JSON.stringify(report, null, 2));
  console.log(JSON.stringify(report, null, 2));
} catch (e) {
  report.error = e.message;
  writeFileSync(out + "/results.json", JSON.stringify(report, null, 2));
  throw e;
} finally {
  await browser.close();
}

