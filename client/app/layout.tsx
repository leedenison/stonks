// The root layout is the only server component. It loads the faces, sets
// their variables on <html> and mounts the providers and the shell.
//
// The conventions the whole client follows:
//
// Money and quantities are exact decimals through big.js, never JavaScript
// number arithmetic. The package is installed by the first code that handles
// an amount.
//
// Nothing fetches in an effect. Server state goes through the query client,
// lib/query-client.ts, and a query that needs a session through
// hooks/use-authed-query.ts; keys come from lib/query-keys.ts.
//
// A route segment that requires a session is guarded by its own layout, as
// app/profile/layout.tsx does.
//
// data-testid goes on page containers, tables, rows, buttons, modals and form
// inputs, named for what the element is rather than where it sits. The e2e
// suite selects on these and nothing else.
import type { Metadata } from "next";
import { Inter, JetBrains_Mono, Space_Grotesk } from "next/font/google";
import type { ReactNode } from "react";
import "./globals.css";
import { AppHeader } from "./components/app-header";
import { Providers } from "./providers";

const display = Space_Grotesk({
  subsets: ["latin"],
  variable: "--face-display",
  display: "swap",
});
const body = Inter({
  subsets: ["latin"],
  variable: "--face-body",
  display: "swap",
});
const mono = JetBrains_Mono({
  subsets: ["latin"],
  variable: "--face-mono",
  display: "swap",
});

export const metadata: Metadata = {
  title: { default: "Stonks", template: "%s | Stonks" },
  description: "Portfolio tracking",
};

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html
      lang="en"
      className={`${display.variable} ${body.variable} ${mono.variable}`}
    >
      <body className="min-h-dvh bg-background font-sans text-text-primary antialiased">
        <Providers>
          <AppHeader />
          <main className="mx-auto max-w-6xl px-4 py-6">{children}</main>
        </Providers>
      </body>
    </html>
  );
}
