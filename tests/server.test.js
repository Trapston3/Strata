import { strict as assert } from "node:assert";
import { sanitizePath } from "../src/server.js";

describe("sanitizePath", () => {
  it("should return '/' for empty strings, null, or undefined", () => {
    assert.equal(sanitizePath(""), "/");
    assert.equal(sanitizePath(null), "/");
    assert.equal(sanitizePath(undefined), "/");
  });

  it("should return the original path if there is no query string", () => {
    assert.equal(sanitizePath("/api/logs"), "/api/logs");
    assert.equal(sanitizePath("/health"), "/health");
    assert.equal(sanitizePath("/"), "/");
  });

  it("should strip query strings and return the path", () => {
    assert.equal(sanitizePath("/api/logs?foo=bar"), "/api/logs");
    assert.equal(sanitizePath("/health?test=1&debug=true"), "/health");
    assert.equal(sanitizePath("/?search=query"), "/");
  });

  it("should handle paths without leading slashes (though Express usually provides them)", () => {
    assert.equal(sanitizePath("api/logs?foo=bar"), "api/logs");
    assert.equal(sanitizePath("health"), "health");
  });
});

describe("DELETE /api/logs", () => {
  let server;
  let baseUrl;

  before((done) => {
    import("../src/server.js").then(({ app }) => {
      server = app.listen(0, "127.0.0.1", () => {
        baseUrl = `http://127.0.0.1:${server.address().port}`;
        done();
      });
    }).catch(done);
  });

  after((done) => {
    if (server) {
      server.closeAllConnections();
      server.close(done);
    } else {
      done();
    }
  });

  it("should clear the log buffer and return 204", async () => {
    // 1. Populate the log buffer by making a dummy request
    const resHealth = await fetch(`${baseUrl}/health`);
    await resHealth.text(); // consume body to ensure request completes

    // 2. Verify logs are present
    const resBefore = await fetch(`${baseUrl}/api/logs`);
    const logsBefore = await resBefore.json();
    assert.ok(logsBefore.length > 0, "Log buffer should not be empty before deletion");

    // 3. Make the DELETE request
    const resDelete = await fetch(`${baseUrl}/api/logs`, { method: "DELETE" });
    await resDelete.text(); // consume body
    assert.equal(resDelete.status, 204, "DELETE request should return 204 No Content");

    // 4. Verify logs are cleared (only the DELETE request and subsequent GET might be present)
    const resAfter = await fetch(`${baseUrl}/api/logs`);
    const logsAfter = await resAfter.json();

    // The previous GET /health should be gone.
    const healthLogs = logsAfter.filter(l => l.path === "/health");
    assert.equal(healthLogs.length, 0, "Previous logs should be cleared");

    // The logBuffer may have the DELETE request itself, and the new GET request, but it will be much smaller
    assert.ok(logsAfter.length < logsBefore.length || logsBefore.length <= 2, "Log buffer size should be reset");
  });
});
