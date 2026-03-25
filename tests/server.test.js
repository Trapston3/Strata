import { strict as assert } from "node:assert";
import request from "supertest";
import { app, sanitizePath, server } from "../src/server.js";

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

describe("GET /health", () => {
  after(() => {
    if (server) {
      server.close();
    }
  });

  it("should return status 200 and a runtime snapshot", async () => {
    const res = await request(app).get("/health");
    assert.equal(res.status, 200);
    assert.ok(res.body.uptime !== undefined, "Expected response body to contain uptime");
    assert.ok(res.body.memory !== undefined, "Expected response body to contain memory");
    assert.ok(res.body.processID !== undefined, "Expected response body to contain processID");
  });
});
