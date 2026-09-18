import type { ReactNode } from "react";
import { BackButton } from "./back-button";

// Page is the column a page renders in: an action bar across the top with
// the title on the left and the page's actions beside it, a rule below, and
// the body. A page that belongs under another, as a statement's does under
// the statements, names that page as back, and an arrow to it sits left of
// the title. Prose width suits a form or a record; wide suits a table,
// which is given the room its columns need.
export function Page({
  title,
  back,
  actions,
  width = "prose",
  testId,
  children,
}: {
  title: string;
  back?: string;
  actions?: ReactNode;
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
