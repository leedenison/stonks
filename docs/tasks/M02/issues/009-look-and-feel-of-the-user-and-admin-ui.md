---
title: Look and feel of the user and admin UI
type: task
---

## Scope

The shell and page patterns the user and admin pages are built on, settled before the
first of them is built.

In:

- The application shell: navigation between the user pages including the transactions
  page, where the admin area sits and how an administrator reaches it, and what a
  non-admin sees of it.
- Page templates for the shapes the initial functionality needs: a table of rows, a
  dialog that starts work, and a page showing a run's outcome.
- The activity sheet: the icon in the top bar, the badge that signals a finished run,
  the list of runs with the state of each, and an item expanded to show its rejected
  rows.
- How a rejected row, an empty table, a loading state and an error are presented, so
  the upload dialog, the activity sheet, the history and holdings pages present them the
  same way.
- The admin pages that follow this milestone, runs, findings and datasources, sketched
  to the same patterns so the shell does not change when they arrive.
- The design as a canvas or mockups covering each page, the dialog and the sheet of
  issues [007](007-statements-in-the-browser.md) and
  [008](008-holdings-in-the-browser.md), and any change to the `frontend-design` skill
  the decisions require.
- Applying the `frontend-design` skill to the existing UI: the shell, the sign-in and
  profile pages and the error and not-found pages, so the tokens, dark mode, typography,
  density and motion it fixes are what the new pages inherit.

Out:

- Building the upload dialog, the activity sheet, the history and holdings pages.

## Design

### Palette

Two hues with distinct jobs. Primary is a slate blue and is structural: the top bar, the
active navigation item, table header tints and focus rings. Accent is a warm orange and
marks action: the primary button, the active marker in the navigation and the bar under
the top bar. Positive and negative stay reserved for gains and losses and for run
outcomes.

Each hue has three steps: the hue, a dark step and a light or soft step. Values by scheme:

| Token         | Light             | Dark              |
| ------------- | ----------------- | ----------------- |
| primary       | rgb(62 107 138)   | rgb(85 153 204)   |
| primary-dark  | rgb(27 58 75)     | rgb(26 48 72)     |
| primary-light | rgb(191 211 225)  | rgb(30 61 90)     |
| accent        | rgb(224 122 47)   | rgb(224 122 47)   |
| accent-dark   | rgb(184 94 29)    | rgb(200 104 32)   |
| accent-soft   | rgb(247 216 193)  | rgb(45 24 5)      |
| background    | rgb(244 246 249)  | rgb(13 17 23)     |
| surface       | rgb(255 255 255)  | rgb(22 28 39)     |
| border        | rgb(214 222 231)  | rgb(37 47 62)     |
| text          | rgb(15 23 32)     | rgb(226 232 240)  |
| text-muted    | rgb(90 107 122)   | rgb(122 143 168)  |

The top bar is primary-dark with white text in both schemes.

Every text and background pair meets AA at 4.5:1. White on accent is 3:1 and is not used
for text; white text sits on accent-dark. Text on the accent-soft notice tint is
accent-dark in light and accent in dark.

The scheme follows the system by default. The profile dropdown holds a light, dark or
system switch, kept in local storage.

### Icon

The logo is a barrel of banknotes, at `client/public/logo.png` and
`client/public/logo-inverted.png`. The inverted variant sits on the top bar; the plain
variant is for light surfaces and the favicon.

### Top bar

A full-width bar on primary-dark, white text, with a faint diagonal hatch over it at low
opacity and a 3px gradient from accent to primary along its bottom edge. It is the same
on every page, including the admin area.

Left to right:

- The logo at 36px and the wordmark in the display face, together a link home.
- A slot for a context chip: a translucent white pill with a chevron that opens a chooser.
  Nothing fills it in this milestone.
- On the right, the activity icon with its badge, then the profile chip: the user's
  email with a chevron, translucent on hover, opening a dropdown. The dropdown shows the
  email as its header, then the user's secondary pages, then Admin when the user is an
  administrator, then the scheme switch, then a separator and log out. A signed-out
  visitor sees the sign-in button in place of both.

### Left navigation

A sidebar of about 13rem on the surface colour with a right border, listing the user
pages as a vertical stack of rounded items, each with an icon and a label. The active
item has a faint primary-dark tint, semibold primary-dark text and a 3px accent bar at
its left edge. Hover tints an item with primary-light. A page that exists but is not yet
built is listed dimmed and inert, so the shape of the navigation is settled early.

The sidebar collapses to an icon rail below a breakpoint, and a control at its foot
collapses it by hand. The collapsed state is kept in local storage.

The user pages are Holdings and Transactions. Statements, the history page, is reached
from the profile dropdown.

### Admin area

The admin area is reached from the profile dropdown and shares the top bar. It has its
own left navigation: top-level entries first, then uppercase section headings each with
an indented list of children along a left rule. A non-admin who reaches an admin URL sees
an access denied page under the top bar.

### Buttons and inputs

- Primary action: accent-dark background, white semibold text, small padding, rounded-md,
  darkening on hover. At most one per view.
- Secondary action: bordered surface, muted hover tint of primary-light.
- Text action: an icon and a label in the action colour, a step of the accent hue that
  reaches AA on a surface, with the muted hover tint. The form an action takes in the
  action bar.
- Inputs: bordered surface, primary border and a faint primary ring on focus.
- An inline error or notice: accent-soft at half opacity with the text colour above.

### Tables

A table sits in a bordered surface card with rounded corners and a faint shadow, scrolling
horizontally when narrower than its columns. The header row has a faint primary-dark tint,
a heavier bottom border and uppercase muted labels with wide tracking, and stays fixed at
the top while the rows scroll under it. Rows tint on hover. Numbers are in the mono face
with tabular figures, right aligned. Categorical values are small tinted chips.

A table page is full width within the shell, capped well above the reading width so
holdings and transactions get their columns.

### States

- Loading: skeleton rows shaped like the table they stand in, so nothing shifts when the
  data arrives.
- Empty: a short sentence saying what the table would hold and the primary action that
  fills it. An empty transactions page carries the upload button.
- Error: the inline notice above the table, with the message and a retry.

### Run states

A run's progress is its state, shown as a chip. The mapping is the same in the activity
sheet, the history page and the admin runs page:

| State                     | Chip                                        |
| ------------------------- | ------------------------------------------- |
| pending                   | muted                                       |
| running                   | primary, with a spinner and elapsed time    |
| completed, no rejections  | positive                                    |
| completed with rejections | accent                                      |
| failed or interrupted     | negative                                    |

A run reaching a terminal state invalidates the transactions, holdings and statements
queries, so the page behind the sheet refreshes without a reload.

Rejections are grouped by reason. Each reason shows its count and expands to its rows,
each with its ordinal and the row as stated.

### Pages and dialogs

A page is a column that fades in on first render. Across its top runs the action bar:
the page title on the left, the page's actions beside it as text actions, and a rule
below, on the surface colour. The body sits under the bar with its own padding, at prose
width for a form or a record and wide for a table. A page without actions still has the
bar with its title. The upload action sits in the bar of the transactions and statements
pages.

A dialog is centred over a 40% black overlay on a rounded surface, with a titled header row
holding the close control, and traps focus.

A dialog that starts work is staged. The upload dialog goes: choose or drop a file;
parsing, with a spinner; review, showing the broker detected, the row count, the claimed
period as editable dates and the first few rows; submit. The dialog closes once the run
is created and hands off to the activity sheet.

The transactions page is a drop target. Dropping an export opens the upload dialog at
the parsing stage.

### Activity sheet

The activity icon in the top bar opens a full-height sheet from the right edge, over the
page and under the top bar, closed by its control, by Escape or by a click outside. The
badge on the icon counts runs that reached a terminal state since the sheet was last
opened. The sheet lists the user's runs newest first, each with its state chip, and an
item expands to its grouped rejections.

The sheet's items are runs and the UI calls them activity.

## Open questions

- Whether the display and body faces change with the palette.
- Whether submitting the dialog opens the sheet or only badges the icon.
