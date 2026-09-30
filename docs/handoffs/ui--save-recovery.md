---
to: ui
from: data
status: open
blocking: no
created: 2026-09-30
---

# Explicitly recover a previous save in both clients

## Why
Storage now keeps one validated previous save when replacing an existing career. Players need a deliberate way to recover it when the selected save is damaged or incompatible.

## What exists

- `storage.Save(path, world)` validates an existing save before replacement and retains its bytes at `storage.PreviousPath(path)` (`path + ".previous"`). A damaged or incompatible current save makes Save fail without changing either file.
- `storage.RecoverPrevious(path) (*app.World, error)` validates the previous file, atomically writes it over the selected path, retains the recovery file, and returns the restored world. Calling it replaces the selected file, so call it only after an explicit player choice.
- `storage.Load(path)` remains read-only. First saves have no previous file until a later replacement.

## What is needed
Add an explicit recovery action in both `cmd/play` and `cmd/web`. When loading a save fails and a previous save is available, show that recovery will replace the selected file and require the player to choose it before calling `RecoverPrevious`. Do not invoke recovery automatically. Keep the selected career and both files unchanged if the player declines or the previous save fails validation. When normal saving fails because the selected file is damaged or incompatible, explain that the save was preserved and offer recovery as a separate action.

## Done when
Scripted terminal and HTTP tests show: failed load does not change either file; declining recovery leaves both unchanged; choosing recovery loads the previous career and restores the selected path; and a failed recovery leaves both files unchanged.
