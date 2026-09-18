import type { ReactNode } from "react";

// Page is the column a page renders in. Prose width suits a form or a
// record; wide suits a table, which is given the room its columns need.
export function Page({
  title,
  width = "prose",
  testId,
  children,
}: {
  title: string;
  width?: "prose" | "wide";
  testId?: string;
  children?: ReactNode;
}) {
  return (
    <section
      data-testid={testId}
      className={`flex flex-col gap-4 ${width === "wide" ? "max-w-[100rem]" : "max-w-4xl"}`}
    >
      <h1
        data-testid="page-title"
        className="text-2xl font-semibold tracking-tight"
      >
        {title}
      </h1>
      {children}
    </section>
  );
}
