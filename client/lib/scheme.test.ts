import { beforeEach, describe, expect, it } from "vitest";
import { applyScheme, parseScheme, schemeKey, schemeScript } from "./scheme";

describe("scheme", () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.removeAttribute("data-theme");
  });

  it("parses an override and falls back to system", () => {
    expect(parseScheme("dark")).toBe("dark");
    expect(parseScheme("light")).toBe("light");
    expect(parseScheme(null)).toBe("system");
    expect(parseScheme("purple")).toBe("system");
  });

  it("applies an override as the attribute and system as its absence", () => {
    const root = document.documentElement;
    applyScheme(root, "dark");
    expect(root.getAttribute("data-theme")).toBe("dark");
    applyScheme(root, "system");
    expect(root.hasAttribute("data-theme")).toBe(false);
  });

  it("has a script that applies the stored override", () => {
    localStorage.setItem(schemeKey, "dark");
    new Function(schemeScript)();
    expect(document.documentElement.getAttribute("data-theme")).toBe("dark");
  });

  it("has a script that leaves system alone", () => {
    new Function(schemeScript)();
    expect(document.documentElement.hasAttribute("data-theme")).toBe(false);
  });
});
