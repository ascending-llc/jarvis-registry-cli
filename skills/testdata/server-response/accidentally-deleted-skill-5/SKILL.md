---
name: accidentally-deleted-skill-5
description: This skill prints out "accidentally-deleted-skill-5".
license: Apache-2.0
user-invocable: true
---

This represents a skill that exists at remote, is tracked in the local manifest file,
but whose folder is not in the Jarvis Registry CLI controlled skills folder.
This mimics the case where the human user manually deleted the skill's folder.
After the sync, it should be restored with the right folder and contents at `version` 5.
