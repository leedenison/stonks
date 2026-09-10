"use client";

import { GoogleOAuthProvider } from "@react-oauth/google";
import { QueryClientProvider } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";
import { AuthProvider, expireSession } from "@/contexts/auth-context";
import { newQueryClient } from "@/lib/query-client";
import { newTransport } from "@/lib/transport";

// Read as a literal so the build inlines it.
const googleClientId = process.env.NEXT_PUBLIC_GOOGLE_CLIENT_ID ?? "";

export function Providers({ children }: { children: ReactNode }) {
  // One client and one transport per mount. A module-level client would be
  // shared by every request rendered on the server.
  const [client] = useState(newQueryClient);
  const [transport] = useState(() => newTransport(() => expireSession(client)));
  return (
    <QueryClientProvider client={client}>
      <GoogleOAuthProvider clientId={googleClientId}>
        <AuthProvider transport={transport}>{children}</AuthProvider>
      </GoogleOAuthProvider>
    </QueryClientProvider>
  );
}
