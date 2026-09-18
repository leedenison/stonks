// The colour scheme a user chose: an override of light or dark, or system,
// which leaves the choice to prefers-color-scheme. The override is the
// data-theme attribute on <html>; system is its absence.

export type Scheme = "light" | "dark" | "system";

export const schemeKey = "stonks.scheme";

export function parseScheme(raw: string | null): Scheme {
  return raw === "light" || raw === "dark" ? raw : "system";
}

export function applyScheme(root: HTMLElement, scheme: Scheme) {
  if (scheme === "system") {
    root.removeAttribute("data-theme");
  } else {
    root.setAttribute("data-theme", scheme);
  }
}

// schemeScript runs in <head> before first paint, so a stored override is on
// <html> before any style is computed and nothing flashes the other scheme.
export const schemeScript = `(function(){try{var s=localStorage.getItem(${JSON.stringify(schemeKey)});if(s==="light"||s==="dark"){document.documentElement.setAttribute("data-theme",s)}}catch(e){}})()`;
