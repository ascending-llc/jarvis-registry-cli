---
name: not-in-remote-and-name-collide-skill-3
description: 'This skill prints out "not-in-remote-and-name-collide-skill-3".'
---

This represents a skill that exists at local but NOT at remote—the skill with the same `id` does not exist on the server or is no longer accessible to the user.
In addition, this local skill's name collides with that of a remote skill of a different `id`, which the user does have access to.
After the sync, the same-named folder should still be there—it would be deleted and recreated; the contents should be the same as that of the remote skill;
the local manifest file should track the remote skill's `id` and `version`.
