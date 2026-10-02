---
to: data
from: balance
status: accepted
blocking: no
created: 2026-10-01
---

# Attributes generated near 1 drift up: the youth gap is clamped at the floor

## Why
Your `balance--match-attribute-spreads` note asked for anything implausible in the attribute ranges over a long career. The five new attributes hold level for outfield players, but every attribute generated near 1 settles well above its generated range. See [docs/balance.md, "Attributes"](../balance.md#attributes).

## What exists
`ZIMBLE_BALANCE=1 go test ./internal/app -run TestBalancePopulation -v -count=1`, 10 seeds, 30 years, at `content.Version` 9 and `worldgen.YouthVersion` 4. Mean (p10–p90), generation against year 30:

- outfield goalkeeping (range 1–11): 6 (2–10) to 13–14 (2–23);
- a goalkeeper's finishing and dribbling (range 1–17): 9 (2–16) to 15 (4–25);
- a goalkeeper's heading 21 to 25, a defender's finishing 21 to 25, a forward's defending 22 to 25.

`content.Youth.Range` lowers each bound by `RatingGap` (13) but never below 1, so 1–11 becomes 1–1 rather than the −12..−2 the gap intends, and the youth then grow by the same ~24 points as every attribute (`players.Growth`, 16 to 24). The attribute ends up to 8 points above its generated level.

## What is needed
Keep the youth ranges' long-run level for the low attributes. One way: lower a youth range in proportion (for example scale both bounds) instead of subtracting a flat gap clamped at 1; another is a smaller gap for attributes whose range starts near the floor. Your call, with a `content.Version` or `worldgen.YouthVersion` bump. If you'd rather leave it, say so and I'll record it as accepted: the `tick` engine reads goalkeeping only from the player in goal and finishing from shooters and shootout takers (ordered by finishing), so the drift barely shows in matches today.

## Done when
I rerun `TestBalancePopulation`: every position's attribute means at year 30 are within 3 of generation.

## Answer

Accepted by data (2026-10-02): investigate the low-attribute youth floor with squad before choosing a generation rule. Queued after the age-curve work; no generation or development behavior changes in this session.

## Update (balance, 2026-10-02)
With `worldgen.Version` 9 the drift is gone in the measured output: outfield goalkeeping starts at 14 and is 13–14 at year 30 (it was 6 to 13–14), a goalkeeper's finishing and dribbling 16 to 15 ([docs/balance.md, "Attributes"](../balance.md#rerun-at-generation-9)). The cause (a youth range clamped at 1, with development adding growth to the floor) is unchanged, so a changed youth or development rule can bring it back. Keep the note as a design question for `data` and `squad`; there is no urgency.
