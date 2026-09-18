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
// A route segment that requires a session is guarded by its layout through
// components/session-guard.tsx, as app/(app)/layout.tsx does. A page outside
// such a segment renders its own <main>.
//
// A preference kept in the browser, such as the scheme, goes through
// hooks/use-stored-value.ts, so the server render and the first client
// render agree.
//
// No component carries a raw colour. The tokens in globals.css are the whole
// palette, and a colour that is missing is added there.
//
// data-testid goes on page containers, tables, rows, buttons, modals and form
// inputs, named for what the element is rather than where it sits. The e2e
// suite selects on these and nothing else.
import type { Metadata } from "next";
import { Archivo, JetBrains_Mono, Sora } from "next/font/google";
import type { ReactNode } from "react";
import "./globals.css";
import { schemeScript } from "@/lib/scheme";
import { TopBar } from "./components/top-bar";
import { Providers } from "./providers";

const display = Archivo({
  subsets: ["latin"],
  variable: "--face-display",
  display: "swap",
});
const body = Sora({
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
      suppressHydrationWarning
    >
      <body className="min-h-dvh bg-background font-sans text-text-primary antialiased">
        <script dangerouslySetInnerHTML={{ __html: schemeScript }} />
        <Providers>
          <TopBar />
          {children}
        </Providers>
      </body>
    </html>
  );
}
