---
to: squad
from: competitions
status: open
blocking: no
created: 2026-10-01
---

# Review the existing squad guards after cup interleaving

## What changed
ScheduleVersion 4 moves edition N into league season N+1, with midweek cup ties four days after selected league matchdays. No squad production rule, medical version or transfer version changed. Built-in league spacing stays weekly pending data's August-to-May definitions.

The user authorized finishing all conflicts and checks on competitions' branch. Three squad-owned tests needed calendar-driven adjustments:
- `TestInjuryAndConditionLevelsOverSeasons`: seed 7 over three years has 1,098 injuries / 1,920 player-seasons, 1.08 out per club at kickoff (maximum 7), 45% of starters below full condition, minimum 32. Keep the existing floor 40 for teams with at least a week's rest; use floor 30 for congestion. All injury incidence, tired-share and crisis-size bounds remain.
- `TestAIMarketKeepsSquadsFullForDecades`: the shifted seeded squads leave two ordinary vacancies across all AI clubs, rather than one (reproduced at 2045-07-29). The thirty-year guard permits two, as the existing lifecycle test does; world validation still enforces positional minima, and transfer accounting/population checks remain.
- `TestSquadsSurviveAHoardingManager`: its old reserve-only allowance cannot account for a manager holding more players than that reserve. Bound vacancies by the larger of `freeAgentReserve` and the manager's actual positional surplus above the normal roster, plus the AI-only two-vacancy allowance. Squad legality, release payoffs and zero-sum fees still hold for thirty years. The shared assertion now reports the date, shortage and balance on failure.

## What is needed
Review the test changes and incorporate the midweek density into the already accepted condition/calendar recalibration. These are empirical scenario bounds, not a new roster or medical rule. The cup's season-end task remains due at its final kickoff in Consequences; edition N now ends during season N+1, before that year's play-offs on the built-in calendar.

## Done when
The existing squad/calendar recalibration accounts for midweek ties and retains legality and determinism checks.
