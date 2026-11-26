import { afterEach, describe, expect, it, vi } from "vitest";
import { execFileSync, spawnSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

vi.setConfig({ testTimeout: 60000 });

const repoRoot = dirname(dirname(dirname(fileURLToPath(import.meta.url))));
const script = join(repoRoot, "scripts", "sync-upstream.sh").replaceAll("\\", "/");
const bashPath = process.platform === "win32" ? "C:\\Program Files\\Git\\bin\\bash.exe" : "bash";
const CARDS = "src/app/(dashboard)/dashboard/usage/components/OverviewCards.js";
const CSS = "src/app/globals.css";

const gitEnv = {
  ...process.env,
  GIT_AUTHOR_NAME: "t",
  GIT_AUTHOR_EMAIL: "t@t",
  GIT_COMMITTER_NAME: "t",
  GIT_COMMITTER_EMAIL: "t@t",
  GIT_CONFIG_COUNT: "1",
  GIT_CONFIG_KEY_0: "core.autocrlf",
  GIT_CONFIG_VALUE_0: "false",
};

const donateRule = 'button[aria-label="Donate"],\nbutton.border-pink-500\\/30,\n.border-pink-500\\/30 {\n  display: none !important;\n}\n';
const lines = (count, prefix) => Array.from({ length: count }, (_, i) => `${prefix} ${i + 1}`).join("\n") + "\n";
const pkg = (version) => JSON.stringify({ name: "x", version }, null, 2) + "\n";

const baseFiles = {
  "package.json": pkg("1.0.0"),
  "CHANGELOG.md": "# v1.0.0\n- base\n",
  [CSS]: lines(10, "css") + donateRule + lines(10, "tail"),
  [CARDS]: "// idr cost detail\n" + lines(20, "card"),
  "src/shared/components/Header.js": lines(3, "header"),
  "src/other.js": lines(5, "other"),
};

const dirs = [];

function git(cwd, ...args) {
  return execFileSync("git", args, { cwd, env: gitEnv, encoding: "utf8" });
}

function write(dir, files) {
  for (const [path, content] of Object.entries(files)) {
    const full = join(dir, path);
    if (content === null) {
      rmSync(full, { force: true });
      continue;
    }
    mkdirSync(dirname(full), { recursive: true });
    writeFileSync(full, content);
  }
}

function commitIfChanged(dir, message) {
  git(dir, "add", "-A");
  if (git(dir, "status", "--porcelain").trim()) git(dir, "commit", "-q", "-m", message);
}

function makeRepo({ upstream = {}, fork = {} }) {
  const dir = mkdtempSync(join(tmpdir(), "sync-upstream-"));
  dirs.push(dir);
  git(dir, "init", "-q", "-b", "master");
  write(dir, baseFiles);
  commitIfChanged(dir, "base");
  git(dir, "branch", "up");
  git(dir, "checkout", "-q", "up");
  write(dir, upstream);
  commitIfChanged(dir, "upstream");
  git(dir, "checkout", "-q", "master");
  write(dir, fork);
  commitIfChanged(dir, "fork");
  return dir;
}

function runSync(dir) {
  const outputFile = join(dir, ".git", "sync-output").replaceAll("\\", "/");
  writeFileSync(outputFile, "");
  const result = spawnSync(bashPath, [script], {
    cwd: dir,
    encoding: "utf8",
    env: { ...gitEnv, UPSTREAM_REF: "up", GITHUB_OUTPUT: outputFile },
  });
  const outputs = Object.fromEntries(
    readFileSync(outputFile, "utf8")
      .split("\n")
      .filter(Boolean)
      .map((line) => [line.slice(0, line.indexOf("=")), line.slice(line.indexOf("=") + 1)])
  );
  return { status: result.status, log: `${result.stdout}${result.stderr}`, outputs };
}

function expectUntouched(dir, headBefore) {
  expect(git(dir, "rev-parse", "HEAD").trim()).toBe(headBefore);
  expect(git(dir, "status", "--porcelain").trim()).toBe("");
  expect(existsSync(join(dir, ".git", "MERGE_HEAD"))).toBe(false);
}

afterEach(() => {
  while (dirs.length) rmSync(dirs.pop(), { recursive: true, force: true });
});

describe("upstream sync", () => {
  it("does nothing when upstream has no new commits", () => {
    const dir = makeRepo({ fork: { "src/other.js": lines(5, "fork") } });
    const head = git(dir, "rev-parse", "HEAD").trim();
    const result = runSync(dir);
    expect(result.status).toBe(0);
    expect(result.outputs).toMatchObject({ status: "up-to-date", version: "1.0.0", version_changed: "false" });
    expectUntouched(dir, head);
  });

  it("merges cleanly, passes the multi-line donate guard and reports the version bump", () => {
    const dir = makeRepo({
      upstream: { "package.json": pkg("1.1.0"), "src/other.js": lines(5, "up") },
      fork: { "src/shared/components/Header.js": lines(3, "fork-header") },
    });
    const result = runSync(dir);
    expect(result.status).toBe(0);
    expect(result.outputs).toMatchObject({ status: "merged", version: "1.1.0", version_changed: "true" });
    expect(git(dir, "rev-list", "--parents", "-1", "HEAD").trim().split(" ")).toHaveLength(3);
    expect(git(dir, "log", "-1", "--format=%s")).toMatch(/^chore: merge upstream decolua\/9router \(/);
  });

  it("does not report a version change when only code moved", () => {
    const dir = makeRepo({ upstream: { "src/other.js": lines(5, "up") } });
    const result = runSync(dir);
    expect(result.status).toBe(0);
    expect(result.outputs).toMatchObject({ status: "merged", version_changed: "false" });
  });

  it("takes the upstream CHANGELOG when both sides edited it", () => {
    const dir = makeRepo({
      upstream: { "CHANGELOG.md": "# v1.1.0\n- upstream\n" },
      fork: { "CHANGELOG.md": "# Unreleased\n- fork\n" },
    });
    const result = runSync(dir);
    expect(result.status).toBe(0);
    expect(readFileSync(join(dir, "CHANGELOG.md"), "utf8")).toBe("# v1.1.0\n- upstream\n");
    expect(git(dir, "status", "--porcelain").trim()).toBe("");
  });

  it("keeps fork lines on a conflicting UI hunk and still takes the other upstream hunks", () => {
    const base = baseFiles[CSS].split("\n");
    const forkCss = [...base];
    forkCss[2] = "css 3 FORK";
    const upstreamCss = [...base];
    upstreamCss[2] = "css 3 UPSTREAM";
    upstreamCss[base.length - 3] = "tail 9 UPSTREAM";
    const dir = makeRepo({
      upstream: { [CSS]: upstreamCss.join("\n") },
      fork: { [CSS]: forkCss.join("\n") },
    });
    const result = runSync(dir);
    expect(result.status).toBe(0);
    const merged = readFileSync(join(dir, CSS), "utf8");
    expect(merged).toContain("css 3 FORK");
    expect(merged).not.toContain("css 3 UPSTREAM");
    expect(merged).toContain("tail 9 UPSTREAM");
    expect(merged).not.toMatch(/^(<<<<<<<|=======|>>>>>>>)/m);
  });

  it("fails and leaves the branch untouched on a conflict it cannot resolve", () => {
    const dir = makeRepo({
      upstream: { "src/other.js": lines(5, "up") },
      fork: { "src/other.js": lines(5, "fork") },
    });
    const head = git(dir, "rev-parse", "HEAD").trim();
    const result = runSync(dir);
    expect(result.status).not.toBe(0);
    expect(result.outputs.reason).toContain("src/other.js");
    expectUntouched(dir, head);
  });

  it("fails and leaves the branch untouched when upstream removes the donate-hide rule", () => {
    const dir = makeRepo({ upstream: { [CSS]: lines(10, "css") + lines(10, "tail") } });
    const head = git(dir, "rev-parse", "HEAD").trim();
    const result = runSync(dir);
    expect(result.status).not.toBe(0);
    expect(result.outputs.reason).toContain("donate-hide rule missing");
    expectUntouched(dir, head);
  });

  it("fails when upstream brings the EST. COST disclaimer back", () => {
    const dir = makeRepo({ upstream: { [CARDS]: baseFiles[CARDS] + "Estimated, not actual billing\n" } });
    const head = git(dir, "rev-parse", "HEAD").trim();
    const result = runSync(dir);
    expect(result.status).not.toBe(0);
    expect(result.outputs.reason).toContain("EST. COST disclaimer");
    expectUntouched(dir, head);
  });
});
