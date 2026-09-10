"use client";

import { useRouter } from "next/navigation";
import { type ReactNode, useEffect } from "react";
import { Skeleton } from "@/app/components/skeleton";
import { useAuth } from "@/contexts/auth-context";

// The guard for the profile segment. Route guarding is done here rather than
// in middleware, because only the browser holds the restored session.
export default function ProfileLayout({ children }: { children: ReactNode }) {
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
