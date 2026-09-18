---
name: frontend-design
description: Visual design conventions for the Stonks front end -- the token system, the scheme, the faces, the shell and page patterns, the shared components and the states they present, motion, and the test ids the e2e suite depends on. Use when building or changing any UI.
---

# Frontend Design

Stonks is a tool people look at repeatedly to read numbers. The goal is legibility and
calm: dense tabular data that is easy to scan, a restrained palette, and motion that
orients rather than decorates.

Avoid the generic AI aesthetic -- the purple-to-blue gradient hero, emoji section headings,
a card with a coloured left border for no reason. Every visual decision should be
answerable with what it does for the reader.

## Tokens

The palette is declared once in `client/app/globals.css`, and no component carries a raw
colour. A colour that is missing is added there as a token.

Two hues with distinct jobs. Primary is structural: the top bar, the active navigation
item, table header tints and focus rings. Accent marks action: the primary button, the
active marker in the navigation and the rule under the top bar. Positive and negative are
reserved for gains and losses and for run outcomes.

| Token          | Light            | Dark             | Use                                        |
| -------------- | ---------------- | ---------------- | ------------------------------------------ |
| background     | rgb(244 246 249) | rgb(13 17 23)    | the page                                   |
| surface        | rgb(255 255 255) | rgb(22 28 39)    | cards, bars, dialogs                       |
| surface-tint   | 4% primary-dark over surface | the same | table header ground |
| border         | rgb(214 222 231) | rgb(37 47 62)    |                                            |
| text-primary   | rgb(15 23 32)    | rgb(226 232 240) |                                            |
| text-muted     | rgb(90 107 122)  | rgb(122 143 168) | labels, secondary text                     |
| primary        | rgb(62 107 138)  | rgb(85 153 204)  | focus rings, running chip                  |
| primary-dark   | rgb(27 58 75)    | rgb(26 48 72)    | the top bar, active navigation text        |
| primary-light  | rgb(191 211 225) | rgb(30 61 90)    | hover tints, at 15%                        |
| accent         | rgb(224 122 47)  | rgb(224 122 47)  | the active marker, the rule, rejections    |
| accent-dark    | rgb(184 94 29)   | rgb(184 94 29)   | the primary button ground                  |
| accent-soft    | rgb(247 216 193) | rgb(45 24 5)     | the notice tint, at 50%                    |
| on-accent-soft | rgb(133 66 16)   | rgb(224 122 47)  | text on the notice tint                    |
| action         | rgb(160 80 24)   | rgb(224 122 47)  | an action written as text on a surface     |
| on-dark        | rgb(255 255 255) | rgb(255 255 255) | text on primary-dark and accent-dark       |
| positive       | rgb(21 128 61)   | rgb(74 222 128)  |                                            |
| negative       | rgb(185 28 28)   | rgb(248 113 113) |                                            |

Every text and ground pair meets AA at 4.5:1. White on accent is 3:1, so white text sits
on accent-dark only.

## Scheme

The scheme follows the system by default. A `data-theme` of `light` or `dark` on `<html>`
overrides it, chosen from the profile menu and kept in localStorage through
`client/hooks/use-scheme.ts`. An inline script in the root layout applies the stored
choice before first paint. The `dark` variant matches both routes.

## Faces

Archivo for headings, through `font-display`, which `h1` to `h3` take by default. Sora for
body text. JetBrains Mono with tabular figures for every number, date and identifier:
`font-mono tabular-nums`, right aligned in a table.

## Shell

- `components/top-bar.tsx`: the logo and wordmark leading home, the activity icon with
  its badge, and the profile menu with the secondary pages, the admin link for an
  administrator, the scheme switch and sign out. A visitor sees a sign-in link.
- `components/sidebar.tsx`: Holdings and Transactions with icons. The current page has the
  tint and the accent bar. It is an icon rail below `lg`, and collapses by hand with the
  choice kept in localStorage.
- `components/admin-nav.tsx` and `access-denied.tsx`: the admin area's sectioned
  navigation, with unbuilt pages dimmed and inert, and what a non-admin sees.
- `components/session-guard.tsx`: the guard a layout wraps its segment in, as
  `app/(app)/layout.tsx` and `app/admin/layout.tsx` do.

## Pages

`components/page-frame.tsx` is the column a page renders in: an action bar across the top
on the surface colour, with the title on the left, the page's actions beside it and a
rule below, then the padded body at prose width for a form or a record and wide for a
table. A page that belongs under another, as a statement's does under the statements,
names that page as `back`, and an arrow to it sits left of the title.

Actions are `components/button.tsx`: `primary` on accent-dark, at most one per view;
`secondary` bordered on the surface; `text` for the action bar, an icon and a label in
the action colour. A page's primary action also appears in its empty state.

## Components

- Tables: `components/table.tsx` gives `TableCard`, `Thead`, `Th`, `Td` and `Tr`. The card
  scrolls in both directions within the page, so the tinted header stays put. A `Tr` with
  an `href` opens a page and keeps a link in its first cell. `skeleton-rows.tsx` fills the
  body while it loads.
- `components/chip.tsx`: a small tinted categorical value. `state-chip.tsx` shows a run's
  outcome, with a spinner and the elapsed time while it runs.
- `components/notice.tsx`: an inline message above the content it concerns, `error` or
  `info`, with an optional retry.
- `components/empty-state.tsx`: where a table would be, saying what it would hold and
  offering the action that fills it.
- `components/dialog.tsx`: a native `<dialog>` centred over the page, with a scrolling body
  and a fixed footer for its actions. `sheet.tsx`: a native `<dialog>` anchored under the
  top bar on the right, not modal, so the bar stays live.
- `components/menu.tsx`: a dropdown under its trigger, closing on Escape, a click outside,
  a choice and navigation.
- `components/rejection-groups.tsx`: rejected rows grouped by reason, each reason a line
  with its count, opening to its rows on a page and standing alone in a narrow column.

## States

| State                     | Presentation                                         |
| ------------------------- | ---------------------------------------------------- |
| loading                   | `SkeletonRows` in a table, `Skeleton` elsewhere       |
| empty                     | `EmptyState` with the action that fills it            |
| error                     | `Notice` in the error tone with a retry               |
| run pending               | muted chip                                            |
| run running               | primary chip, spinner, elapsed time                   |
| run completed             | positive chip                                         |
| run completed, rejections | accent chip                                           |
| run failed or interrupted | negative chip                                         |

## Motion

A page column fades in on first render through `animate-fade-in`; `stagger-*` delays a
sequence. `prefers-reduced-motion` and the `data-testmode` attribute the e2e suite sets
zero every animation and transition.

## Test ids

`data-testid` goes on page containers, tables, rows, buttons, dialogs and form inputs,
named for what the element is rather than where it sits. The e2e suite selects on these
and nothing else, so an id it depends on is renamed only with its specs. A row or an item
carries its record's identifier, as `statement-row-<runId>` and `activity-item-<runId>`.
A chip carries its outcome as `data-state`.

## See also

The `typescript` skill for component and data-fetching conventions, and the `e2e-testing`
skill for how the suite uses the test ids.
