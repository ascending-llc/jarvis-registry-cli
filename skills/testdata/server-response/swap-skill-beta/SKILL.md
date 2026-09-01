---
name: swap-skill-beta
description: This skill prints out "swap-skill-beta".
user-invocable: true
---

This represents one of a pair of skills (with swap-skill-alpha) whose names get swapped by the remote at sync time, to exercise the rename-swap race between two concurrent update calls (id 8: alpha->beta, id 9: beta->alpha).
This represents the new skill contents at version 2, belonging to id 6a1367200d32ee7200500008, which was previously synced locally under the name "swap-skill-alpha".
