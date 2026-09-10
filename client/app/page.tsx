"use client";

import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { useAuth } from "@/contexts/auth-context";
import { SignIn } from "./components/sign-in";
import { Skeleton } from "./components/skeleton";

// The landing page: the sign-in for a visitor, a redirect to the profile for
// a signed-in user.
export default function Home() {
  const { state } = useAuth();
  const router = useRouter();

  useEffect(() => {
    if (state.status === "authenticated") {
      router.replace("/profile");
    }
  }, [state.status, router]);

  if (state.status !== "unauthenticated") {
    return <Skeleton />;
  }
  return (
    <section data-testid="landing-page" className="flex flex-col gap-4">
      <h1 className="animate-fade-in text-2xl font-semibold tracking-tight">
        Stonks
      </h1>
      <p className="animate-fade-in stagger-1 max-w-prose text-text-muted">
        Portfolio tracking: the holdings in each portfolio, and their valuation
        from the prices of the instruments held.
      </p>
      <div className="animate-fade-in stagger-2">
        <SignIn />
      </div>
    </section>
  );
}
