"use client";

import { useRouter } from "next/navigation";
import { type ReactNode, useEffect } from "react";
import { useAuth } from "@/contexts/auth-context";
import { Skeleton } from "./skeleton";

// SessionGuard renders its children only with a session, and sends a visitor
// to the landing page. Guarding is done here rather than in middleware,
// because only the browser holds the restored session.
export function SessionGuard({ children }: { children: ReactNode }) {
  const { state } = useAuth();
  const router = useRouter();

  useEffect(() => {
    if (state.status === "unauthenticated") {
      router.replace("/");
    }
  }, [state.status, router]);

  switch (state.status) {
    case "restoring":
      return <Skeleton />;
    case "unauthenticated":
      return null;
    case "authenticated":
      return <>{children}</>;
  }
}
