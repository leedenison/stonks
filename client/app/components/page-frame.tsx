import type { ReactNode } from "react";
import { BackButton } from "./back-button";

// Page is the frame of every page: an action bar with the title and the
// page's actions, then the body.
export function Page({
  title,
  back,
  actions,
  width = "prose",
  testId,
  children,
}: {
  title: string;
  // back is the page this one belongs under, as the statements page is for
  // a statement.
  back?: string;
  actions?: ReactNode;
  // Prose suits a form or a record. Wide suits a table and gives its
  // columns the room they need.
  width?: "prose" | "wide";
  testId?: string;
  children?: ReactNode;
}) {
  return (
    <section data-testid={testId} className="flex min-h-full flex-col">
      <header
        data-testid="page-bar"
        className="flex flex-wrap items-center gap-x-6 gap-y-2 border-b border-border bg-surface px-6 py-2.5"
      >
        <div className="flex items-center gap-2">
          {back && <BackButton to={back} />}
          <h1
            data-testid="page-title"
            className="text-xl font-semibold tracking-tight"
          >
            {title}
          </h1>
        </div>
        {actions && (
          <div className="flex flex-wrap items-center gap-1">{actions}</div>
        )}
      </header>
      <div
        className={`flex flex-col gap-4 px-6 py-6 ${width === "wide" ? "max-w-[100rem]" : "max-w-4xl"}`}
      >
        {children}
      </div>
    </section>
  );
}
