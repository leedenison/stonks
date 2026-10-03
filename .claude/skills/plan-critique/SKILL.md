---
name: plan-critique
description: How an implementation plan for an issue is drafted, critiqued by four independent agents and revised before the user sees it. The critics cover API design, testability, consistency with recorded decisions, and facts about third-party systems. Use when asked to plan an issue, to draft or revise an implementation plan, or when planning work in plan mode.
---

# Plan Critique

A plan reaches the user after four independent critics have read it and the author has
answered every finding. The user sees the final plan and a record of what the critics
changed.

## Procedure

1. **Read the issue.** Issues live in docs/tasks/<label>/issues/<nnn>-<slug>.md. Read
   the milestone's ADRs in docs/tasks/<label>/adr/, the deferred notes in
   docs/deferred/, and the code the plan will touch.

2. **Draft the plan** to the scratchpad directory, as plan-<label>-<nnn>.md. Do not show
   it to the user and do not summarise it to them yet. Follow the plan shape below.

3. **Launch the four critics in parallel**, in one message, with the Agent tool. Set
   subagent_type to each agent's name:

   - api-critic
   - testability-critic
   - consistency-critic
   - facts-critic

   Give each the absolute path of the plan and the absolute path of the issue, and
   nothing else. Each critic reads the repository itself. Do not restate the plan in the
   prompt, because the critic must judge the plan as written.

4. **Judge each finding.** A finding is valid when its evidence holds and its proposed
   change makes the plan better. Revise the plan for every valid finding. For each
   rejected finding, write down the reason.

5. **Run a second round** only when the revision changed the plan's shape, for example a
   new abstraction, a new external dependency, or a different test strategy. Rerun only
   the critics whose area changed. Stop after the second round.

6. **Present the plan**, then a section titled "Critiques considered". That section has
   one bullet per finding. Each bullet names the critic and states the finding in a few
   words. It then says what changed, or says the finding was rejected and why. Group the
   bullets by critic. Do not omit rejected findings.

In plan mode, complete steps 1 to 6 before calling ExitPlanMode, and put the
"Critiques considered" section in the plan file.
