---
name: not-in-remote-and-name-collide-skill-3
description: 'This skill prints out "not-in-remote-and-name-collide-skill-3".'
---

This represents a remote skill (`id` `6a1367200d32ee7200500006`) whose name collides with a
different local skill's outdated name (`id` `6a1367200d32ee7200500003`, which no longer exists
at remote and is slated for deletion).
It should be newly created after the sync, at `version` 6, only after the stale local folder of
the same name has been deleted.
