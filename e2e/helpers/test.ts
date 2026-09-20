import { test as base, expect } from "@playwright/test";
import { closeRedis, deleteSession, injectSession, seedSession } from "./auth";
import {
  closeDB,
  deleteUser,
  type Role,
  type SeededUser,
  seedUser,
} from "./db";

// Seeded is a user seeded for one test, with the session started for them.
export type Seeded = { user: SeededUser; session: string };

type Fixtures = {
  // seed invents a user of role with a live session. What a test seeds is
  // removed once it ends, the session with the user's rows.
  seed: (role?: Role) => Promise<Seeded>;
  // signIn seeds a user of role and puts their session in the browser
  // context, so the page loads signed in.
  signIn: (role?: Role) => Promise<Seeded>;
};

type WorkerFixtures = {
  // pools closes the database and Redis connections with the worker.
  pools: void;
};

// Every spec imports test from here. The context marks each document with
// data-testmode before any of its scripts run, so the client zeroes every
// animation and transition and no assertion races one. It also answers
// Google's script with an empty one, so nothing in the suite reaches Google
// and the sign-in renders its wrapper without a button.
export const test = base.extend<Fixtures, WorkerFixtures>({
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
  // eslint-disable-next-line no-empty-pattern -- a fixture names what it depends on by destructuring; this one depends on nothing
  seed: async ({}, use) => {
    const users: string[] = [];
    const sessions: string[] = [];
    await use(async (role: Role = "user") => {
      const user = await seedUser(role);
      users.push(user.id);
      const session = await seedSession(user);
      sessions.push(session);
      return { user, session };
    });
    await Promise.all(sessions.map(deleteSession));
    await Promise.all(users.map(deleteUser));
  },
  signIn: async ({ context, seed }, use) => {
    await use(async (role?: Role) => {
      const seeded = await seed(role);
      await injectSession(context, seeded.session);
      return seeded;
    });
  },
  pools: [
    // eslint-disable-next-line no-empty-pattern -- a fixture names what it depends on by destructuring; this one depends on nothing
    async ({}, use) => {
      await use();
      await closeDB();
      await closeRedis();
    },
    { scope: "worker", auto: true },
  ],
});

export { expect };
