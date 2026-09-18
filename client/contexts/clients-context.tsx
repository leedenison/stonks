"use client";

import type { Transport } from "@connectrpc/connect";
import { createContext, type ReactNode, useContext, useMemo } from "react";
import { type Clients, newClients } from "@/lib/clients";

const ClientsContext = createContext<Clients | null>(null);

// ClientsProvider holds the typed clients over the one transport, so a hook
// that calls the API reaches a client without building its own.
export function ClientsProvider({
  transport,
  children,
}: {
  transport: Transport;
  children: ReactNode;
}) {
  const clients = useMemo(() => newClients(transport), [transport]);
  return (
    <ClientsContext.Provider value={clients}>
      {children}
    </ClientsContext.Provider>
  );
}

export function useClients(): Clients {
  const ctx = useContext(ClientsContext);
  if (!ctx) {
    throw new Error("useClients called outside ClientsProvider");
  }
  return ctx;
}
