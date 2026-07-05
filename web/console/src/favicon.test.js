import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

describe("XDP Console favicon", () => {
  it("registers a branded favicon instead of relying on the browser default globe", () => {
    const html = readFileSync("index.html", "utf8");
    expect(html).toContain('<link rel="icon" type="image/svg+xml" href="/favicon.svg" />');
    expect(html).toContain('<meta name="theme-color" content="#13bfb4" />');

    const svg = readFileSync("public/favicon.svg", "utf8");
    expect(svg).toContain("<title>XDP Console</title>");
    expect(svg).toContain('viewBox="0 0 64 64"');
    expect(svg).toContain("#18212a");
    expect(svg).toContain("#13bfb4");
  });
});
