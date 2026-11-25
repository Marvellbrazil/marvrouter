import { describe, expect, it } from "vitest";
import { spawnSync } from "node:child_process";
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = dirname(dirname(dirname(fileURLToPath(import.meta.url))));
const updaterPath = join(repoRoot, "scripts", "update-marvrouter.sh");
const updater = readFileSync(updaterPath, "utf8").replace(/\nmain "\$@"\s*$/, "\n");

function runLatestTag({ apiStatus, feedStatus, feedBody }) {
  const dir = mkdtempSync(join(tmpdir(), "marvrouter-updater-"));
  const fakeBin = join(dir, "bin");
  mkdirSync(fakeBin);
  writeFileSync(join(dir, "api.json"), '[{"tag_name":"fork-v0.5.120","draft":false}]');
  writeFileSync(join(dir, "feed.xml"), feedBody ?? "");
  writeFileSync(join(fakeBin, "curl"), `#!/bin/sh
case "$*" in
  *api.github.com*) [ "${apiStatus}" = 200 ] && cat "$TEST_API_JSON" && exit 0; exit 22 ;;
  *releases.atom*) [ "${feedStatus}" = 200 ] && cat "$TEST_FEED_XML" && exit 0; exit 22 ;;
  *) exit 22 ;;
esac
`);
  const shellUpdater = join(dir, "updater.sh");
  writeFileSync(shellUpdater, updater);
  try {
    const result = spawnSync(bashPath, ["-c", `curl() { "$FAKE_CURL" "$@"; }; source "$1"; latest_tag`, "test", shellUpdater], {
      encoding: "utf8",
      env: { ...process.env, FAKE_CURL: join(fakeBin, "curl"), TEST_API_JSON: join(dir, "api.json"), TEST_FEED_XML: join(dir, "feed.xml") },
    });
    return { ...result, output: `${result.stdout ?? ""}${result.stderr ?? ""}` };
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

const bashPath = process.platform === "win32" ? "C:\\Program Files\\Git\\bin\\bash.exe" : "bash";
const atomFeed = '<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"><entry><link href="https://github.com/Marvellbrazil/marvrouter/releases/tag/fork-v0.5.120" /></entry></feed>';

describe("server updater release discovery", () => {
  it("falls back to the release feed when the GitHub API returns 403", () => {
    const result = runLatestTag({ apiStatus: 403, feedStatus: 200, feedBody: atomFeed });
    expect(result.status).toBe(0);
    expect(result.stdout.trim()).toBe("[update-marvrouter] GitHub Releases API unavailable; trying releases feed.\nfork-v0.5.120");
    expect(result.stderr).toBe("");
  });

  it("reports a clean failure when the API and release feed are unavailable", () => {
    const result = runLatestTag({ apiStatus: 403, feedStatus: 403 });
    expect(result.status).not.toBe(0);
    expect(result.output).toContain("unable to retrieve GitHub releases from API or feed.");
    expect(result.output).not.toContain("Traceback");
    expect(result.output).not.toContain("JSONDecodeError");
  });
});
