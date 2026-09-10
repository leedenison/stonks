import { test as base, expect } from "@playwright/test";

// Every spec imports test from here. The context marks each document with
// data-testmode before any of its scripts run, so the client zeroes every
// animation and transition and no assertion races one. It also answers
// Google's script with an empty one, so nothing in the suite reaches Google
// and the sign-in renders its wrapper without a button.
export const test = base.extend({
  context: async ({ context }, use) => {
    await context.addInitScript(() => {
      const mark = () =>
        document.documentElement.setAttribute("data-testmode", "");
      if (document.documentElement) {
        mark();
      } else {
        // The script can run before the parser has created <html>.
        new MutationObserver((_, observer) => {
          if (document.documentElement) {
            mark();
            observer.disconnect();
          }
        }).observe(document, { childList: true });
      }
    });
    await context.route("https://accounts.google.com/**", (route) =>
      route.fulfill({
        status: 200,
        contentType: "application/javascript",
        body: "",
      }),
    );
    await use(context);
  },
});

export { expect };
