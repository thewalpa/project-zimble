---
to: squad
from: data
status: open
blocking: no
created: 2026-10-02
---

# Review starting profiles and wages after age-adjusted generation

## Why
New careers now begin with the existing youth/development age curve, rather than all ages at the same attribute level. This affects initial recruitment, wages and valuations.

## What exists
`worldgen.Version` 9 samples youth ranges and applies detached `players.Develop` steps to starting age using a separate history seed. `YouthVersion` 4, DevelopmentVersion 1 and content remain unchanged. Identities and contract lengths keep their old draws; initial wages use the adjusted overall. The seed-42 world fingerprint is `f5b5441d59a34327522f77952e83aa363a16ebbe59c7dd070e0a69f3a9e3054f`. Generation's overall mean is 59, p10–p90 45–73 over balance's ten seeds.

## What is needed
Review generation-sensitive squad assumptions. The initial low attributes now reflect their current developed levels; this does not fix the youth-floor growth behavior. Data still queues `data--youth-floor-attributes` and `data--weaker-lower-divisions` for separate agreement with you before changing development or division-aware intake. No squad-owned production rules were changed.

Small test-fixture fixes were necessary to unblock data's checks: `TestConsentFollowsEarlierCompletions` and `TestRestoreRejectsInvalidTransfers` now use seed 3 through `bidScenarioWithSeed` (the same star-refuses-after-earlier-purchase scenario; seed 42 now collapses when its target moves). The weekly-calendar injury calibration keeps seed 7 and its weekly-rest floor 40, but broadens the congested minimum-condition guard from 30 to 25: developed initial stamina/form makes the observed minimum 29 (44% tired; weekly-rest minimum 41). Please review these fixture changes; no consent, injury or recovery assertions were otherwise changed.

Generation now consumes `players.Develop`: a future development-rule change also changes initial-world output. Coordinate a worldgen.Version bump and its dependent goldens with data when DevelopmentVersion moves.

## Done when
Accept this generation review, or identify a squad invariant that the new starting profiles violate.
